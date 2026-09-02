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
	probes [1]common.Probe

	// ring buffer reader for cert events
	rdCertEvents *ringbuf.Reader

	// observers
	Observers []Observer

	// process manager
	processManager *process.Manager

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
	m.Observers = append(m.Observers, observer)
}

func (c *CaManager) Start(ctx context.Context) error {
	ctx, span := tracer.WithoutCancel(ctx, "CaManager.Start")
	defer span.End()

	if c.strategy == InjectStrategyEbpf && c.certObjs != nil {
		// open a ring buffer reader
		rdCertEvents, err := ringbuf.NewReader(c.certObjs.CertEvents)
		if err != nil {
			return fmt.Errorf("creating cert event reader: %w", err)
		}
		c.rdCertEvents = rdCertEvents

		// read cert events
		go c.readCertEvents(ctx)

		// define probes
		c.probes = [1]common.Probe{
			// transparently intercept cert reads
			common.NewKprobe(c.certObjs.MonitorCertOpenEntry, openat2Functions...),
		}

		// attach probes
		for _, probe := range c.probes {
			if err := probe.Attach(); err != nil {
				return fmt.Errorf("attaching probe %s: %w", probe.ID(), err)
			}
		}
	} else {
		// open a ring buffer reader
		rdCertEvents, err := ringbuf.NewReader(c.tapObjs.CertEvents)
		if err != nil {
			return fmt.Errorf("creating cert event reader: %w", err)
		}
		c.rdCertEvents = rdCertEvents

		// read cert events
		go c.readCertEvents(context.WithoutCancel(ctx))

		// define probes
		c.probes = [1]common.Probe{
			// monitor cert reads
			common.NewKprobe(c.tapObjs.MonitorCertOpenEntry, openat2Functions...),
		}

		// attach probes
		for _, probe := range c.probes {
			if err := probe.Attach(); err != nil {
				return fmt.Errorf("attaching probe %s: %w", probe.ID(), err)
			}
		}
	}

	return nil
}

func (c *CaManager) Stop() error {
	_, span := tracer.Start(context.TODO(), "CaManager.Stop")
	defer span.End()

	// close all probes
	for _, probe := range c.probes {
		if err := probe.Detach(); err != nil {
			return fmt.Errorf("detaching probe %s: %w", probe.ID(), err)
		}
	}

	// iterate over all containers and cleanup
	c.containers.Iter(func(key uint64, value *Container) bool {
		if err := value.Cleanup(); err != nil {
			c.logger.Error("cleaning up cert injector for container",
				zap.Uint64("root_id", key),
				zap.Error(err),
			)
		}
		return true
	})

	return nil
}

func (c *CaManager) ProcessStarted(ctx context.Context, p *process.Process) error {
	ctx, span := tracer.WithoutCancel(ctx, "CaManager.ProcessStarted") //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()

	// ignore specific runc processes
	if strings.HasPrefix(p.Binary, "runc") {
		return nil
	}

	// fetch the container
	container, exists := c.containers.Load(p.RootID)

	// init container
	if !exists {
		container = NewContainer(p.RootID, p.Pid, c.caBytes, c.strategy, c.logger, c.certObjs, c.tapObjs, c.handleCertInjected, c.handleCertRemoved)
		c.containers.Store(p.RootID, container)
	}

	// add the process to the container
	if err := container.AddProcess(p.Pid); err != nil {
		// don't scan multiple times for the same process
		if errors.Is(err, ErrPIDExists) {
			return nil
		}
		return fmt.Errorf("adding process to container: %w", err)
	}

	// ensure the process strategy is forward or proxy
	if p.Strategy != process.StrategyForward && p.Strategy != process.StrategyProxy {
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

func (c *CaManager) ProcessStopped(ctx context.Context, p *process.Process) error {
	ctx, span := tracer.WithoutCancel(ctx, "CaManager.ProcessStopped") //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()

	// fetch the container
	container, exists := c.containers.Load(p.RootID)
	if !exists {
		return nil
	}

	// decrement the container process count
	container.RemoveProcess(p.Pid)

	// if the container has no more processes, remove it
	if container.IsEmpty() {
		if err := container.Cleanup(); err != nil {
			c.logger.Error("cleaning up cert injector for container",
				zap.Uint64("root_id", p.RootID),
				zap.Error(err),
			)
		}
		c.containers.Delete(p.RootID)
	}

	return nil
}

func (c *CaManager) readCertEvents(ctx context.Context) {
	for {
		record := recordPool.Get().(*ringbuf.Record)
		err := c.rdCertEvents.ReadInto(record)
		if err != nil {
			recordPool.Put(record)

			if errors.Is(err, os.ErrClosed) {
				break
			}
			c.logger.Error("failed to read from buffer", zap.Error(err))
		}

		err = c.readEvent(ctx, record)
		if err != nil {
			recordPool.Put(record)

			c.logger.Error("failed to read event", zap.Error(err))
		}

		recordPool.Put(record)
	}
}

func (c *CaManager) handleCertRead(pid int, filename string) error {
	// fetch the process
	p := c.processManager.Get(pid)
	if p == nil {
		return nil
	}

	// notify observers
	for _, observer := range c.Observers {
		go func() {
			if err := observer.CertRead(p, filename); err != nil {
				c.logger.Error("notifying observer of cert read", zap.Error(err))
			}
		}()
	}

	return nil
}

func (c *CaManager) handleCertInjected(pid int, path string, rootID uint64) {
	// fetch the process
	p := c.processManager.Get(pid)
	if p == nil {
		return
	}

	// notify observers
	for _, observer := range c.Observers {
		go func() {
			if err := observer.CertInjected(p, path, rootID); err != nil {
				c.logger.Error("notifying observer of cert injected", zap.Error(err))
			}
		}()
	}
}

func (c *CaManager) handleCertRemoved(pid int, path string, rootID uint64) {
	// fetch the process
	p := c.processManager.Get(pid)
	if p == nil {
		return
	}

	// notify observers
	for _, observer := range c.Observers {
		go func() {
			if err := observer.CertRemoved(p, path, rootID); err != nil {
				c.logger.Error("notifying observer of cert removed", zap.Error(err))
			}
		}()
	}
}
