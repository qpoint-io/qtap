package tls

import (
	"context"
	"fmt"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"github.com/qpoint-io/qtap/pkg/telemetry/metrics"
	"go.uber.org/zap"
)

// TlsProbe is the legacy interface for TLS probes.
// New probes should implement the Scanner interface instead.
type TlsProbe interface {
	Start(ctx context.Context) error
	Stop() error
	process.Observer
}

// TlsManager manages TLS probe lifecycle and coordinates scanning.
// It bridges between the process manager (via process.Observer) and the orchestrator.
type TlsManager struct {
	logger       *zap.Logger
	orchestrator *Orchestrator

	// Final observer called after all probes have run
	final process.Observer
}

var tracer = telemetry.Tracer()

// NewTlsManager creates a new TlsManager with the given legacy probes.
func NewTlsManager(logger *zap.Logger, orchestrator *Orchestrator) *TlsManager {
	return &TlsManager{
		logger:       logger,
		orchestrator: orchestrator,
	}
}

func (m *TlsManager) Start(ctx context.Context) error {
	// Start any legacy adapters that need initialization
	for _, scanner := range m.orchestrator.probes {
		if adapter, ok := scanner.(*LegacyProbeAdapter); ok {
			if err := adapter.Probe().Start(ctx); err != nil {
				return fmt.Errorf("starting legacy probe %s: %w", adapter.Name(), err)
			}
		}
	}

	metrics.NewSystemGaugeFunc("probes_started", "The number of probes that have started",
		func() float64 {
			return float64(len(m.orchestrator.probes))
		},
	)

	metrics.NewSystemGaugeFunc("scan_cache_entries", "The number of entries in the scan cache",
		func() float64 {
			return float64(m.orchestrator.Cache().Len())
		},
	)

	return nil
}

func (m *TlsManager) SetFinalObserver(o process.Observer) {
	m.final = o
}

func (m *TlsManager) Stop() error {
	// Stop any legacy adapters
	for _, scanner := range m.orchestrator.probes {
		if adapter, ok := scanner.(*LegacyProbeAdapter); ok {
			if err := adapter.Stop(); err != nil {
				return fmt.Errorf("stopping legacy probe %s: %w", adapter.Name(), err)
			}
		}
	}

	if err := m.orchestrator.Stop(); err != nil {
		return fmt.Errorf("stopping orchestrator: %w", err)
	}

	return nil
}

func (m *TlsManager) ProcessStarted(ctx context.Context, proc *process.Process) error {
	ctx, span := tracer.WithoutCancel(ctx, "TlsManager.ProcessStarted")
	defer span.End()

	// start a new scan, cancelling any in-progress scan and waiting for it to finish
	ctx, err := proc.StartScan(ctx)
	if err != nil {
		return fmt.Errorf("starting scan: %w", err)
	}
	defer proc.FinishScan()

	// check if process is still active
	if proc.Exited() {
		return nil
	}

	// check if the process strategy is observe
	if proc.Strategy != process.StrategyObserve {
		return nil
	}

	// check if process is filtered
	if proc.IsFiltered(config.FilterLevel_TLS) {
		return nil
	}

	// // ensure the elf gets closed at the end of the scan (for legacy probes)
	// defer func() {
	// 	if err := proc.CloseElf(); err != nil {
	// 		m.logger.Error("closing elf", zap.Error(err))
	// 	}
	// }()

	// Run orchestrator-based scanners
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}

	err = m.orchestrator.ProcessStarted(ctx, proc)
	if m.final != nil {
		if e := m.final.ProcessStarted(ctx, proc); e != nil {
			m.logger.Error("starting process on final observer", zap.Error(e))
		}
	}

	return err
}

func (m *TlsManager) ProcessReplaced(ctx context.Context, proc *process.Process) error {
	ctx, span := tracer.WithoutCancel(ctx, "TlsManager.ProcessReplaced")
	defer span.End()

	if m.final != nil {
		if e := m.final.ProcessReplaced(ctx, proc); e != nil {
			m.logger.Error("replacing process on final observer", zap.Error(e))
		}
	}

	// cancel any in-progress scan since the process is being replaced
	proc.CancelScan(process.ErrProcessReplaced)

	return m.orchestrator.ProcessReplaced(ctx, proc)
}

func (m *TlsManager) ProcessStopped(ctx context.Context, proc *process.Process) error {
	ctx, span := tracer.WithoutCancel(ctx, "TlsManager.ProcessStopped")
	defer span.End()

	// cancel any in-progress scan since the process is stopping
	proc.CancelScan(process.ErrProcessStopped)

	err := m.orchestrator.ProcessStopped(ctx, proc)

	if m.final != nil {
		if e := m.final.ProcessStopped(ctx, proc); e != nil {
			m.logger.Error("stopping process on final observer", zap.Error(e))
		}
	}

	return err
}
