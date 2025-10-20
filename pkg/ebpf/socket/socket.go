package socket

import (
	"encoding/binary"
	"fmt"
	"net"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/qnet"
)

// socketEvents represents socket event types
type Source uint32

const (
	Client Source = iota + 1 // iota is 1 for the first constant
	Server
)

// traffic direction
type Direction int

// directions
const (
	Ingress Direction = iota
	Egress
)

func (d Direction) String() string {
	switch d {
	case Ingress:
		return "ingress"
	case Egress:
		return "egress"
	default:
		return "unknown"
	}
}

// socket types
type socketType uint32

const (
	socketType_UNKNOWN socketType = iota
	socketType_TCP
	socketType_UDP
	socketType_RAW
	socketType_ICMP
)

// L7 protocol
type Protocol uint32

const (
	Protocol_UNKNOWN Protocol = iota
	Protocol_HTTP1
	Protocol_HTTP2
	Protocol_DNS
	Protocol_MONGODB
	Protocol_GRPC
)

func (p Protocol) String() string {
	switch p {
	case Protocol_UNKNOWN:
		return "UNKNOWN"
	case Protocol_HTTP1:
		return "HTTP1"
	case Protocol_HTTP2:
		return "HTTP2"
	case Protocol_DNS:
		return "DNS"
	case Protocol_MONGODB:
		return "MONGODB"
	case Protocol_GRPC:
		return "GRPC"
	default:
		return fmt.Sprintf("BAD PROTOCOL(%d)", p)
	}
}

// connKey represents the C structure c_key_t in Go.
type connKey struct {
	Pid        uint32
	LocalIP    [16]uint8
	RemoteIP   [16]uint8
	LocalPort  uint16
	RemotePort uint16
}

func (c connKey) buildConnKey() connection.ConnKey {
	return connection.ConnKey{
		Pid:        c.Pid,
		LocalIP:    net.IP(c.LocalIP[:]),
		RemoteIP:   net.IP(c.RemoteIP[:]),
		LocalPort:  fixPortEndianness(binary.NativeEndian, c.LocalPort),
		RemotePort: fixPortEndianness(binary.NativeEndian, c.RemotePort),
	}
}

func (c connKey) String() string {
	localIP := net.IP(c.LocalIP[:])
	remoteIP := net.IP(c.RemoteIP[:])

	localFamily := "ipv4"
	if localIP.To4() == nil {
		localFamily = "ipv6"
	}
	remoteFamily := "ipv4"
	if remoteIP.To4() == nil {
		remoteFamily = "ipv6"
	}

	return fmt.Sprintf("ID:%d SRC:%s IPv4/IPv6:%s DST:%s IPv4/IPv6:%s SRC_PORT:%d DST_PORT:%d",
		c.Pid,
		localIP.String(),
		localFamily,
		remoteIP.String(),
		remoteFamily,
		c.LocalPort,
		c.RemotePort,
	)
}

// socketEvents represents socket event types
type socketEvents uint64

const (
	socketEvents_OPEN socketEvents = iota + 1 // iota is 1 for the first constant
	socketEvents_CLOSE
	socketEvents_DATA
	socketEvents_PROTO
	socketEvents_HOSTNAME
	socketEvents_TLS_CLIENT_HELLO
)

// event
type socketEvent struct {
	Type socketEvents
}

// socketOpenEvent represents the C structure socket_open_event_t in Go.
type socketOpenEvent struct {
	TimestampNS  uint64     // The time of the event in nanoseconds
	ConnKey      connKey    // Connection key
	Pid          uint32     // Process PID
	Tgid         uint32     // Process TGID
	SocketType   socketType // socket type
	IsRedirected bool       // is this a redirected through a forwarder?
	Source       Source     // the source of the connection
}

func (e socketOpenEvent) buildConnOpenEvent() connection.OpenEvent {
	oe := connection.OpenEvent{
		ConnKey:      e.ConnKey.buildConnKey(),
		TimestampNS:  e.TimestampNS,
		PID:          e.Pid,
		TGID:         e.Tgid,
		Local:        qnet.NetAddrFromIPPort(net.IP(e.ConnKey.LocalIP[:]), e.ConnKey.LocalPort),
		Remote:       qnet.NetAddrFromIPPort(net.IP(e.ConnKey.RemoteIP[:]), e.ConnKey.RemotePort),
		IsRedirected: e.IsRedirected,
		SocketType:   e.socketType(),
	}

	switch e.Source {
	case Client:
		oe.Source = connection.Client
	case Server:
		oe.Source = connection.Server
	}

	return oe
}

func (e socketOpenEvent) socketType() connection.SocketType {
	switch e.SocketType {
	case socketType_ICMP:
		return connection.SocketType_ICMP
	case socketType_RAW:
		return connection.SocketType_RAW
	case socketType_TCP:
		return connection.SocketType_TCP
	case socketType_UDP:
		return connection.SocketType_UDP
	default:
		return connection.SocketType_UNKNOWN
	}
}

// socketCloseEvent represents the C structure socket_close_event_t in Go.
type socketCloseEvent struct {
	TimestampNS uint64  // Timestamp of the close syscall
	ConnKey     connKey // Connection key
	WrBytes     int64   // Total number of bytes written on that connection
	RdBytes     int64   // Total number of bytes read on that connection
	Pid         uint32  // Process PID
	Tgid        uint32  // Process TGID
}

func (e socketCloseEvent) buildConnCloseEvent() connection.CloseEvent {
	return connection.CloseEvent{
		ConnKey:     e.ConnKey.buildConnKey(),
		TimestampNS: e.TimestampNS,
		WrBytes:     e.WrBytes,
		RdBytes:     e.RdBytes,
	}
}

const MAX_MSG_SIZE = 30720 // Ensure this matches the C definition

// attr represents the attributes within the socket_data_event_t struct.
type attr struct {
	TimestampNS uint64  // The timestamp when syscall completed
	ConnKey     connKey // Connection key
	Direction   uint32  // The type of the actual data
	MsgSize     uint32  // The size of the original message
	Pos         uint64  // A 0-based position number for this event
	Pid         uint32  // Process PID
	Tgid        uint32  // Process TGID
}

// socketProtoEvent represents the C struct socket_proto_event_t in Go.
type socketProtoEvent struct {
	TimestampNS uint64   // Timestamp when the protocol was detected
	ConnKey     connKey  // Connection key
	Protocol    Protocol // l7 protocol
	IsTLS       bool     // is this ssl?
}

func (e socketProtoEvent) buildConnectionProtocolEvent() connection.ProtocolEvent {
	var p connection.Protocol

	switch e.Protocol {
	case Protocol_UNKNOWN:
		p = connection.Protocol_UNKNOWN
	case Protocol_DNS:
		p = connection.Protocol_DNS
	case Protocol_HTTP1:
		p = connection.Protocol_HTTP1
	case Protocol_HTTP2:
		p = connection.Protocol_HTTP2
	case Protocol_MONGODB:
		p = connection.Protocol_MONGODB
	case Protocol_GRPC:
		p = connection.Protocol_GRPC
	}

	return connection.ProtocolEvent{
		ConnKey:     e.ConnKey.buildConnKey(),
		TimestampNS: e.TimestampNS,
		Protocol:    p,
		IsTLS:       e.IsTLS,
	}
}

// socketHostnameAttr represents the attributes within the socket_hostname_event_t struct.
type socketHostnameAttr struct {
	TimestampNs uint64
	ConnKey     connKey // Connection key
	HostnameLen uint8
	_           [7]byte
}

func (e socketProtoEvent) ProtocolString() string {
	switch e.Protocol {
	case Protocol_HTTP1, Protocol_HTTP2:
		return "http"
	case Protocol_DNS:
		return "dns"
	case Protocol_GRPC:
		return "http2grpc"
	default:
		return "unknown"
	}
}

// socketTLSClientHelloAttr represents the attributes within the socket_tls_client_hello_attr_t struct.
type socketTLSClientHelloAttr struct {
	ConnKey connKey // Connection key
	Size    uint32  // 4 bytes
}
