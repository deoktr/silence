package main

import (
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	readWait       = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
)

var (
	addr = "localhost:8080"

	//go:embed home.html
	homeHtml []byte

	newline  = []byte{'\n'}
	space    = []byte{' '}
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
)

type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
	id   string
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		uid := []byte(c.id + " ")
		message = append(uid[:], message[:]...)
		message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
		c.hub.broadcast <- message
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		message := bytes.TrimSpace(fmt.Appendf(nil, "> User disconnected: %s (total: %d)", c.id, len(c.hub.clients)))
		c.hub.broadcast <- message
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)
			n := len(c.send)
			for range n {
				w.Write(newline)
				w.Write(<-c.send)
			}
			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	uid, _ := uuid.NewRandom()
	id := uid.String()[:6]
	client := &Client{hub: hub, conn: conn, send: make(chan []byte, 256), id: id}
	client.hub.register <- client
	message := bytes.TrimSpace(fmt.Appendf(nil, "> New user connected: %s (total: %d)", id, len(client.hub.clients)+1))
	client.hub.broadcast <- message
	go client.writePump()
	go client.readPump()
}

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
}

func newHub() *Hub {
	return &Hub{
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

func getListener() net.Listener {
	var listener net.Listener = nil
	var err error
	if os.Getenv("LISTEN_PID") == strconv.Itoa(os.Getpid()) {
		// systemd file listener for systemd.socket
		f := os.NewFile(3, "from systemd")
		listener, err = net.FileListener(f)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("silence listening on socket file")
	} else {
		// port bind
		listener, err = net.Listen("tcp", addr)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("silence listening on: %s", addr)
	}
	return listener
}

func serve() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
		w.Write(homeHtml)
	})
	hub := newHub()
	go hub.run()
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(hub, w, r)
	})

	server := &http.Server{
		Addr:              addr,
		Handler:           http.HandlerFunc(mux.ServeHTTP),
		ReadTimeout:       readWait,
		WriteTimeout:      writeWait,
		ReadHeaderTimeout: readWait,
		MaxHeaderBytes:    1 << 20,
	}

	listener := getListener()

	err := server.Serve(listener)
	if err != nil {
		log.Fatal(err)
		return
	}
}

func init() {
	flag.StringVar(&addr, "addr", addr, "silence listen address")
}

func main() {
	flag.Parse()

	serve()
}
