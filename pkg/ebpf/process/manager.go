package process

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/ringbuf"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

const (
	cacheSize = 1024
)

var recordPool = sync.Pool{
	New: func() interface{} {
		return new(ringbuf.Record)
	},
}

type Manager struct {
	logger   *zap.Logger
	reciever process.Receiver
	cache    *lru.Cache[int32, *process.Process]

	// bridge to the bpf probes
	tracepoints []*common.Tracepoint
	rb          *ringbuf.Reader
	metaMap     *ebpf.Map
}

var tracer = telemetry.Tracer()

func New(logger *zap.Logger, mmap *ebpf.Map, rb *ringbuf.Reader, tps []*common.Tracepoint) *Manager {
	cache, err := lru.New[int32, *process.Process](cacheSize)
	if err != nil {
		panic(err)
	}

	return &Manager{
		logger:      logger,
		rb:          rb,
		metaMap:     mmap,
		tracepoints: tps,
		cache:       cache,
	}
}

func (m *Manager) Start(ctx context.Context) error {
	ctx = context.WithoutCancel(ctx)
	ctx, span := tracer.Start(ctx, "Manager.Start")
	defer span.End()

	// attach the tracepoints
	for _, tracepoint := range m.tracepoints {
		if err := tracepoint.Attach(); err != nil {
			return fmt.Errorf("opening tracepoint %s/%s: %w", tracepoint.Group, tracepoint.Name, err)
		}
	}

	// start the proc event reader
	go m.readProcEvents(ctx)

	return nil
}

func (m *Manager) Stop() error {
	// close the reader
	m.rb.Close()

	// detach the tracepoints
	for _, tracepoint := range m.tracepoints {
		if err := tracepoint.Detach(); err != nil {
			return fmt.Errorf("detaching tracepoint %s/%s: %w", tracepoint.Group, tracepoint.Name, err)
		}
	}

	return nil
}

func (m *Manager) Register(r process.Receiver) {
	m.reciever = r
}

func (m *Manager) readProcEvents(ctx context.Context) {
	ctx = context.WithoutCancel(ctx)
	for {
		if ctx.Err() != nil {
			m.logger.Error("context cancelled", zap.Error(ctx.Err()))
			break
		}

		record := recordPool.Get().(*ringbuf.Record)
		err := m.rb.ReadInto(record)
		if err != nil {
			recordPool.Put(record)
			if errors.Is(err, os.ErrClosed) {
				break
			}
			m.logger.Error("failed to read from proc buffer", zap.Error(err))
			continue
		}

		if err := m.readProcEvent(ctx, record); err != nil {
			m.logger.Error("failed to read proc event", zap.Error(err))
			continue
		}

		recordPool.Put(record)
	}
}

var (
	eventPool = sync.Pool{
		New: func() interface{} {
			return new(event)
		},
	}
	readerEventPool = sync.Pool{
		New: func() interface{} {
			return new(bytes.Reader)
		},
	}
)

func (m *Manager) readProcEvent(ctx context.Context, record *ringbuf.Record) error {
	ctx = context.WithoutCancel(ctx)
	ctx, span := tracer.Start(ctx, "readProcEvent",
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

	event := eventPool.Get().(*event)
	defer eventPool.Put(event)

	if err := binary.Read(r, binary.NativeEndian, event); err != nil {
		m.logger.Error("failed to parse proc event", zap.Error(err))
		return nil
	}

	switch t := event.Type; t {
	case EVENT_EXEC_START:
		span.SetAttributes(attribute.String("type", "exec_start"))
		return m.handleExecStartEvent(ctx, r)
	case EVENT_EXEC_ARGV:
		span.SetAttributes(attribute.String("type", "exec_argv"))
		return m.handleExecArgvEvent(ctx, r)
	case EVENT_EXEC_END:
		span.SetAttributes(attribute.String("type", "exec_end"))
		return m.handleExecEndEvent(ctx, r)
	case EVENT_EXIT:
		span.SetAttributes(attribute.String("type", "exit"))
		return m.handleExitEvent(ctx, r)
	default:
		err := fmt.Errorf("unknown event type: %d", t)
		span.RecordError(err)
		m.logger.Error("failed to parse proc event", zap.Error(err))
		return err
	}
}

func (m *Manager) SetMeta(p *process.Process) error {
	if p == nil {
		return nil
	}

	// if we're already done, don't set the meta
	if p.Exited() {
		if err := m.metaMap.Delete(uint32(p.Pid)); err != nil {
			m.logger.Error("failed to delete process meta", zap.Error(err))
		}
		return nil
	}

	// ensure the meta map is set
	if m.metaMap == nil {
		return nil
	}

	var containerId [13]byte
	copy(containerId[:], p.ContainerID)
	containerId[12] = 0

	// create a process_meta struct to match the C struct
	d := struct {
		RootID         uint64
		QpointStrategy uint32
		Filter         uint8
		TlsOk          bool
		ContainerID    [13]byte
		_              [5]byte
	}{
		RootID:         p.RootID,
		QpointStrategy: uint32(p.Strategy),
		Filter:         p.Filter(),
		TlsOk:          p.TlsOk(),
		ContainerID:    containerId,
	}

	// update the BPF map
	return m.metaMap.Put(uint32(p.Pid), d)
}
