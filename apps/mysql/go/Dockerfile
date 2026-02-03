FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=0 go build -o mysql-client main.go

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/mysql-client .
CMD ["./mysql-client"]
