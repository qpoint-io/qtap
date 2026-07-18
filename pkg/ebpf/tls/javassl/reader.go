package javassl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/cilium/ebpf/ringbuf"
	"go.uber.org/zap"
)

var (
	recordPool = sync.Pool{
		New: func() any {
			return new(ringbuf.Record)
		},
	}
	eventPool = sync.Pool{
		New: func() any {
			return new(event)
		},
	}
	readerEventPool = sync.Pool{
		New: func() any {
			return new(bytes.Reader)
		},
	}
	dataEventPool = sync.Pool{
		New: func() any {
			return new(dataEvent)
		},
	}
	correlateEventPool = sync.Pool{
		New: func() any {
			return new(correlateEvent)
		},
	}
	socketClosedEventPool = sync.Pool{
		New: func() any {
			return new(socketClosedEvent)
		},
	}
)

func (m *SslEngineManager) readEvents() {
	for {
		record := recordPool.Get().(*ringbuf.Record)
		err := m.bridge.EventsRingbufferReader.ReadInto(record)
		if err != nil {
			recordPool.Put(record)
			if errors.Is(err, os.ErrClosed) {
				break
			}
			m.logger.Debug("failed to read from java ssl engine events buffer", zap.Error(err))
			continue
		}

		err = m.readEvent(record)
		if err != nil {
			m.logger.Debug("failed to read java ssl engine event", zap.Error(err))
		}

		recordPool.Put(record)
	}
}

func (m *SslEngineManager) readEvent(record *ringbuf.Record) error {
	// get our reader from the pool
	r := readerEventPool.Get().(*bytes.Reader)
	defer readerEventPool.Put(r)

	// reset the reader with the raw sample from the record
	r.Reset(record.RawSample)

	event := eventPool.Get().(*event)
	defer eventPool.Put(event)

	if err := binary.Read(r, binary.NativeEndian, event); err != nil {
		m.logger.Debug("failed to parse java ssl engine event", zap.Error(err))
		return nil
	}

	switch t := event.Type; t {
	case EVENT_DATA:
		return m.readDataEvent(r)
	case EVENT_CORRELATE:
		return m.readCorrelateEvent(r)
	case EVENT_SOCKET_CLOSED:
		return m.readSocketClosedEvent(r)
	}

	return nil
}

func (m *SslEngineManager) readDataEvent(r *bytes.Reader) error {
	dataEvent := dataEventPool.Get().(*dataEvent)
	defer dataEventPool.Put(dataEvent)

	if err := binary.Read(r, binary.NativeEndian, dataEvent); err != nil {
		m.logger.Debug("failed to parse java ssl engine data event", zap.Error(err))
		return nil
	}

	// Read the message content
	msg := make([]byte, dataEvent.MsgSize)

	if _, err := r.Read(msg); err != nil {
		return fmt.Errorf("failed to read message content: %w", err)
	}

	// process the data event
	switch dataEvent.DataType {
	case DATA_TYPE_PLAINTEXT:
		return m.ProcessPlaintextData(dataEvent.Pid, dataEvent.SessionId, dataEvent.Direction, msg)
	case DATA_TYPE_ENCRYPTED:
		return m.ProcessEncryptedData(dataEvent.Pid, dataEvent.SessionId, dataEvent.Direction, msg)
	}

	return nil
}

func (m *SslEngineManager) readCorrelateEvent(r *bytes.Reader) error {
	correlateEvent := correlateEventPool.Get().(*correlateEvent)
	defer correlateEventPool.Put(correlateEvent)

	if err := binary.Read(r, binary.NativeEndian, correlateEvent); err != nil {
		m.logger.Debug("failed to parse java ssl engine correlate event", zap.Error(err))
		return nil
	}

	// Read the message content
	msg := make([]byte, correlateEvent.MsgSize)
	if _, err := r.Read(msg); err != nil {
		return fmt.Errorf("failed to read message content: %w", err)
	}

	// process the correlate event
	return m.ProcessCorrelationData(correlateEvent.Pid, correlateEvent.Fd, correlateEvent.Cookie, correlateEvent.Direction, msg)
}

func (m *SslEngineManager) readSocketClosedEvent(r *bytes.Reader) error {
	socketClosedEvent := socketClosedEventPool.Get().(*socketClosedEvent)
	defer socketClosedEventPool.Put(socketClosedEvent)

	if err := binary.Read(r, binary.NativeEndian, socketClosedEvent); err != nil {
		m.logger.Debug("failed to parse java ssl engine socket closed event", zap.Error(err))
		return nil
	}

	return m.ProcessSocketClosed(socketClosedEvent.Pid, socketClosedEvent.Fd)
}
