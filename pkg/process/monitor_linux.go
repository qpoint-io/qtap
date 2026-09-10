// Package process discovers and tracks Linux processes and surfaces lifecycle
// callbacks. NewMonitor constructs standalone discovery without starting QTap.
package process

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/qpoint-io/qtap/internal/tap"
	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	"go.uber.org/zap"
)

// Monitor owns process discovery and its BPF resources. Register observers on the
// embedded Manager before calling Start. Use Monitor.Stop to release resources,
// including when the monitor has been constructed but not started.
//
// Lifecycle calls must not run concurrently. Restarting is not supported.
type Monitor struct {
	*Manager
	objects  *tap.TapObjects
	stopOnce sync.Once
	stopErr  error
}

// NewMonitor loads the existing QTap BPF collection and prepares process discovery.
// It does not attach probes or start discovery. A nil logger disables logging.
// The caller must call Stop even if it never calls Start. Constructing a second
// monitor in the same Go process is unsupported, even after stopping the first.
func NewMonitor(logger *zap.Logger) (*Monitor, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	spec, err := tap.LoadTap()
	if err != nil {
		return nil, fmt.Errorf("loading process BPF specification: %w", err)
	}
	pid, ok := spec.Variables["qpid"]
	if !ok {
		return nil, errors.New("process BPF specification has no qpid variable")
	}
	if err := pid.Set(uint32(os.Getpid())); err != nil {
		return nil, fmt.Errorf("setting process BPF pid: %w", err)
	}

	objects := new(tap.TapObjects)
	if err := spec.LoadAndAssign(objects, nil); err != nil {
		return nil, fmt.Errorf("loading process BPF collection: %w", err)
	}
	return newMonitor(logger, objects)
}

// newMonitor takes ownership of an already loaded collection, including on error.
func newMonitor(logger *zap.Logger, objects *tap.TapObjects) (*Monitor, error) {
	source, err := NewEventSource(logger, objects)
	if err != nil {
		return nil, errors.Join(err, objects.Close())
	}
	return &Monitor{
		Manager: NewProcessManager(logger, source),
		objects: objects,
	}, nil
}

// NewEventSource creates a process event source using an already loaded QTap
// collection. The source owns its reader and tracepoint links; the caller retains
// ownership of the collection's maps and programs. Use NewMonitor for standalone
// discovery that also loads and owns the collection.
func NewEventSource(logger *zap.Logger, objs *tap.TapObjects) (Eventer, error) {
	reader, err := ringbuf.NewReader(objs.ProcEvents)
	if err != nil {
		return nil, fmt.Errorf("creating process event reader: %w", err)
	}

	tracepoints := []*common.Tracepoint{
		common.NewTracepoint("syscalls", "sys_enter_execve", objs.SyscallProbeEntryExecve),
		common.NewTracepoint("syscalls", "sys_exit_execve", objs.SyscallProbeRetExecve),
		common.NewTracepoint("syscalls", "sys_enter_execveat", objs.SyscallProbeEntryExecveat),
		common.NewTracepoint("syscalls", "sys_exit_execveat", objs.SyscallProbeRetExecveat),
		common.NewTracepoint("syscalls", "sys_enter_exit_group", objs.SyscallProbeEntryExitGroup),
		common.NewTracepoint("sched", "sched_process_exit", objs.TracepointSchedProcessExit),
	}
	return newEventSource(logger, objs.ProcessMetaMap, reader, tracepoints), nil
}

// Start enumerates existing processes and starts live discovery. If startup
// fails, it releases the monitor's resources. The monitor cannot be restarted.
func (m *Monitor) Start() error {
	if err := m.Manager.Start(); err != nil {
		return errors.Join(err, m.Stop())
	}
	return nil
}

// Stop ends discovery and releases the owned BPF collection. It is safe to call
// again, including after failed startup. Already-dispatched observer callbacks
// may still be running; Stop does not wait for application callback work.
func (m *Monitor) Stop() error {
	m.stopOnce.Do(func() {
		m.stopErr = errors.Join(m.Manager.Stop(), m.objects.Close())
	})
	return m.stopErr
}
