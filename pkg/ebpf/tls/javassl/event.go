package javassl

import "github.com/qpoint-io/qtap/pkg/connection"

// events represents process event types
type events uint64

// this must align with the events enum in bpf/tap/javassl.bpf.c
const (
	EVENT_DATA events = iota + 1
	EVENT_CORRELATE
	EVENT_SOCKET_CLOSED
)

// data type represents the data type of the event
type dataType uint32

// this must align with the data type enum in bpf/tap/javassl.bpf.c
const (
	DATA_TYPE_PLAINTEXT dataType = iota + 1
	DATA_TYPE_ENCRYPTED
)

// ssl engine state represents the state of the ssl engine
type sslEngineState uint32

// this must align with the ssl engine state enum in bpf/tap/javassl.bpf.c
const (
	SSL_ENGINE_STATE_UNKNOWN sslEngineState = iota + 1
	SSL_ENGINE_STATE_KNOWN
	SSL_ENGINE_STATE_IGNORED
)

// direction represents the direction of the event
type direction uint32

// this must align with the direction enum in bpf/tap/javassl.bpf.c
const (
	DIRECTION_INGRESS direction = iota
	DIRECTION_EGRESS
)

func (d direction) String() string {
	return []string{"Ingress", "Egress"}[d]
}

func (d direction) ToConnectionDirection() connection.Direction {
	return []connection.Direction{"ingress", "egress"}[d]
}

// event is the base struct for all events
type event struct {
	Type events
}

// data events via SSLEngine wrap/unwrap uprobes
type dataEvent struct {
	Timestamp uint64
	Pid       uint32
	_         [4]byte // padding to align with the c struct
	SessionId uint64
	Direction direction
	DataType  dataType
	MsgSize   uint32
	_         [4]byte // padding to align with the c struct
}

// correlate events via SSLEngine syscall probes
type correlateEvent struct {
	Timestamp uint64
	Pid       uint32
	Fd        int32
	Cookie    uint64
	Direction direction
	MsgSize   uint32
}

// socket closed event
type socketClosedEvent struct {
	Pid uint32
	Fd  int32
}
