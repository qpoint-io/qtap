package socket

import (
	"bytes"
	"encoding/binary"
	"sync"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/tlsutils"
	"go.uber.org/zap"
)

var socketEventPool = sync.Pool{
	New: func() interface{} {
		return new(socketEvent)
	},
}

var readerEventPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Reader)
	},
}

var attrPool = sync.Pool{
	New: func() interface{} {
		return new(attr)
	},
}

var hostnameAttrProol = sync.Pool{
	New: func() interface{} {
		return new(socketHostnameAttr)
	},
}

var socketProtoEventPool = sync.Pool{
	New: func() interface{} {
		return new(socketProtoEvent)
	},
}

var tlsClientHelloAttrPool = sync.Pool{
	New: func() interface{} {
		return new(socketTLSClientHelloAttr)
	},
}

func (m *SocketEventManager) readEvent(record *ringbuf.Record) error {
	// get our reader from the pool
	r := readerEventPool.Get().(*bytes.Reader)
	defer readerEventPool.Put(r)

	// reset the reader with the raw sample from the record
	r.Reset(record.RawSample)

	// get our event from the pool
	event := socketEventPool.Get().(*socketEvent)
	defer socketEventPool.Put(event)

	// read the event from the reader
	if err := binary.Read(r, binary.NativeEndian, event); err != nil {
		m.logger.Error("failed to parse event", zap.Error(err))
		return nil
	}

	switch t := event.Type; t {
	case socketEvents_OPEN:
		m.handleSocketOpenEvent(r)
	case socketEvents_CLOSE:
		m.handleSocketCloseEvent(r)
	case socketEvents_DATA:
		m.handleSocketDataEvent(r)
	case socketEvents_PROTO:
		m.handleSocketProtoEvent(r)
	case socketEvents_HOSTNAME:
		m.handleSocketHostnameEvent(r)
	case socketEvents_TLS_CLIENT_HELLO:
		m.handleSocketTLSClientHelloEvent(r)
	}

	return nil
}

func (m *SocketEventManager) handleSocketOpenEvent(r *bytes.Reader) {
	var e socketOpenEvent

	if err := binary.Read(r, binary.NativeEndian, &e); err != nil {
		m.logger.Error("failed to parse event", zap.Error(err))
		return
	}

	e.Local.Port = fixPortEndianness(binary.NativeEndian, e.Local.Port)
	e.Remote.Port = fixPortEndianness(binary.NativeEndian, e.Remote.Port)

	m.eventHandler.HandleEvent(e.buildConnOpenEvent())
}

func (m *SocketEventManager) handleSocketCloseEvent(r *bytes.Reader) {
	var e socketCloseEvent
	if err := binary.Read(r, binary.NativeEndian, &e); err != nil {
		m.logger.Error("failed to parse event", zap.Error(err))
		return
	}

	m.eventHandler.HandleEvent(e.buildConnCloseEvent())
}

func (m *SocketEventManager) handleSocketDataEvent(r *bytes.Reader) {
	attr := attrPool.Get().(*attr)
	defer attrPool.Put(attr)

	// First, read the attr part
	if err := binary.Read(r, binary.NativeEndian, attr); err != nil {
		m.logger.Error("failed to parse event (attr)", zap.Error(err))
		return
	}

	// Read the message content
	msg := make([]byte, attr.MsgSize)

	if _, err := r.Read(msg); err != nil {
		m.logger.Error("failed to parse event (msg)", zap.Error(err))
		return
	}

	event := connection.DataEvent{
		Cookie:   connection.Cookie(attr.Cookie),
		Size:     int(attr.MsgSize),
		Position: int(attr.Pos),
		Data:     msg,
	}

	// Set the direction
	switch attr.Direction {
	case 0:
		event.Direction = connection.Ingress
		// event.Direction = Ingress
	case 1:
		event.Direction = connection.Egress
		// event.Direction = Egress
	}

	m.eventHandler.HandleEvent(event)
}

func (m *SocketEventManager) handleSocketProtoEvent(r *bytes.Reader) {
	e := socketProtoEventPool.Get().(*socketProtoEvent)
	defer socketProtoEventPool.Put(e)

	if err := binary.Read(r, binary.NativeEndian, e); err != nil {
		m.logger.Error("failed to parse event", zap.Error(err))
		return
	}

	m.eventHandler.HandleEvent(e.buildConnectionProtocolEvent())
}

func (m *SocketEventManager) handleSocketHostnameEvent(r *bytes.Reader) {
	attr := hostnameAttrProol.Get().(*socketHostnameAttr)
	defer hostnameAttrProol.Put(attr)

	// First, read the attr part
	if err := binary.Read(r, binary.NativeEndian, attr); err != nil {
		m.logger.Error("failed to parse event (attr)", zap.Error(err))
		return
	}

	// Read the message content
	msg := make([]byte, attr.HostnameLen)

	if _, err := r.Read(msg); err != nil {
		m.logger.Error("failed to parse event (hostname)", zap.Error(err))
		return
	}

	m.eventHandler.HandleEvent(connection.HostnameEvent{
		Cookie: connection.Cookie(attr.Cookie),
		Name:   string(msg),
	})
}

func (m *SocketEventManager) handleSocketTLSClientHelloEvent(r *bytes.Reader) {
	attr := tlsClientHelloAttrPool.Get().(*socketTLSClientHelloAttr)
	defer tlsClientHelloAttrPool.Put(attr)

	if err := binary.Read(r, binary.NativeEndian, attr); err != nil {
		m.logger.Error("failed to parse event (attr)", zap.Error(err))
		return
	}
	// Read the message content
	msg := make([]byte, attr.Size)

	if _, err := r.Read(msg); err != nil {
		m.logger.Error("failed to read event (tls handshake)", zap.Any("attr", attr), zap.Error(err))
		return
	}

	h, ok := tlsutils.ParseClientHello(msg)
	if !ok {
		m.logger.Error("failed to parse event (tls handshake)", zap.Any("attr", attr))
		return
	}

	m.eventHandler.HandleEvent(connection.TLSClientHelloEvent{
		Cookie: connection.Cookie(attr.Cookie),
		Msg:    h,
	})
}

// fixPortEndianness recovers are uint16 that was in big endian format
// and read in native endian format recovering the original value
func fixPortEndianness(bo binary.ByteOrder, val uint16) uint16 {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, val)
	return bo.Uint16(b)
}
