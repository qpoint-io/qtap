package tls

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/hashicorp/go-multierror"
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/synq"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"github.com/qpoint-io/qtap/pkg/telemetry/metrics"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

var tracer = telemetry.Tracer()

// TlsManager monitors processes and containers and ensures they are scanned and attached to the correct probes.
// It bridges between the process manager (via process.Observer) and the target scanner.
type TlsManager struct {
	logger  *zap.Logger
	scanner TargetScanner

	observer TlsObserver

	// procAttachments tracks process attachments by PID
	// These should be closed on the ProcessStopped signal.
	procAttachments  *synq.Map[int, *procAttachment]
	procAttachTokens *tokenProvider[int]

	// containers tracks scanned containers by ID.
	// These should be closed when the manager is stopped.
	containers *synq.Map[string, io.Closer]
	// containerMu is used to ensure that we don't scan the same container multiple times concurrently.
	containerMu *synq.MutexMap[string]

	// ctx is canceled when the manager is stopped
	ctx    context.Context
	cancel context.CancelFunc
}

// procAttachment tracks a process that has been attached to.
type procAttachment struct {
	// localExe is the path to the executable of the process relative to the container's root filesystem.
	// This path should not be used for scanning or attaching.
	localExe string
	closer   io.Closer
}

// NewTlsManager creates a new TlsManager with the given options.
func NewTlsManager(logger *zap.Logger, scanner TargetScanner) *TlsManager {
	m := newTlsManager(logger, scanner)

	metrics.NewSystemGaugeFunc("active_process_attachments", "The number of active process attachments",
		func() float64 {
			return float64(m.procAttachments.Len())
		},
	)

	metrics.NewSystemGaugeFunc("active_container_attachments", "The number of active container attachments",
		func() float64 {
			return float64(m.containers.Len())
		},
	)

	return m
}

func newTlsManager(logger *zap.Logger, scanner TargetScanner) *TlsManager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &TlsManager{
		logger:  logger,
		scanner: scanner,
		ctx:     ctx,
		cancel:  cancel,

		procAttachments:  synq.NewMap[int, *procAttachment](),
		procAttachTokens: newTokenProvider[int](),
		containers:       synq.NewMap[string, io.Closer](),
		containerMu:      synq.NewMutexMap[string](),
	}

	return m
}

func (m *TlsManager) Close() error {
	m.cancel()

	var eg multierror.Group

	eg.Go(func() error {
		var err error
		m.procAttachments.Iter(func(pid int, attachment *procAttachment) bool {
			if attachment.closer == nil {
				return true
			}

			if e := attachment.closer.Close(); e != nil {
				err = fmt.Errorf("detaching probes for pid %d: %w", pid, e)
				return false
			}

			return true
		})
		return err
	})

	eg.Go(func() error {
		var err error
		m.containers.Iter(func(id string, closer io.Closer) bool {
			if closer == nil {
				return true
			}
			if e := closer.Close(); e != nil {
				err = fmt.Errorf("detaching container %s: %w", id, e)
				return false
			}
			return true
		})
		return err
	})

	eg.Go(func() error {
		if err := m.scanner.Close(); err != nil {
			return fmt.Errorf("closing scanner: %w", err)
		}
		return nil
	})

	return eg.Wait().ErrorOrNil()
}

// ProcessStarted scans a process binary and attaches applicable TLS probes.
// It runs all scanners concurrently and caches results by binary hash.
// Returns a combined Detacher for all attached probes.
func (m *TlsManager) ProcessStarted(ctx context.Context, proc *process.Process) error {
	if proc.Exe == "" {
		return nil // not a real process
	}

	ctx, span := tracer.WithRemoteCancel(ctx, m.ctx, "TlsManager.ProcessStarted")
	defer span.End()
	span.SetAttributes(
		attribute.Int("pid", proc.Pid),
		attribute.String("exe", proc.Exe),
		attribute.String("container_id", proc.ContainerID),
	)

	// Start scanning the container for shared libraries
	containerScan := make(chan struct{})
	go func() {
		m.ContainerStarted(ctx, proc.ContainerID, proc.Root)
		close(containerScan)
	}()
	defer func() {
		<-containerScan
	}()

	if proc.Exited() {
		return nil
	}
	if proc.Strategy != process.StrategyObserve {
		return nil
	}
	if proc.IsFiltered(config.FilterLevel_TLS) {
		return nil
	}

	err := m.scanAndAttachProcess(ctx, proc)
	if err != nil {
		return fmt.Errorf("scanning and attaching process: %w", err)
	}

	return nil
}

// ProcessReplaced handles when a process's binary is replaced (via exec())
func (m *TlsManager) ProcessReplaced(ctx context.Context, proc *process.Process) error {
	if proc.Exe == "" {
		return nil // not a real process
	}

	ctx, span := tracer.WithRemoteCancel(ctx, m.ctx, "TlsManager.ProcessReplaced")
	defer span.End()
	span.SetAttributes(attribute.Int("pid", proc.Pid))

	attachment, exists := m.procAttachments.Load(proc.Pid)
	if !exists {
		// ProcessStarted did not run. This is likely because the process was replaced before the scan was complete.
		// We will start a new scan and attach.
		return m.scanAndAttachProcess(ctx, proc)
	}

	ll := m.logger.With(zap.Int("pid", proc.Pid))
	span.SetAttributes(attribute.String("old_exe_path", attachment.localExe))

	if attachment.localExe != proc.Exe {
		// the path has changed. Rescan.
		ll.Debug("executable path has changed, rescanning process",
			zap.String("old_exe_path", attachment.localExe),
			zap.String("new_exe_path", proc.Exe),
		)

		if err := m.detachProcess(ctx, proc.Pid); err != nil {
			return fmt.Errorf("detaching existing probes: %w", err)
		}
		return m.scanAndAttachProcess(ctx, proc)
	}

	// technically it is possible that the old binary was moved to a different path and a new one replaced it.
	// this is very unlikely so we will ignore this case for now.

	return nil
}

// ProcessStopped cleans up when a process exits
func (m *TlsManager) ProcessStopped(ctx context.Context, proc *process.Process) error {
	if proc.Exe == "" {
		return nil // not a real process
	}

	ctx, span := tracer.WithRemoteCancel(ctx, m.ctx, "TlsManager.ProcessStopped")
	defer span.End()
	span.SetAttributes(attribute.Int("pid", proc.Pid))

	err := m.detachProcess(ctx, proc.Pid)
	if err != nil {
		return fmt.Errorf("detaching process: %w", err)
	}

	if m.observer != nil {
		if e := m.observer.ProcessStopped(ctx, proc); e != nil {
			m.logger.Error("error notifying tls observer of process stop", zap.Error(e))
		}
	}

	return nil
}

// scanAndAttachProcess scans a process binary and attaches applicable TLS probes.
func (m *TlsManager) scanAndAttachProcess(ctx context.Context, proc *process.Process) error {
	ctx, span := tracer.Start(ctx, "TlsManager.scanAndAttachProcess",
		trace.WithAttributes(attribute.Int("pid", proc.Pid)),
	)
	defer span.End()

	attachToken := m.procAttachTokens.Get(proc.Pid)

	if m.observer != nil {
		if e := m.observer.ProcessScanStarted(ctx, proc); e != nil {
			m.logger.Error("error notifying tls observer of process scan", zap.Error(e))
		}
	}

	res, err := m.scanner.Scan(ctx, &ExeScannable{
		Path:    proc.PidExe,
		Cmdline: proc.FullCmd(),
		Root:    proc.RootFS(),
	})
	if err != nil {
		return err
	}

	if !attachToken() {
		// a newer scan has started. we will not attach as the data we have is now likely outdated.
		return nil
	}

	closer, err := m.scanner.Attach(ctx, proc.Pid, proc.PidExe, res)
	if err != nil {
		return err
	}

	var detectedProbes []string
	for _, res := range res.ProbeResults {
		if res.ProbeDetected() {
			detectedProbes = append(detectedProbes, res.ProbeName())
		}
	}
	proc.SetDetectedTLSProbeTypes(detectedProbes)

	m.procAttachments.Store(proc.Pid, &procAttachment{
		localExe: proc.Exe,
		closer:   closer,
	})

	if m.observer != nil {
		if e := m.observer.ProcessAttachCompleted(ctx, proc); e != nil {
			m.logger.Error("error notifying tls observer of process attach", zap.Error(e))
		}
	}

	return nil
}

// detachProcess removes and closes all attachments for a PID
func (m *TlsManager) detachProcess(ctx context.Context, pid int) error {
	ctx, span := tracer.Start(ctx, "TlsManager.detachProcess") //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()
	span.SetAttributes(attribute.Int("pid", pid))

	attachment, exists := m.procAttachments.Load(pid)
	if !exists {
		return nil
	}
	span.SetAttributes(attribute.Bool("was_attached", attachment.closer != nil))

	if attachment.closer != nil {
		if err := attachment.closer.Close(); err != nil {
			return fmt.Errorf("detaching probes: %w", err)
		}
	}
	m.procAttachments.Delete(pid)

	return nil
}

func (m *TlsManager) ContainerStarted(ctx context.Context, id, root string) {
	mu := m.containerMu.Get(id)
	mu.Lock()
	defer mu.Unlock()

	_, scanned := m.containers.Load(id)
	if scanned {
		// already scanned
		return
	}

	if m.observer != nil {
		if e := m.observer.ContainerScanStarted(ctx, id); e != nil {
			m.logger.Error("error notifying tls observer of container scan", zap.Error(e))
		}
	}

	closer, err := m.scanAndAttachContainer(ctx, id, root)
	// mark container as scanned
	m.containers.Store(id, closer)
	if err != nil {
		// NOTE: perhaps we should track the number of failed attempts and allow one retry before giving up.
		m.logger.Error("failed to scan container", zap.String("container_id", id), zap.Error(err))
	}

	if m.observer != nil {
		if e := m.observer.ContainerAttachCompleted(ctx, id); e != nil {
			m.logger.Error("error notifying tls observer of container attach", zap.Error(e))
		}
	}
}

// scanAndAttachContainer scans a container for shared libraries
func (m *TlsManager) scanAndAttachContainer(ctx context.Context, id, root string) (io.Closer, error) {
	ctx, span := tracer.WithRemoteCancel(ctx, m.ctx, "TlsManager.scanAndAttachContainer",
		trace.WithAttributes(attribute.String("container_id", id)),
	)
	defer span.End()

	res, err := m.scanner.ScanContainer(ctx, id, root)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return m.scanner.AttachContainer(ctx, res)
}

// tokenProviders allows multiple goroutines to coordinate operation
// on a shared resource where only the latest goroutine wins.
type tokenProvider[T comparable] struct {
	mu     sync.Mutex
	tokens map[T]int
}

func (p *tokenProvider[T]) Get(key T) (valid func() bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	token, exists := p.tokens[key]
	if exists {
		token++
	}
	p.tokens[key] = token

	return func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()

		latest, exists := p.tokens[key]
		if !exists || latest != token {
			return false
		}

		// clean up
		delete(p.tokens, key)
		return true
	}
}

func newTokenProvider[T comparable]() *tokenProvider[T] {
	return &tokenProvider[T]{
		tokens: make(map[T]int),
	}
}

type TlsObserver interface {
	ProcessScanStarted(ctx context.Context, proc *process.Process) error
	ProcessAttachCompleted(ctx context.Context, proc *process.Process) error
	ProcessStopped(ctx context.Context, proc *process.Process) error
	ContainerScanStarted(ctx context.Context, id string) error
	ContainerAttachCompleted(ctx context.Context, id string) error
}

func (m *TlsManager) SetObserver(o TlsObserver) {
	m.observer = o
}
