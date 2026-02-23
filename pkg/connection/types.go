package connection

import (
	"fmt"
)

type Cookie uint64

func (c Cookie) Key() Cookie {
	return c
}

type pidfd struct {
	PID uint32
	FD  int32
}

// a unique connection composite key
type ConnPIDKey struct {
	PID      uint32 // Process PID
	TGID     uint32 // Process TGID
	FD       int32  // The file descriptor to the opened network connection
	FUNCTION Source // The function of the connection
	TSID     uint64 // Timestamp at the initialization of the struct
}

// returns a string representation of connID
func (c ConnPIDKey) String() string {
	return fmt.Sprintf("PID:%d TGID:%d FD:%d FUNCTION:%s TSID:%d",
		c.PID,
		c.TGID,
		c.FD,
		c.FUNCTION.String(),
		c.TSID)
}

func (c ConnPIDKey) PIDFD() pidfd {
	return pidfd{PID: c.PID, FD: c.FD}
}

type SocketType string

const (
	SocketType_UNKNOWN SocketType = ""
	SocketType_TCP     SocketType = "tcp"
	SocketType_UDP     SocketType = "udp"
	SocketType_RAW     SocketType = "raw"
	SocketType_ICMP    SocketType = "icmp"
)

func (t SocketType) String() string {
	return string(t)
}

type Source uint32

const (
	Client Source = iota + 1 // iota is 1 for the first constant
	Server
)

func (s Source) String() string {
	switch s {
	case Client:
		return "client"
	case Server:
		return "server"
	default:
		return "unknown"
	}
}

type HandlerType string

const (
	HandlerType_RAW        HandlerType = "raw"
	HandlerType_REDIRECTED HandlerType = "redirected"
	HandlerType_FORWARDING HandlerType = "forwarding"
)

func (t HandlerType) String() string {
	return string(t)
}

type Direction string

func (d Direction) String() string {
	return string(d)
}

// directions
const (
	Ingress Direction = "ingress"
	Egress  Direction = "egress"
)

// L7 protocol
type Protocol string

const (
	Protocol_UNKNOWN Protocol = "unknown"
	Protocol_HTTP1   Protocol = "http1"
	Protocol_HTTP2   Protocol = "http2"
	Protocol_DNS     Protocol = "dns"
	Protocol_MONGODB Protocol = "mongodb"
	Protocol_REDIS   Protocol = "redis"
	Protocol_MYSQL   Protocol = "mysql"
	Protocol_KAFKA   Protocol = "kafka"
	Protocol_GRPC    Protocol = "grpc"
)

func (c Protocol) String() string {
	return string(c)
}
