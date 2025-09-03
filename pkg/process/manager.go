package process

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/synq"
	"github.com/qpoint-io/qtap/pkg/telemetry/metrics"
	"github.com/sourcegraph/conc"
	"go.uber.org/zap"
)

// Receiver is the interface for the process manager
//
//go:generate go tool go.uber.org/mock/mockgen -destination ./mocks/receiver.go -package mocks . Receiver
type Receiver interface {
	RegisterProcess(p *Process) error
	UnregisterProcess(pid, exitCode int) error
}

// Eventer is the interface for the process eventer
//
//go:generate go tool go.uber.org/mock/mockgen -destination ./mocks/eventer.go -package mocks . Eventer
type Eventer interface {
	Start() error
	Stop() error
	Register(Receiver)
	SetMeta(p *Process) error
}

type Manager struct {
	Logger      *zap.Logger
	procEventer Eventer

	// observers
	Observers []Observer

	// env mask
	envMask *synq.Map[string, bool]

	// env tags maps env var keys to tags for setting those tags
	// with the value of the env var
	envTags []config.EnvTag

	// internal
	mu    sync.Mutex
	procs *synq.Map[int, *Process]
	// processWaiters is a map of pid to channels that are waiting for the process to be discovered
	processWaiters map[int][]chan *Process
}

func NewProcessManager(logger *zap.Logger, procEventer Eventer) *Manager {
	pm := &Manager{
		Logger:         logger,
		procEventer:    procEventer,
		procs:          synq.NewMap[int, *Process](),
		envMask:        synq.NewMap[string, bool](),
		processWaiters: make(map[int][]chan *Process),
	}

	if procEventer != nil {
		procEventer.Register(pm)
	}

	metrics.NewSystemGaugeFunc("observers_active", "The number of observers currently being tracked",
		func() float64 {
			return float64(len(pm.Observers))
		},
	)

	return pm
}

func (m *Manager) RegisterProcess(p *Process) error {
	return m.addProc(p)
}

func (m *Manager) UnregisterProcess(pid, exitCode int) error {
	proc, exists := m.procs.Load(pid)
	if !exists {
		return nil
	}

	proc.ExitCode = exitCode

	return m.removeProc(proc)
}

func (m *Manager) Get(pid int) *Process {
	m.mu.Lock()
	defer m.mu.Unlock()

	// fetch the process by pid
	process, _ := m.procs.Load(pid)

	// return the process
	return process
}

// Await gets a process by pid. If it exists, it is returned immediately.
// Otherwise, it will wait for it to be discovered and then return it.
// The context can be used to set a timeout.
func (m *Manager) Await(ctx context.Context, pid int) (*Process, error) {
	m.mu.Lock()
	if proc, exists := m.procs.Load(pid); exists {
		m.mu.Unlock()
		return proc, nil
	}

	waiter := make(chan *Process)
	m.processWaiters[pid] = append(m.processWaiters[pid], waiter)
	m.mu.Unlock()

	// wait for the process to be discovered or the context to be done
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case p := <-waiter:
		return p, nil
	}
}

func (m *Manager) Observe(observer Observer) {
	m.Observers = append(m.Observers, observer)
}

func (m *Manager) MaskEnvVars(envVars []string) {
	for _, envVar := range envVars {
		m.envMask.Store(envVar, true)
	}
}

func (m *Manager) Start() error {
	// add QPOINT_STRATEGY to the env mask
	m.envMask.Store(QpointStrategyEnvVar, true)
	m.envMask.Store(QpointTagsEnvVar, true)

	// sync with /proc
	if err := m.preloadProcs(); err != nil {
		return fmt.Errorf("syncing with /proc: %w", err)
	}

	// start ebpf eventer
	err := m.procEventer.Start()
	if err != nil {
		return fmt.Errorf("starting process eventer: %w", err)
	}

	return nil
}

func (m *Manager) SetConfig(cfg *config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// remove the old env tags from the env mask
	for _, old := range m.envTags {
		m.envMask.Delete(old.Env)
	}

	if cfg != nil && cfg.Tap != nil {
		m.envTags = cfg.Tap.EnvTags
	}

	// set the env tags in the env mask
	for _, envTag := range m.envTags {
		m.envMask.Store(envTag.Env, true)
	}

	// update the filters
	m.updateFilters(cfg)
}

func (m *Manager) Stop() error {
	// stop the process eventer
	return m.procEventer.Stop()
}

func (m *Manager) preloadProcs() error {
	// load all of the procs
	snap, err := AllProcesses()
	if err != nil {
		return fmt.Errorf("reading processes: %w", err)
	}

	// Process new and existing processes
	for _, proc := range snap {
		if _, exists := m.procs.Load(proc.Pid); !exists {
			// if the process was created before qpoint, we know that it
			// was not scanned by qpoint before
			proc.PredatesQpoint = true

			if err := m.addProc(proc); err != nil {
				return fmt.Errorf("adding process: %w", err)
			}
			if err := m.procEventer.SetMeta(proc); err != nil {
				return fmt.Errorf("setting meta: %w", err)
			}
		}
	}

	return nil
}

func (m *Manager) addProc(p *Process) error {
	p.envTags = m.envTags

	var procChanged bool
	proc, exists := m.procs.Load(p.Pid)
	if exists {
		if p.Exe != proc.Exe {
			procChanged = true
		}
		if p.Binary != proc.Binary {
			procChanged = true
		}
		if !slices.Equal(p.Args, proc.Args) {
			procChanged = true
		}
		processRenamedTotal.WithLabelValues(getProcessLabels(p)...).Inc()

		// replace the process
		p = proc
	}

	// discover the process
	if err := p.Discover("/proc", m.envMask); err != nil {
		if _, ok := p.checkProcessError(err); ok {
			// this happens when processes are exiting quickly, we can ignore
			return nil
		}
		m.Logger.Debug("failed to discover process", zap.Int("pid", p.Pid), zap.Error(err))
		return nil
	}

	if !procChanged {
		// report metrics
		labels := getProcessLabels(p)
		processOpenTotal.WithLabelValues(labels...).Inc()
		processActiveTotal.WithLabelValues(labels...).Inc()
	}

	// lock the registry
	m.mu.Lock()
	defer m.mu.Unlock()

	// add to the map
	m.procs.Store(p.Pid, p)

	// notify the waiters
	waiters, ok := m.processWaiters[p.Pid]
	if ok {
		delete(m.processWaiters, p.Pid)
		go func() {
			for _, waiter := range waiters {
				select {
				case waiter <- p:
				default:
				}
			}
		}()
	}

	// initialize the observers
	go m.initProcObservers(p, procChanged)

	return nil
}

func (m *Manager) initProcObservers(p *Process, replace bool) {
	// if the process has already exited, ignore
	if p.Exited() {
		return
	}

	// we use a wait group to ensure the observers have time to complete
	// because they all share the same instance of the ELF file which is
	// cached in memory.
	var wg conc.WaitGroup

	for _, observer := range m.Observers {
		wg.Go(func() {
			if p.Exited() {
				return
			}

			var err error
			if replace {
				err = observer.ProcessReplaced(p)
			} else {
				err = observer.ProcessStarted(p)
			}

			if err != nil {
				// short-lived processes often terminate before observers can fully initialize,
				// this is common and the observers are aware that this happens to we can ignore
				if _, ok := p.checkProcessError(err); ok {
					// m.Logger.Debug("checking process error for exit type", zap.Int("pid", p.Pid), zap.String("error_type", d), zap.Error(err))
					return
				}

				m.Logger.Error("notifying observer of process start/replace", zap.Error(err))
			}
		})
	}
	if p := wg.WaitAndRecover(); p != nil {
		m.Logger.Error("panic observed during process start/replace", zap.Error(p.AsError()))
	}
}

func (m *Manager) removeProc(p *Process) error {
	// report metrics
	labels := getProcessLabels(p)
	processCloseTotal.WithLabelValues(append(labels, strconv.Itoa(p.ExitCode))...).Inc()
	processActiveTotal.WithLabelValues(labels...).Dec()
	processDuration.WithLabelValues(append(labels, strconv.Itoa(p.ExitCode))...).Observe(time.Since(p.startTime).Seconds())

	// acquire a lock
	m.mu.Lock()
	defer m.mu.Unlock()

	// close the process
	if err := p.Close(); err != nil {
		m.Logger.Error("closing process", zap.Error(err))
	}

	// inform the observers
	for _, observer := range m.Observers {
		go func() {
			if err := observer.ProcessStopped(p); err != nil {
				m.Logger.Error("notifying observer of process stop", zap.Error(err))
			}
		}()
	}

	// remove the entry
	m.procs.Delete(p.Pid)

	// // debug
	// if p.ContainerID != "root" {
	// 	m.Logger.Debug("process removed",
	// 		zap.Int("pid", p.Pid),
	// 		zap.String("comm", p.Comm),
	// 		zap.String("exe", p.Exe),
	// 		zap.String("container_id", p.ContainerID),
	// 		zap.String("pod_id", p.PodID),
	// 		zap.Int("total_procs", m.procs.Len()),
	// 	)
	// }

	return nil
}
