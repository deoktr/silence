FROM docker.io/library/golang:1.24 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /go/bin/silence

FROM gcr.io/distroless/base-debian12:nonroot

USER nonroot
COPY --from=build /go/bin/silence /usr/bin/silence
CMD [ "/usr/bin/silence", "-addr", "0.0.0.0:8080" ]
