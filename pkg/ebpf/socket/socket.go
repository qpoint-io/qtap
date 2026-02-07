package socket

import (
	"fmt"
	"net"
	"strconv"
	"syscall"

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
	Protocol_REDIS
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
	case Protocol_REDIS:
		return "REDIS"
	case Protocol_GRPC:
		return "GRPC"
	default:
		return fmt.Sprintf("BAD PROTOCOL(%d)", p)
	}
}

// connPIDID represents the C structure conn_pid_id_t in Go.
type connPIDID struct {
	PID      uint32 // Process PID
	TGID     uint32 // Process TGID
	FD       int32  // The file descriptor to the opened network connection
	FUNCTION Source // The function of the connection
	TSID     uint64 // Timestamp at the initialization of the struct
}

func (c connPIDID) buildConnPIDKey() connection.ConnPIDKey {
	return connection.ConnPIDKey{
		PID:      c.PID,
		TGID:     c.TGID,
		FD:       c.FD,
		FUNCTION: connection.Source(c.FUNCTION),
		TSID:     c.TSID,
	}
}

// ensure netAddr fulfills net.Addr interface
var _ net.Addr = netAddr{}

type netAddr struct {
	SaFamily uint16   // Address family (AF_INET or AF_INET6), 2 bytes
	Addr     [16]byte // IPv6 address space, also used for IPv4, 16 bytes
	Port     uint16   // Address port, 2 bytes
}

func (a netAddr) AddrSize() uint16 {
	switch a.SaFamily {
	case syscall.AF_INET:
		return 4
	case syscall.AF_INET6:
		return 16
	default:
		return 0
	}
}

func (a netAddr) FamilyString() string {
	switch a.SaFamily {
	case syscall.AF_INET:
		return "ipv4"
	case syscall.AF_INET6:
		return "ipv6"
	default:
		return "unknown"
	}
}

func (a netAddr) AddrString() string {
	switch {
	case a.Port == 0 && a.SaFamily == syscall.AF_INET:
		return "0.0.0.0"
	case a.Port == 0 && a.SaFamily == syscall.AF_INET6:
		return "::"
	default:
		return qnet.IPString(net.IP(a.Addr[:a.AddrSize()]))
	}
}

func (a netAddr) String() string {
	switch a.SaFamily {
	case syscall.AF_INET, syscall.AF_INET6:
		return net.JoinHostPort(a.AddrString(), strconv.Itoa(int(a.Port)))
	default:
		return "unknown"
	}
}

func (a *netAddr) IsPrivateIP() bool {
	return net.IP(a.Addr[:a.AddrSize()]).IsPrivate()
}

func (a netAddr) Network() string {
	return ""
}

func (a netAddr) buildConnectionNetAddr() qnet.NetAddr {
	return qnet.NetAddr{
		Family: qnet.NetFamily(a.FamilyString()),
		IP:     net.IP(a.Addr[:a.AddrSize()]),
		Port:   a.Port,
	}
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
	socketEvents_TLS_SERVER_HELLO
)

// event
type socketEvent struct {
	Type socketEvents
}

// socketOpenEvent represents the C structure socket_open_event_t in Go.
type socketOpenEvent struct {
	TimestampNS  uint64     // The time of the event in nanoseconds
	ConnID       connPIDID  // A unique ID for the connection
	Cookie       uint64     // Socket cookie
	Local        netAddr    // the local address
	Remote       netAddr    // the remote address
	Pid          uint32     // Process PID
	Tgid         uint32     // Process TGID
	SocketType   socketType // socket type
	IsRedirected bool       // is this a redirected through a forwarder?
}

func (e socketOpenEvent) buildConnOpenEvent() connection.OpenEvent {
	oe := connection.OpenEvent{
		Cookie:       connection.Cookie(e.Cookie),
		ConnPIDKey:   e.ConnID.buildConnPIDKey(),
		TimestampNS:  e.TimestampNS,
		PID:          e.ConnID.PID,
		TGID:         e.ConnID.TGID,
		Local:        e.Local.buildConnectionNetAddr(),
		Remote:       e.Remote.buildConnectionNetAddr(),
		IsRedirected: e.IsRedirected,
		SocketType:   e.socketType(),
	}

	switch e.ConnID.FUNCTION {
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
	TimestampNS uint64    // Timestamp of the close syscall
	ConnID      connPIDID // The unique ID of the connection
	Cookie      uint64    // Socket cookie
	WrBytes     int64     // Total number of bytes written on that connection
	RdBytes     int64     // Total number of bytes read on that connection
	Pid         uint32    // Process PID
	Tgid        uint32    // Process TGID
}

func (e socketCloseEvent) buildConnCloseEvent() connection.CloseEvent {
	return connection.CloseEvent{
		Cookie:      connection.Cookie(e.Cookie),
		TimestampNS: e.TimestampNS,
		WrBytes:     e.WrBytes,
		RdBytes:     e.RdBytes,
	}
}

const MAX_MSG_SIZE = 30720 // Ensure this matches the C definition

// attr represents the attributes within the socket_data_event_t struct.
type attr struct {
	TimestampNS uint64    // The timestamp when syscall completed
	ConnID      connPIDID // Connection identifier
	Cookie      uint64    // Socket cookie
	Direction   uint32    // The type of the actual data
	MsgSize     uint32    // The size of the original message
	Pos         uint64    // A 0-based position number for this event
	Pid         uint32    // Process PID
	Tgid        uint32    // Process TGID
}

// socketProtoEvent represents the C struct socket_proto_event_t in Go.
type socketProtoEvent struct {
	TimestampNS uint64    // Timestamp when the protocol was detected
	ConnID      connPIDID // Connection identifier
	Cookie      uint64    // Socket cookie
	Protocol    Protocol  // l7 protocol
	IsTLS       bool      // is this ssl?
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
	case Protocol_REDIS:
		p = connection.Protocol_REDIS
	case Protocol_GRPC:
		p = connection.Protocol_GRPC
	}

	return connection.ProtocolEvent{
		Cookie:      connection.Cookie(e.Cookie),
		TimestampNS: e.TimestampNS,
		Protocol:    p,
		IsTLS:       e.IsTLS,
	}
}

// socketHostnameAttr represents the attributes within the socket_hostname_event_t struct.
type socketHostnameAttr struct {
	TimestampNs uint64
	ConnID      connPIDID
	Cookie      uint64 // Socket cookie
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
	Cookie uint64  // 8 bytes
	Size   uint32  // 4 bytes
	_      [4]byte // 4 bytes padding to align to 8 bytes
}

// socketTLSServerHelloAttr represents the attributes within the socket_tls_server_hello_attr_t struct.
type socketTLSServerHelloAttr struct {
	Cookie uint64  // 8 bytes
	Size   uint32  // 4 bytes
	_      [4]byte // 4 bytes padding to align to 8 bytes
}
