FROM docker.io/library/golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY main.go home.html ./
RUN CGO_ENABLED=0 go build -o /silence

FROM gcr.io/distroless/static-debian13:nonroot

COPY --from=build /silence /silence
ENTRYPOINT ["/silence"]
CMD ["-addr", "0.0.0.0:8000"]
