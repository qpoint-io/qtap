package tls

import (
	"context"
	"errors"
	"fmt"
	"io"

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
	procAttachments *synq.Map[int, *procAttachment]
	procCoordinator *KeyedCoordinator[int]

	// containers tracks scanned containers by ID.
	// These should be closed when the manager is stopped.
	containers *synq.Map[string, io.Closer]
	// containerGroup ensures that we don't scan the same container multiple times concurrently.
	containerGroup synq.SingleFlight[string, any]

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

// processInfo contains the fields we need from *process.Process as that one is mutable.
type processInfo struct {
	pid         int
	exe         string
	containerID string
	root        string
	pidExe      string
	cmdline     []string
	strategy    process.QpointStrategy

	// IMPORTANT: proc might be mutated after creation. users should mind this.
	proc *process.Process
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

		procAttachments: synq.NewMap[int, *procAttachment](),
		procCoordinator: NewKeyedCoordinator[int](),
		containers:      synq.NewMap[string, io.Closer](),
	}

	return m
}

func (m *TlsManager) Close() error {
	m.cancel()

	var eg multierror.Group

	m.procAttachments.Iter(func(pid int, attachment *procAttachment) bool {
		if attachment.closer == nil {
			return true
		}

		eg.Go(func() error {
			if err := attachment.closer.Close(); err != nil {
				return fmt.Errorf("closing process attachment for pid %d: %w", pid, err)
			}
			return nil
		})

		return true
	})

	m.containers.Iter(func(id string, closer io.Closer) bool {
		if closer == nil {
			return true
		}
		eg.Go(func() error {
			if err := closer.Close(); err != nil {
				return fmt.Errorf("closing container attachment for id %s: %w", id, err)
			}
			return nil
		})
		return true
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
	// copy the process info asap because *process.Process is mutable
	info := newProcessInfo(proc)

	if info.exe == "" {
		return nil // not a real process
	}
	if m.ctx.Err() != nil {
		return m.ctx.Err()
	}

	ctx, span := tracer.WithRemoteCancel(ctx, m.ctx, "TlsManager.ProcessStarted")
	defer span.End()
	span.SetAttributes(
		attribute.Int("pid", info.pid),
		attribute.String("exe", info.exe),
		attribute.String("container_id", info.containerID),
	)

	attachToken := m.procCoordinator.Start(info.pid)

	// Start scanning the container for shared libraries
	containerScan := make(chan struct{})
	go func() {
		m.containerStarted(ctx, info.containerID, info.root)
		close(containerScan)
	}()
	defer func() {
		<-containerScan
	}()

	if proc.Exited() {
		return nil
	}
	if info.strategy != process.StrategyObserve {
		return nil
	}
	if proc.IsFiltered(config.FilterLevel_TLS) {
		return nil
	}

	if m.observer != nil {
		if e := m.observer.ProcessScanStarted(ctx, proc); e != nil {
			m.logger.Debug("error notifying tls observer of process scan", zap.Error(e))
		}
	}
	err := m.scanAndAttachProcess(ctx, info, attachToken)
	if err != nil && !errors.Is(err, ErrOpSuperseded) {
		return fmt.Errorf("scanning and attaching process: %w", err)
	}

	return nil
}

// ProcessReplaced handles when a process's binary is replaced (via exec())
func (m *TlsManager) ProcessReplaced(ctx context.Context, proc *process.Process) error {
	// copy the process info asap because *process.Process is mutable
	info := newProcessInfo(proc)

	if info.exe == "" {
		return nil // not a real process
	}
	if m.ctx.Err() != nil {
		return m.ctx.Err()
	}

	ctx, span := tracer.WithRemoteCancel(ctx, m.ctx, "TlsManager.ProcessReplaced")
	defer span.End()
	span.SetAttributes(attribute.Int("pid", info.pid))

	attachToken := m.procCoordinator.Start(info.pid)
	if m.observer != nil {
		if e := m.observer.ProcessScanStarted(ctx, proc); e != nil {
			m.logger.Debug("error notifying tls observer of process scan", zap.Error(e))
		}
	}

	attachment, exists := m.procAttachments.Load(info.pid)
	if exists {
		if err := m.detachProcess(ctx, info.pid, attachment); err != nil {
			return fmt.Errorf("detaching existing probes: %w", err)
		}
	}

	err := m.scanAndAttachProcess(ctx, info, attachToken)
	if err != nil && !errors.Is(err, ErrOpSuperseded) {
		return err
	}
	return nil
}

// ProcessStopped cleans up when a process exits
func (m *TlsManager) ProcessStopped(ctx context.Context, proc *process.Process) error {
	if proc.Exe == "" {
		return nil // not a real process
	}
	if m.ctx.Err() != nil {
		return m.ctx.Err()
	}

	ctx, span := tracer.WithRemoteCancel(ctx, m.ctx, "TlsManager.ProcessStopped")
	defer span.End()
	span.SetAttributes(attribute.Int("pid", proc.Pid))

	m.procCoordinator.Cleanup(proc.Pid, true)

	var err error
	attachment, exists := m.procAttachments.Load(proc.Pid)
	if exists {
		err = m.detachProcess(ctx, proc.Pid, attachment)
		if err != nil {
			err = fmt.Errorf("detaching process: %w", err)
		}
	}

	if m.observer != nil {
		if e := m.observer.ProcessStopped(ctx, proc); e != nil {
			m.logger.Debug("error notifying tls observer of process stop", zap.Error(e))
		}
	}

	return err
}

// scanAndAttachProcess scans a process binary and attaches applicable TLS probes.
func (m *TlsManager) scanAndAttachProcess(ctx context.Context, proc *processInfo, attachToken *opToken[int]) error {
	ctx, span := tracer.Start(ctx, "TlsManager.scanAndAttachProcess",
		trace.WithAttributes(attribute.Int("pid", proc.pid)),
	)
	defer span.End()

	if attachToken == nil {
		attachToken = m.procCoordinator.Start(proc.pid)
	}

	res, err := m.scanner.Scan(ctx, &ExeScannable{
		Path:    proc.pidExe,
		Cmdline: proc.cmdline,
		Root:    proc.root,
	})
	if err != nil {
		return err
	}

	var closer io.Closer
	err = attachToken.Execute(ctx, func(ctx context.Context) error {
		m.logger.Debug("attaching probes to process",
			zap.Int("pid", proc.pid),
			zap.String("exe", proc.exe),
			zap.String("container_id", proc.containerID),
		)

		var e error
		closer, e = m.scanner.Attach(ctx, &ExeAttachable{
			PID:  proc.pid,
			Path: proc.pidExe,
			Root: proc.root,
		}, res)
		return e
	})
	if err != nil {
		return err
	}

	// attach can take a while, so we need to check again if we are still the latest token
	err = attachToken.Execute(ctx, func(ctx context.Context) error {
		var detectedProbes []string
		for _, res := range res.ProbeResults {
			if res.ProbeDetected() {
				detectedProbes = append(detectedProbes, res.ProbeName())
			}
		}

		proc.proc.SetDetectedTLSProbeTypes(detectedProbes)
		m.procAttachments.Store(proc.pid, &procAttachment{
			localExe: proc.exe,
			closer:   closer,
		})

		if m.observer != nil {
			if e := m.observer.ProcessAttachCompleted(ctx, proc.proc); e != nil {
				m.logger.Debug("error notifying tls observer of process attach", zap.Error(e))
			}
		}
		return nil
	})

	return err
}

// detachProcess removes and closes all attachments for a PID
func (m *TlsManager) detachProcess(ctx context.Context, pid int, attachment *procAttachment) error {
	ctx, span := tracer.Start(ctx, "TlsManager.detachProcess") //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()
	span.SetAttributes(attribute.Int("pid", pid))
	span.SetAttributes(attribute.Bool("was_attached", attachment.closer != nil))

	if attachment.closer != nil {
		if err := attachment.closer.Close(); err != nil {
			return fmt.Errorf("detaching probes: %w", err)
		}
	}
	m.procAttachments.Delete(pid)

	return nil
}

func (m *TlsManager) containerStarted(ctx context.Context, id, root string) {
	if _, scanned := m.containers.Load(id); scanned {
		// already scanned
		return
	}

	// queue a scan. containerGroup ensures that we don't scan the same container multiple times concurrently.
	_, _ = m.containerGroup.Do(id, func() (any, error) {
		// the cache may have been updated by the time our scan is executed.
		// check again:
		if _, scanned := m.containers.Load(id); scanned {
			// already scanned
			return nil, nil
		}

		if m.observer != nil {
			if e := m.observer.ContainerScanStarted(m.ctx, id); e != nil {
				m.logger.Debug("error notifying tls observer of container scan", zap.Error(e))
			}
		}

		closer, err := m.scanAndAttachContainer(ctx, id, root)
		// mark container as scanned
		m.containers.Store(id, closer)
		if err != nil && !errors.Is(err, context.Canceled) {
			// NOTE: perhaps we should track the number of failed attempts and allow one retry before giving up.
			m.logger.Info("failed to scan container", zap.String("container_id", id), zap.Error(err))
		}

		if m.observer != nil {
			if e := m.observer.ContainerAttachCompleted(m.ctx, id); e != nil {
				m.logger.Debug("error notifying tls observer of container attach", zap.Error(e))
			}
		}
		return nil, nil
	})
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

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	return m.scanner.AttachContainer(ctx, res)
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

func newProcessInfo(proc *process.Process) *processInfo {
	if proc == nil {
		return nil
	}
	return &processInfo{
		pid:         proc.Pid,
		exe:         proc.Exe,
		containerID: proc.ContainerID,
		root:        proc.RootFS(),
		pidExe:      proc.PidExe,
		cmdline:     proc.FullCmd(),
		strategy:    proc.Strategy,

		proc: proc,
	}
}
