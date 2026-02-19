package trace

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"unsafe"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/qpoint-io/qtap/pkg/qnet"
	"go.uber.org/zap"
)

var (
	readerEventPool = sync.Pool{
		New: func() any {
			return new(bytes.Reader)
		},
	}

	traceEventPool = sync.Pool{
		New: func() any {
			return new(TraceEventMeta)
		},
	}
)

func (m *TraceManager) readEvent(record *ringbuf.Record) error {
	// get our reader from the pool
	r := readerEventPool.Get().(*bytes.Reader)
	defer readerEventPool.Put(r)

	// reset the reader with the raw sample from the record
	r.Reset(record.RawSample)

	// get our event from the pool
	event := traceEventPool.Get().(*TraceEventMeta)
	defer traceEventPool.Put(event)

	// read the event from the reader
	if err := binary.Read(r, binary.NativeEndian, event); err != nil {
		m.logger.Error("failed to parse event", zap.Error(err))
		return nil
	}

	switch event.Type {
	case TraceMsg:
		return m.readMsgEvent(r, event)

	case TraceAttr:
		return m.readAttrEvent(r, event)

	case TraceEnd:
		return m.readEndEvent(r, event)
	}

	return nil
}

func (m *TraceManager) readMsgEvent(r *bytes.Reader, event *TraceEventMeta) error {
	// create a msg event
	var msg TraceMsgEvent

	// read the msg event
	if err := binary.Read(r, binary.NativeEndian, &msg); err != nil {
		m.logger.Error("failed to read msg event", zap.Error(err))
		return nil
	}

	// create a byte slice to read the msg into
	msgBytes := make([]byte, msg.MsgSize-1)

	// read the msg into the byte slice
	if _, err := r.Read(msgBytes); err != nil {
		m.logger.Error("failed to read msg", zap.Error(err))
		return nil
	}

	// convert the msg bytes to a string
	msgString := string(msgBytes)

	// create a new trace entry
	entry := NewTraceEntry(msgString)

	// add the trace entry to the manager
	m.activeEntries[event.Tsid] = entry

	return nil
}

func (m *TraceManager) readAttrEvent(r *bytes.Reader, event *TraceEventMeta) error {
	// get the trace entry
	entry, ok := m.activeEntries[event.Tsid]
	if !ok {
		return nil
	}

	// create a new attribute event
	var attr TraceAttrEvent

	// read the entire struct
	if err := binary.Read(r, binary.NativeEndian, &attr); err != nil {
		m.logger.Error("failed to read eBPF attr event", zap.Error(err))
		return nil
	}

	// convert the title to a string using the known size
	titleString := string(bytesToString(attr.Title[:attr.TitleSize-1]))

	// handle different attribute types
	switch TraceAttrType(attr.AttrType) {
	case TraceString:
		// read the string size
		var strSize uint32
		if err := binary.Read(r, binary.NativeEndian, &strSize); err != nil {
			m.logger.Error("failed to read string size", zap.Error(err))
			return nil
		}

		// read the padding (12 bytes)
		if _, err := r.Read(make([]byte, 12)); err != nil {
			m.logger.Error("failed to read padding", zap.Error(err))
			return nil
		}

		// read the string data
		stringData := make([]byte, strSize-1)
		if _, err := r.Read(stringData); err != nil {
			m.logger.Error("failed to read string data", zap.Error(err))
			return nil
		}

		entry.AddField(zap.String(titleString, string(stringData)))
	case TraceInt:
		var intValue int64
		if err := binary.Read(r, binary.NativeEndian, &intValue); err != nil {
			m.logger.Error("failed to read int value", zap.Error(err))
			return nil
		}

		// add the field
		entry.AddField(zap.Int64(titleString, intValue))

		// if this is "pid" then we need to lookup the process name
		if titleString == "pid" && m.procMgr != nil {
			proc := m.procMgr.Get(int(intValue))
			if proc != nil {
				entry.AddField(zap.String("exe", proc.Exe))
			}
		}
	case TraceUint:
		var uintValue uint64
		if err := binary.Read(r, binary.NativeEndian, &uintValue); err != nil {
			m.logger.Error("failed to read uint value", zap.Error(err))
			return nil
		}

		entry.AddField(zap.Uint64(titleString, uintValue))
	case TracePointer:
		var ptrValue uint64
		if err := binary.Read(r, binary.NativeEndian, &ptrValue); err != nil {
			m.logger.Error("failed to read pointer value", zap.Error(err))
			return nil
		}

		entry.AddField(zap.String(titleString, fmt.Sprintf("%#016x", ptrValue)))
	case TraceBool:
		var boolValue bool
		if err := binary.Read(r, binary.NativeEndian, &boolValue); err != nil {
			m.logger.Error("failed to read bool value", zap.Error(err))
			return nil
		}

		entry.AddField(zap.Bool(titleString, boolValue))
	case TraceIP4:
		var ip4Value uint32
		if err := binary.Read(r, binary.BigEndian, &ip4Value); err != nil {
			m.logger.Error("failed to read IP4 value", zap.Error(err))
			return nil
		}

		ip := make([]byte, 4)
		binary.BigEndian.PutUint32(ip, ip4Value)
		entry.AddField(zap.String(titleString, qnet.IPString(net.IP(ip))))
	case TraceIP6:
		var ip6Value [4]uint32
		if err := binary.Read(r, binary.BigEndian, &ip6Value); err != nil {
			m.logger.Error("failed to read IP6 value", zap.Error(err))
			return nil
		}

		ip := make([]byte, 16)
		for i := range 4 {
			binary.BigEndian.PutUint32(ip[i*4:], ip6Value[i])
		}
		entry.AddField(zap.String(titleString, qnet.IPString(net.IP(ip))))
	}

	return nil
}

func (m *TraceManager) readEndEvent(_ *bytes.Reader, event *TraceEventMeta) error {
	// get the trace entry
	entry, ok := m.activeEntries[event.Tsid]
	if !ok {
		return nil
	}

	// print the trace entry
	entry.Print(m.logger)

	// remove the trace entry from the manager
	delete(m.activeEntries, event.Tsid)

	return nil
}

// Helper function to convert []int8 to []byte
func bytesToString(b []int8) []byte {
	return *(*[]byte)(unsafe.Pointer(&b))
}
