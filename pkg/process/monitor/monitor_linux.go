// Package monitor constructs Linux process discovery without starting the QTap
// application. It supports one Monitor per Go process.
package monitor

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/qpoint-io/qtap/internal/tap"
	ebpfProcess "github.com/qpoint-io/qtap/pkg/ebpf/process"
	"github.com/qpoint-io/qtap/pkg/process"
	"go.uber.org/zap"
)

// Monitor owns process discovery and its BPF resources. Register observers on the
// embedded Manager before calling Start. Use Monitor.Stop to release resources,
// including when the monitor has been constructed but not started.
//
// Lifecycle calls must not run concurrently. Restarting is not supported.
type Monitor struct {
	*process.Manager
	objects  *tap.TapObjects
	stopOnce sync.Once
	stopErr  error
}

// New loads the existing QTap BPF collection and prepares process discovery.
// It does not attach probes or start discovery. A nil logger disables logging.
// The caller must call Stop even if it never calls Start.
func New(logger *zap.Logger) (*Monitor, error) {
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
	source, err := ebpfProcess.NewFromObjects(logger, objects)
	if err != nil {
		return nil, errors.Join(err, objects.Close())
	}
	return &Monitor{
		Manager: process.NewProcessManager(logger, source),
		objects: objects,
	}, nil
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
