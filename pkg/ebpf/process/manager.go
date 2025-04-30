package process

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	"github.com/qpoint-io/qtap/pkg/process"
	"go.uber.org/zap"
)

const (
	cacheTTL  = time.Millisecond * 50
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
	cache    *expirable.LRU[int32, *process.Process]

	// bridge to the bpf probes
	tracepoints []*common.Tracepoint
	rb          *ringbuf.Reader
	metaMap     *ebpf.Map
}

func New(logger *zap.Logger, mmap *ebpf.Map, rb *ringbuf.Reader, tps []*common.Tracepoint) *Manager {
	return &Manager{
		logger:      logger,
		rb:          rb,
		metaMap:     mmap,
		tracepoints: tps,
		cache:       expirable.NewLRU[int32, *process.Process](cacheSize, nil, cacheTTL),
	}
}

func (m *Manager) Start() error {
	// attach the tracepoints
	for _, tracepoint := range m.tracepoints {
		if err := tracepoint.Attach(); err != nil {
			return fmt.Errorf("opening tracepoint %s/%s: %w", tracepoint.Group, tracepoint.Name, err)
		}
	}

	// start the proc event reader
	go m.readProcEvents()

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

func (m *Manager) readProcEvents() {
	for {
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

		err = m.readProcEvent(record)
		if err != nil {
			m.logger.Error("failed to read proc event", zap.Error(err))
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

func (m *Manager) readProcEvent(record *ringbuf.Record) error {
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
		return m.handleExecStartEvent(r)
	case EVENT_EXEC_ARGV:
		return m.handleExecArgvEvent(r)
	case EVENT_EXEC_END:
		return m.handleExecEndEvent(r)
	case EVENT_EXIT:
		return m.handleExitEvent(r)
	}

	return nil
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
