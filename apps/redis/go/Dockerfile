FROM golang:1.26.0-alpine AS builder

WORKDIR /app

COPY go.mod main.go ./
RUN go mod tidy && go build -o redis-client .

FROM alpine:latest

WORKDIR /app
COPY --from=builder /app/redis-client .

CMD ["./redis-client"]
