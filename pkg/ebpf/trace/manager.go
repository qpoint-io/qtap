package trace

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/synq"
	"go.uber.org/zap"
)

var (
	recordPool = sync.Pool{
		New: func() interface{} {
			return new(ringbuf.Record)
		},
	}
)

type TraceManager struct {
	// logger
	logger *zap.Logger

	// toggle map
	toggleMap *ebpf.Map

	// event map
	eventMap *ebpf.Map

	// process manager
	procMgr *process.Manager

	// state of toggled processes
	toggled *synq.Map[int, bool]

	// ring buffer reader for trace events
	rdTraceEvents *ringbuf.Reader

	// active trace entries
	activeEntries map[uint64]*TraceEntry

	// embed a default proc observer
	process.DefaultObserver

	// the toggle matcher
	matcher *Matcher
}

func NewTraceManager(logger *zap.Logger, toggleMap *ebpf.Map, eventMap *ebpf.Map, procMgr *process.Manager, toggleQuery string) (*TraceManager, error) {
	matcher, err := NewMatcher(toggleQuery)
	if err != nil {
		return nil, fmt.Errorf("creating toggle matcher: %w", err)
	}

	return &TraceManager{
		logger:        logger,
		toggleMap:     toggleMap,
		eventMap:      eventMap,
		procMgr:       procMgr,
		matcher:       matcher,
		toggled:       synq.NewMap[int, bool](),
		activeEntries: make(map[uint64]*TraceEntry),
	}, nil
}

func (m *TraceManager) Start() error {
	// if toggle query is empty then nothing to do
	if len(m.matcher.GetModuleToggles()) == 0 {
		return nil
	}

	for _, modValue := range m.matcher.GetModuleToggles() {
		// debug
		m.logger.Debug("BPF Trace enabled for module", zap.String("module", modValue))

		// add the mod to the bpf map
		component, ok := QtapComponentFromString(modValue)
		if !ok {
			return fmt.Errorf("invalid component: %s", modValue)
		}
		if err := m.toggleMap.Put(uint32(component), true); err != nil {
			return fmt.Errorf("failed to set trace toggle for component %s: %w", modValue, err)
		}
	}

	// open a ring buffer reader
	rdTraceEvents, err := ringbuf.NewReader(m.eventMap)
	if err != nil {
		return fmt.Errorf("creating trace event reader: %w", err)
	}
	m.rdTraceEvents = rdTraceEvents

	// read trace events
	go m.readTraceEvents()

	return nil
}

func (m *TraceManager) Stop() error {
	// if the ring buffer reader is nil then nothing to do
	// this happens when there's no trace queries
	if m.rdTraceEvents == nil {
		return nil
	}

	// close the ring buffer reader
	if err := m.rdTraceEvents.Close(); err != nil {
		return fmt.Errorf("closing trace event reader: %w", err)
	}

	return nil
}

func (m *TraceManager) ProcessStarted(proc *process.Process) error {
	// nothing to do if we don't have any proc toggles
	if !m.matcher.HasProcToggles() {
		return nil
	}

	// extract the exe from the process
	exe := proc.Exe

	// check if the exe matches any toggles
	if !m.matcher.MatchExe(exe) {
		return nil
	}

	// debug
	m.logger.Debug("BPF Trace enabled for process",
		zap.Int("pid", proc.Pid),
		zap.String("exe", exe),
		zap.String("container_id", proc.ContainerID),
	)

	// add to the bpf map
	if err := m.toggleMap.Put(uint32(proc.Pid), true); err != nil {
		return fmt.Errorf("failed to add process to trace toggle map: %w", err)
	}

	// add the process to the toggled map
	m.toggled.Store(proc.Pid, true)

	return nil
}

func (m *TraceManager) ProcessStopped(proc *process.Process) error {
	// nothing to do if we don't have any proc toggles
	if !m.matcher.HasProcToggles() {
		return nil
	}

	// check to see if the process is in the toggled map
	_, exists := m.toggled.Load(proc.Pid)

	// nothing to do if the process is not toggled
	if !exists {
		return nil
	}

	// delete the process from the bpf map
	if err := m.toggleMap.Delete(uint32(proc.Pid)); err != nil {
		return fmt.Errorf("failed to delete process from trace toggle map: %w", err)
	}

	// remove the process from the toggled map
	m.toggled.Delete(proc.Pid)

	return nil
}

func (m *TraceManager) readTraceEvents() {
	for {
		record := recordPool.Get().(*ringbuf.Record)
		err := m.rdTraceEvents.ReadInto(record)
		if err != nil {
			recordPool.Put(record)

			if errors.Is(err, os.ErrClosed) {
				break
			}
			m.logger.Error("failed to read from buffer", zap.Error(err))
		}

		err = m.readEvent(record)
		if err != nil {
			recordPool.Put(record)

			m.logger.Error("failed to read event", zap.Error(err))
		}

		recordPool.Put(record)
	}
}
