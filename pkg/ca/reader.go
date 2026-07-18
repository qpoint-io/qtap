package ca

import (
	"bytes"
	"context"
	"encoding/binary"
	"sync"
	"unsafe"

	"github.com/cilium/ebpf/ringbuf"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

var (
	readerEventPool = sync.Pool{
		New: func() any {
			return new(bytes.Reader)
		},
	}

	certEventPool = sync.Pool{
		New: func() any {
			return new(CertEventMeta)
		},
	}

	certReadEventPool = sync.Pool{
		New: func() any {
			return new(CertReadEvent)
		},
	}
)

func (c *CaManager) readEvent(ctx context.Context, record *ringbuf.Record) error {
	ctx, span := tracer.Start(context.TODO(), "CaManager.readEvent", //nolint:ineffassign,wastedassign,staticcheck
		trace.WithLinks(trace.LinkFromContext(ctx)),
		trace.WithNewRoot(),
		trace.WithSpanKind(trace.SpanKindConsumer),
	)
	defer span.End()

	// get our reader from the pool
	r := readerEventPool.Get().(*bytes.Reader)
	defer readerEventPool.Put(r)

	// reset the reader with the raw sample from the record
	r.Reset(record.RawSample)

	// get our event from the pool
	event := certEventPool.Get().(*CertEventMeta)
	defer certEventPool.Put(event)

	// read the event from the reader
	if err := binary.Read(r, binary.NativeEndian, event); err != nil {
		c.logger.Error("failed to parse event", zap.Error(err))
		return nil
	}

	// handle cert read events
	if event.Type == CertRead {
		return c.readCertReadEvent(r, event)
	}

	return nil
}

func (c *CaManager) readCertReadEvent(r *bytes.Reader, _ *CertEventMeta) error {
	// create a msg event
	event := certReadEventPool.Get().(*CertReadEvent)
	defer certReadEventPool.Put(event)

	// read the event
	if err := binary.Read(r, binary.NativeEndian, event); err != nil {
		c.logger.Error("failed to read cert read event", zap.Error(err))
		return nil
	}

	// convert the filename to a string using the known size
	filename := string(bytesToString(event.File[:event.FileSize-1]))

	// handle the cert read
	if err := c.handleCertRead(int(event.Pid), filename); err != nil {
		c.logger.Error("failed to handle cert read", zap.Error(err))
		return nil
	}

	return nil
}

// Helper function to convert []int8 to []byte
func bytesToString(b []int8) []byte {
	return *(*[]byte)(unsafe.Pointer(&b))
}
