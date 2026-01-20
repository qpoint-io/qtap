FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY main.go ./
RUN go build -o redis-client .

FROM alpine:latest

WORKDIR /app
COPY --from=builder /app/redis-client .

CMD ["./redis-client"]
