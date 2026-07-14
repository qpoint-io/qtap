FROM golang:1.26.5-alpine AS builder

WORKDIR /app

COPY go.mod go.sum main.go ./
RUN go mod download && go build -o kafka-client .

FROM alpine:latest

WORKDIR /app
COPY --from=builder /app/kafka-client .

CMD ["./kafka-client"]
