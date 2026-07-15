FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod main.go ./
RUN go mod tidy && go build -o kafka-client .

FROM alpine:latest

WORKDIR /app
COPY --from=builder /app/kafka-client .

CMD ["./kafka-client"]
