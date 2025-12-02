package tls

import (
	"context"
	"fmt"
	"io"
	"strings"

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
	scanner *TargetScanner

	// final observer called after all probes have run
	// TODO: replace with a more tls-focused interface
	final process.Observer

	// procAttachments tracks process attachments by PID
	// These should be closed on the ProcessStopped signal.
	procAttachments *synq.Map[int, *procAttachment]

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
	exePath string
	closer  io.Closer
}

// NewTlsManager creates a new TlsManager with the given legacy probes.
func NewTlsManager(logger *zap.Logger, scanner *TargetScanner) *TlsManager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &TlsManager{
		logger:  logger,
		scanner: scanner,
		ctx:     ctx,
		cancel:  cancel,

		procAttachments: synq.NewMap[int, *procAttachment](),
		containers:      synq.NewMap[string, io.Closer](),
		containerMu:     synq.NewMutexMap[string](),
	}

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

func (m *TlsManager) SetFinalObserver(o process.Observer) {
	m.final = o
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
				return fmt.Errorf("detaching probes for pid %d: %w", pid, err)
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
				return fmt.Errorf("detaching container %s: %w", id, err)
			}
			return nil
		})
		return true
	})

	if err := m.scanner.Close(); err != nil {
		return fmt.Errorf("closing scanner: %w", err)
	}

	return eg.Wait().ErrorOrNil()
}

// ProcessStarted scans a process binary and attaches applicable TLS probes.
// It runs all scanners concurrently and caches results by binary hash.
// Returns a combined Detacher for all attached probes.
func (m *TlsManager) ProcessStarted(ctx context.Context, proc *process.Process) error {
	if proc.Exe == "" {
		return nil // not a real process
	}

	ctx, span := tracer.WithoutCancel(ctx, "TlsManager.ProcessStarted")
	defer span.End()
	span.SetAttributes(
		attribute.Int("pid", proc.Pid),
		attribute.String("exe", proc.Exe),
		attribute.String("container_id", proc.ContainerID),
	)

	// Start scanning the container for shared libraries
	go m.ContainerStarted(ctx, proc.ContainerID, proc.Root)

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

	if m.final != nil {
		// TODO: implement ProcessReplaced interrupt for final observer
		if e := m.final.ProcessStarted(ctx, proc); e != nil {
			m.logger.Error("starting process on final observer", zap.Error(e))
		}
	}

	return nil
}

// ProcessReplaced handles when a process's binary is replaced (via exec())
func (m *TlsManager) ProcessReplaced(ctx context.Context, proc *process.Process) error {
	if proc.Exe == "" {
		return nil // not a real process
	}

	ctx, span := tracer.WithoutCancel(ctx, "TlsManager.ProcessReplaced") //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()
	span.SetAttributes(attribute.Int("pid", proc.Pid))

	if m.final != nil {
		if e := m.final.ProcessReplaced(ctx, proc); e != nil {
			m.logger.Error("replacing process on final observer", zap.Error(e))
		}
	}

	attachment, exists := m.procAttachments.Load(proc.Pid)
	if !exists {
		// ProcessStarted did not run for some reason. Rescan.
		// TODO: this can happen if the process is replaced before the scan is complete.
		// in this case we need to interrupt the previous attach (keep the scan running) and start a new one.
		return m.scanAndAttachProcess(ctx, proc)
	}

	ll := m.logger.With(zap.Int("pid", proc.Pid))
	span.SetAttributes(attribute.String("old_exe_path", attachment.exePath))

	if attachment.exePath != proc.PidExe {
		// the path has changed. Rescan.
		ll.Debug("executable path has changed, rescanning process",
			zap.String("old_exe_path", attachment.exePath),
			zap.String("new_exe_path", proc.PidExe),
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

	ctx, span := tracer.WithoutCancel(ctx, "TlsManager.ProcessStopped") //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()
	span.SetAttributes(attribute.Int("pid", proc.Pid))

	err := m.detachProcess(ctx, proc.Pid)
	if err != nil {
		return fmt.Errorf("detaching process: %w", err)
	}

	if m.final != nil {
		if e := m.final.ProcessStopped(ctx, proc); e != nil {
			m.logger.Error("stopping process on final observer", zap.Error(e))
		}
	}
	return nil
}

// scanAndAttachProcess scans a process binary and attaches applicable TLS probes.
func (m *TlsManager) scanAndAttachProcess(ctx context.Context, proc *process.Process) error {
	/*

		TODO:

		1. get attach token, invalidate any previous ones
		2. scan (target scanner will cache)
		3. if attach token is still valid, attach and return

	*/

	res, err := m.scanner.Scan(ctx, proc.PidExe)
	if err != nil {
		return err
	}

	if strings.Contains(proc.Binary, "go") {
		m.logger.Info("go detected, dumping scan result", zap.Any("result", res))
		for probeName, probeRes := range res.ProbeResults {
			m.logger.Info("probe result", zap.String("probe_name", probeName), zap.Any("result", probeRes))
		}
	}

	closer, err := m.scanner.Attach(ctx, proc.Pid, proc.PidExe, res)
	if err != nil {
		return err
	}

	m.procAttachments.Store(proc.Pid, &procAttachment{
		exePath: proc.PidExe,
		closer:  closer,
	})
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

	closer, err := m.scanAndAttachContainer(ctx, id, root)
	// mark container as scanned
	m.containers.Store(id, closer)
	if err != nil {
		// NOTE: perhaps we should track the number of failed attempts and allow one retry before giving up.
		m.logger.Error("failed to scan container", zap.String("container_id", id), zap.Error(err))
	}
}

// scanAndAttachContainer scans a container for shared libraries
func (m *TlsManager) scanAndAttachContainer(ctx context.Context, id, root string) (io.Closer, error) {
	ctx, span := tracer.Start(m.ctx, "TlsManager.scanAndAttachContainer",
		trace.WithLinks(trace.LinkFromContext(ctx)),
		trace.WithAttributes(attribute.String("container_id", id)),
	) //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()

	res, err := m.scanner.ScanContainer(ctx, id, root)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return m.scanner.AttachContainer(ctx, res)
}
