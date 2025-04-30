package process

import (
	"fmt"
	"slices"
	"sync"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/synq"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"github.com/sourcegraph/conc"
	"go.uber.org/zap"
)

// Receiver is the interface for the process manager
//
//go:generate go tool go.uber.org/mock/mockgen -destination ./mocks/receiver.go -package mocks . Receiver
type Receiver interface {
	RegisterProcess(p *Process) error
	UnregisterProcess(pid int) error
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
}

func NewProcessManager(logger *zap.Logger, procEventer Eventer) *Manager {
	pm := &Manager{
		Logger:      logger,
		procEventer: procEventer,
		procs:       synq.NewMap[int, *Process](),
		envMask:     synq.NewMap[string, bool](),
	}

	trackActiveProcessCount(pm.procs.Len)

	if procEventer != nil {
		procEventer.Register(pm)
	}

	telemetry.ObservableGauge("tap_process_observers",
		func() float64 {
			return float64(len(pm.Observers))
		},
		telemetry.WithDescription("The number of observers currently being tracked"),
	)

	telemetry.ObservableGauge("tap_process_procs",
		func() float64 {
			return float64(pm.procs.Len())
		},
		telemetry.WithDescription("The number of processes currently being tracked"),
	)

	return pm
}

func (m *Manager) RegisterProcess(p *Process) error {
	return m.addProc(p)
}

func (m *Manager) UnregisterProcess(pid int) error {
	proc, exists := m.procs.Load(pid)
	if !exists {
		return nil
	}

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
		incrementProcessRenamed()

		// replace the process
		p = proc
	}

	if !procChanged {
		// if a process existed but changed name we already have an add count increment for it
		incrementProcessAdd()
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

	// lock the registry
	m.mu.Lock()
	defer m.mu.Unlock()

	// add to the map
	m.procs.Store(p.Pid, p)

	// initialize the observers
	go m.initProcObservers(p, procChanged)

	// debug
	// if p.ContainerID != "root" {
	// 	m.Logger.Debug("process discovered",
	// 		zap.Int("pid", p.Pid),
	// 		zap.String("exe", p.Exe),
	// 		zap.String("container_id", p.ContainerID),
	// 		zap.Uint64("root_id", p.RootID),
	// 		zap.String("pod_id", p.PodID),
	// 		zap.Int("total_procs", m.procs.Len()),
	// 		zap.String("root_fs", p.RootFS()),
	// 	)
	// }

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
	// increment the process stopped counter
	incrementProcessRemove()

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
