package connection

import (
	"fmt"
	"net"
)

type ConnKey struct {
	Pid        uint32
	LocalIP    net.IP
	RemoteIP   net.IP
	LocalPort  uint16
	RemotePort uint16
}

func (c ConnKey) String() string {
	return fmt.Sprintf("PID:%d LOCAL_IP:%s REMOTE_IP:%s LOCAL_PORT:%d REMOTE_PORT:%d",
		c.Pid,
		c.LocalIP.String(),
		c.RemoteIP.String(),
		c.LocalPort,
		c.RemotePort)
}
func (c ConnKey) Key() string {
	return c.String()
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
	Protocol_GRPC    Protocol = "grpc"
)

func (c Protocol) String() string {
	return string(c)
}
