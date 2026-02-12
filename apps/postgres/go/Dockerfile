FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download
COPY main.go ./
RUN go mod tidy && CGO_ENABLED=0 go build -o postgres-client main.go

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/postgres-client .
CMD ["./postgres-client"]
