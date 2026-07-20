package ca

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/qpoint-io/qtap/internal/tap"
	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/synq"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.uber.org/zap"
)

var (
	openat2Functions = []string{
		"do_sys_openat2",       // Original name
		"__x64_sys_openat2",    // x86_64 variant
		"__x64_do_sys_openat2", // Another possible variant
		"sys_openat2",          // Traditional name without prefix
	}

	recordPool = sync.Pool{
		New: func() any {
			return new(ringbuf.Record)
		},
	}
)

var tracer = telemetry.Tracer()

var ErrCaManagerStopped = errors.New("CA manager cannot be started after it has stopped")

type caManagerState uint8

const (
	caManagerNew caManagerState = iota
	caManagerStarting
	caManagerStarted
	caManagerStopping
	caManagerStopped
)

type certEventReader interface {
	ReadInto(*ringbuf.Record) error
	Close() error
}

type processGetter interface {
	Get(int) *process.Process
}

type CaManager struct {
	// ca bytes to inject
	caBytes []byte

	// inject strategy
	strategy InjectStrategy

	// logger
	logger *zap.Logger

	// map of containers by root filesystem ID
	containers *synq.Map[uint64, *Container]

	// bpf objects
	certObjs *tap.CertsObjects
	tapObjs  *tap.TapObjects

	// bpf probes
	probes []common.Probe

	// ring buffer reader for cert events
	rdCertEvents certEventReader
	readerCancel context.CancelFunc
	readerWG     sync.WaitGroup

	// observers
	Observers []Observer

	// process manager
	processManager processGetter

	// lifecycle state
	startStopMu  sync.Mutex
	lifecycleMu  sync.Mutex
	state        caManagerState
	inFlight     int
	idle         chan struct{}
	startCleanup bool
	observerMu   sync.Mutex
	rootLocksMu  sync.Mutex
	rootLocks    *synq.Map[uint64, *sync.Mutex]
	pidLocksMu   sync.Mutex
	pidLocks     *synq.Map[int, *sync.Mutex]

	// resource factories are overridden by tests to avoid kernel dependencies.
	openReader func() (certEventReader, error)
	newProbes  func() []common.Probe

	// embed a default process observer
	process.DefaultObserver
}

func NewCaManager(
	caBytes []byte,
	strategy InjectStrategy,
	logger *zap.Logger,
	certObjs *tap.CertsObjects,
	captureObjs *tap.TapObjects,
	processManager *process.Manager,
) *CaManager {
	return &CaManager{
		caBytes:        caBytes,
		strategy:       strategy,
		logger:         logger,
		certObjs:       certObjs,
		tapObjs:        captureObjs,
		processManager: processManager,
		containers:     synq.NewMap[uint64, *Container](),
	}
}

func (m *CaManager) Observe(observer Observer) {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	m.Observers = append(m.Observers, observer)
}

func (c *CaManager) Start(ctx context.Context) error {
	ctx, span := tracer.WithoutCancel(ctx, "CaManager.Start")
	defer span.End()

	c.startStopMu.Lock()
	defer c.startStopMu.Unlock()

	c.lifecycleMu.Lock()
	switch c.state {
	case caManagerStarted:
		c.lifecycleMu.Unlock()
		return nil
	case caManagerStopping, caManagerStopped:
		c.lifecycleMu.Unlock()
		return ErrCaManagerStopped
	case caManagerNew:
		c.state = caManagerStarting
	case caManagerStarting:
		c.lifecycleMu.Unlock()
		return errors.New("CA manager is already starting")
	}
	c.lifecycleMu.Unlock()

	reader, err := c.createReader()
	if err != nil {
		c.resetStart()
		return fmt.Errorf("creating cert event reader: %w", err)
	}

	probes := c.createProbes()
	attached := make([]common.Probe, 0, len(probes))
	for _, probe := range probes {
		if err := probe.Attach(); err != nil {
			startErr := fmt.Errorf("attaching probe %s: %w", probe.ID(), err)
			c.lifecycleMu.Lock()
			c.rdCertEvents = reader
			c.probes = attached
			c.startCleanup = true
			c.state = caManagerStopping
			c.lifecycleMu.Unlock()

			rollbackErr := c.cleanupResources()
			if rollbackErr == nil {
				c.lifecycleMu.Lock()
				c.startCleanup = false
				c.state = caManagerNew
				c.lifecycleMu.Unlock()
			}
			return errors.Join(startErr, rollbackErr)
		}
		attached = append(attached, probe)
	}

	c.startCertEventReader(ctx, reader, probes)

	return nil
}

func (c *CaManager) Stop() error {
	_, span := tracer.Start(context.TODO(), "CaManager.Stop")
	defer span.End()

	c.startStopMu.Lock()
	defer c.startStopMu.Unlock()

	c.lifecycleMu.Lock()
	if c.state == caManagerStopped {
		c.lifecycleMu.Unlock()
		return c.cleanupContainers()
	}
	c.state = caManagerStopping
	readerCancel := c.readerCancel
	idle := c.idle
	startCleanup := c.startCleanup
	c.lifecycleMu.Unlock()

	if idle != nil {
		<-idle
	}

	if readerCancel != nil {
		readerCancel()
	}

	var errs []error
	if err := c.cleanupResources(); err != nil {
		errs = append(errs, err)
	}
	if !startCleanup {
		if err := c.cleanupContainers(); err != nil {
			errs = append(errs, err)
		}
	}

	c.lifecycleMu.Lock()
	if len(errs) == 0 {
		if startCleanup {
			c.startCleanup = false
			c.state = caManagerNew
		} else {
			c.state = caManagerStopped
		}
	}
	c.lifecycleMu.Unlock()

	return errors.Join(errs...)
}

func (c *CaManager) cleanupContainers() error {
	var errs []error
	c.containers.Iter(func(key uint64, value *Container) bool {
		if err := value.Cleanup(); err != nil {
			errs = append(errs, fmt.Errorf("cleaning container %d: %w", key, err))
		} else {
			c.containers.Delete(key)
		}
		return true
	})
	return errors.Join(errs...)
}

func (c *CaManager) createReader() (certEventReader, error) {
	if c.openReader != nil {
		return c.openReader()
	}
	if c.strategy == InjectStrategyEbpf && c.certObjs != nil {
		return ringbuf.NewReader(c.certObjs.CertsMaps.CertEvents)
	}
	if c.tapObjs == nil {
		return nil, errors.New("tap objects are unavailable")
	}
	return ringbuf.NewReader(c.tapObjs.TapMaps.CertEvents)
}

func (c *CaManager) createProbes() []common.Probe {
	if c.newProbes != nil {
		return c.newProbes()
	}
	if c.strategy == InjectStrategyEbpf && c.certObjs != nil {
		return []common.Probe{common.NewKprobe(c.certObjs.MonitorCertOpenEntry, openat2Functions...)}
	}
	return []common.Probe{common.NewKprobe(c.tapObjs.MonitorCertOpenEntry, openat2Functions...)}
}

func (c *CaManager) cleanupResources() error {
	var errs []error
	if c.rdCertEvents != nil {
		if err := c.rdCertEvents.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing cert event reader: %w", err))
		} else {
			c.rdCertEvents = nil
			c.readerWG.Wait()
			c.readerCancel = nil
		}
	}

	failedProbes := c.probes[:0]
	for _, probe := range c.probes {
		if probe == nil {
			continue
		}
		if err := probe.Detach(); err != nil {
			errs = append(errs, fmt.Errorf("detaching probe %s: %w", probe.ID(), err))
			failedProbes = append(failedProbes, probe)
		}
	}
	c.probes = failedProbes

	return errors.Join(errs...)
}

func (c *CaManager) resetStart() {
	c.lifecycleMu.Lock()
	c.state = caManagerNew
	c.lifecycleMu.Unlock()
}

func (c *CaManager) startCertEventReader(ctx context.Context, reader certEventReader, probes []common.Probe) {
	ctx, cancel := context.WithCancel(ctx)
	c.lifecycleMu.Lock()
	c.rdCertEvents = reader
	c.readerCancel = cancel
	c.probes = probes
	c.readerWG.Add(1)
	c.state = caManagerStarted
	c.lifecycleMu.Unlock()
	go func() {
		defer c.readerWG.Done()
		c.readCertEvents(ctx, reader)
	}()
}

func (c *CaManager) ProcessStarted(ctx context.Context, p *process.Process) error {
	ctx, span := tracer.WithoutCancel(ctx, "CaManager.ProcessStarted") //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()
	if !c.beginWork() {
		return nil
	}
	defer c.endWork()
	pidLock := c.pidLock(p.Pid)
	pidLock.Lock()
	defer pidLock.Unlock()
	rootLock := c.rootLock(p.RootID)
	rootLock.Lock()
	defer rootLock.Unlock()

	// ignore specific runc processes
	if strings.HasPrefix(p.Binary, "runc") {
		return nil
	}

	// fetch the container
	container, exists := c.containers.Load(p.RootID)

	// init container
	if !exists {
		candidate := NewContainer(p.RootID, p.Pid, c.caBytes, c.strategy, c.logger, c.certObjs, c.tapObjs, c.handleCertInjected, c.handleCertRemoved)
		container, _ = c.containers.LoadOrInsert(p.RootID, candidate)
	}

	// ensure the process strategy is forward or proxy
	if p.Strategy != process.StrategyForward && p.Strategy != process.StrategyProxy {
		if err := container.AddProcess(p.Pid); err != nil && !errors.Is(err, ErrPIDExists) {
			return fmt.Errorf("adding process to container: %w", err)
		}
		return nil
	}

	// scan the container
	foundCerts, err := container.Scan(p)
	if err != nil {
		return fmt.Errorf("scanning container: %w", err)
	}

	// if no certs were found, log a warning
	if !foundCerts {
		c.logger.Warn("no custom certs specified",
			zap.String("strategy", p.Strategy.String()),
			zap.String("exe", p.Exe),
		)
	}

	return nil
}

func (c *CaManager) ProcessReplaced(ctx context.Context, p *process.Process) error {
	var errs []error
	if err := p.SetTlsOk(false); err != nil {
		return fmt.Errorf("revoking TLS termination for replaced process: %w", err)
	}
	if err := c.ProcessStopped(ctx, p); err != nil {
		errs = append(errs, fmt.Errorf("removing replaced process: %w", err))
	}
	if err := c.ProcessStarted(ctx, p); err != nil {
		errs = append(errs, fmt.Errorf("adding replacement process: %w", err))
	}
	return errors.Join(errs...)
}

func (c *CaManager) ProcessStopped(ctx context.Context, p *process.Process) error {
	ctx, span := tracer.WithoutCancel(ctx, "CaManager.ProcessStopped") //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()
	if !c.beginWork() {
		return nil
	}
	defer c.endWork()
	pidLock := c.pidLock(p.Pid)
	pidLock.Lock()
	defer pidLock.Unlock()
	rootLock := c.rootLock(p.RootID)
	rootLock.Lock()
	defer rootLock.Unlock()

	// fetch the container
	container, exists := c.containers.Load(p.RootID)
	if !exists {
		return nil
	}

	var errs []error

	// decrement the container process count
	preserveMap := false
	if c.processManager != nil {
		current := c.processManager.Get(p.Pid)
		preserveMap = current != nil && current.RootID != p.RootID
	}
	var removeErr error
	if preserveMap {
		removeErr = container.RemoveProcessPreservingMap(p.Pid)
	} else {
		removeErr = container.RemoveProcess(p.Pid)
	}
	if removeErr != nil {
		errs = append(errs, fmt.Errorf("removing stopped process: %w", removeErr))
	}

	// if the container has no more processes, remove it
	if container.IsEmpty() {
		if err := container.Cleanup(); err != nil {
			errs = append(errs, fmt.Errorf("cleaning container %d: %w", p.RootID, err))
		} else {
			c.containers.Delete(p.RootID)
		}
	}

	return errors.Join(errs...)
}

func (c *CaManager) readCertEvents(ctx context.Context, reader certEventReader) {
	for {
		record := recordPool.Get().(*ringbuf.Record)
		err := reader.ReadInto(record)
		if err != nil {
			recordPool.Put(record)

			if errors.Is(err, os.ErrClosed) {
				return
			}
			c.logger.Error("failed to read from buffer", zap.Error(err))
			continue
		}

		err = c.readEvent(ctx, record)
		if err != nil {
			c.logger.Error("failed to read event", zap.Error(err))
		}

		recordPool.Put(record)
	}
}

func (c *CaManager) handleCertRead(pid int, filename string) error {
	if c.processManager == nil {
		return nil
	}
	// fetch the process
	p := c.processManager.Get(pid)
	if p == nil {
		return nil
	}
	container, exists := c.containers.Load(p.RootID)
	if !exists {
		return nil
	}
	return container.NotifyIfCertificateActive(pid, filename, func() error {
		return c.notifyObservers(func(observer Observer) error {
			return observer.CertRead(p, filename)
		}, "notifying observer of cert read")
	})
}

func (c *CaManager) handleCertInjected(pid int, path string, rootID uint64) error {
	if c.processManager == nil {
		return nil
	}
	// fetch the process
	p := c.processManager.Get(pid)
	if p == nil {
		return nil
	}

	return c.notifyObservers(func(observer Observer) error {
		return observer.CertInjected(p, path, rootID)
	}, "notifying observer of cert injected")
}

func (c *CaManager) handleCertRemoved(pid int, path string, rootID uint64) error {
	if c.processManager == nil {
		return nil
	}
	// fetch the process
	p := c.processManager.Get(pid)
	if p == nil {
		return nil
	}

	return c.notifyObservers(func(observer Observer) error {
		return observer.CertRemoved(p, path, rootID)
	}, "notifying observer of cert removed")
}

func (c *CaManager) notifyObservers(notify func(Observer) error, logMessage string) error {
	if !c.beginWork() {
		return nil
	}
	defer c.endWork()
	c.observerMu.Lock()
	defer c.observerMu.Unlock()

	c.lifecycleMu.Lock()
	if c.state != caManagerStarted {
		c.lifecycleMu.Unlock()
		return nil
	}
	observers := append([]Observer(nil), c.Observers...)
	c.lifecycleMu.Unlock()

	var errs []error
	for _, observer := range observers {
		if err := notify(observer); err != nil {
			c.logger.Error(logMessage, zap.Error(err))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (c *CaManager) rootLock(rootID uint64) *sync.Mutex {
	c.rootLocksMu.Lock()
	if c.rootLocks == nil {
		c.rootLocks = synq.NewMap[uint64, *sync.Mutex]()
	}
	locks := c.rootLocks
	c.rootLocksMu.Unlock()

	lock, _ := locks.LoadOrInsert(rootID, &sync.Mutex{})
	return lock
}

func (c *CaManager) pidLock(pid int) *sync.Mutex {
	c.pidLocksMu.Lock()
	if c.pidLocks == nil {
		c.pidLocks = synq.NewMap[int, *sync.Mutex]()
	}
	locks := c.pidLocks
	c.pidLocksMu.Unlock()

	lock, _ := locks.LoadOrInsert(pid, &sync.Mutex{})
	return lock
}

func (c *CaManager) beginWork() bool {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if c.state != caManagerStarted {
		return false
	}
	c.addWorkLocked(1)
	return true
}

func (c *CaManager) addWorkLocked(count int) {
	if count == 0 {
		return
	}
	if c.inFlight == 0 {
		c.idle = make(chan struct{})
	}
	c.inFlight += count
}

func (c *CaManager) endWork() {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	c.inFlight--
	if c.inFlight == 0 {
		close(c.idle)
	}
}
