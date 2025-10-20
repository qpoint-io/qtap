// Package connection defines various event types related to network connections.
//
// Event Priority Explanation:
// Each event type has a QueuePriority() method that returns an integer
// representing its priority in the event queue. Lower numbers indicate
// higher priority. The priorities are as follows:
//
// - 1: Highest priority, used for ProtocolEvent, IgnoreConnectionEvent,
//      ErrorEvent, HoldEvent, and HandlerTypeEvent. These events are
//      critical for connection management and error handling.
//
// - 3: Medium-high priority, used for HostnameEvent and
//      OriginalDestinationEvent. These events provide important
//      connection metadata.
//
// - 5: Medium priority, used for DataEvent. This represents the actual
//      data transfer, which is important but not as time-sensitive as
//      connection management events.
//
// - 10: Lowest priority, used for CloseEvent. This is typically the final
//       event in a connection's lifecycle.
//
// If the QueuePriority() method is not defined, the event will always be
// put at the end of the queue.

package connection

import (
	"github.com/qpoint-io/qtap/pkg/qnet"
	"github.com/qpoint-io/qtap/pkg/tlsutils"
)

type OpenEvent struct {
	ConnKey
	TimestampNS  uint64
	PID          uint32
	TGID         uint32
	Local        qnet.NetAddr
	Remote       qnet.NetAddr
	Source       Source
	SocketType   SocketType
	IsRedirected bool
}

type CloseEvent struct {
	ConnKey
	TimestampNS uint64
	WrBytes     int64 // Total number of bytes written on that connection
	RdBytes     int64 // Total number of bytes read on that connection
}

func (e CloseEvent) QueuePriority() int {
	return 10
}

type DataEvent struct {
	ConnKey
	Direction
	Size     int
	Position int
	Data     []byte
}

func (e DataEvent) QueuePriority() int {
	return 5
}

type ProtocolEvent struct {
	ConnKey
	TimestampNS uint64
	Protocol    Protocol
	IsTLS       bool
}

func (e ProtocolEvent) QueuePriority() int {
	return 1
}

type HostnameEvent struct {
	ConnKey
	Name string
}

func (e HostnameEvent) QueuePriority() int {
	return 3
}

func (e HostnameEvent) String() string {
	return e.Name
}

type OriginalDestinationEvent struct {
	ConnKey
	Destination qnet.NetAddr
}

func (e OriginalDestinationEvent) QueuePriority() int {
	return 3
}

type ErrorEventType string

var (
	ErrType_ClientTLSHandshake        ErrorEventType = "client_tls_handshake"
	ErrType_ClientTLSHandshakeTimeout ErrorEventType = "client_tls_handshake_timeout"
)

type ErrorEvent struct {
	ConnKey
	Type    ErrorEventType
	Message string
}

func (e ErrorEvent) QueuePriority() int {
	return 1
}

type HandlerTypeEvent struct {
	ConnKey
	Type HandlerType
}

func (e HandlerTypeEvent) QueuePriority() int {
	return 1
}

// DoneEvent is used to signal that a connection has been finalized
// by an external claimant (e.g. the forwarder).
// DoneEvent does not have a QueuePriority() method so it's always
// processed last.
type DoneEvent struct {
	ConnKey
}

type TLSClientHelloEvent struct {
	ConnKey
	Msg *tlsutils.ClientHello
}

func (e TLSClientHelloEvent) QueuePriority() int {
	return 1
}
