package tls

import (
	"context"
	"errors"
	"fmt"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"github.com/qpoint-io/qtap/pkg/telemetry/metrics"
	"go.uber.org/zap"
)

type TlsProbe interface {
	Start(ctx context.Context) error
	Stop() error
	process.Observer
}

type TlsManager struct {
	// logger
	logger *zap.Logger

	// probes
	probes []TlsProbe

	final process.Observer
}

var tracer = telemetry.Tracer()

func NewTlsManager(logger *zap.Logger, probes ...TlsProbe) *TlsManager {
	return &TlsManager{
		// logger
		logger: logger,

		// initialize selected probes
		probes: probes,
	}
}

func (m *TlsManager) Start(ctx context.Context) error {
	for _, p := range m.probes {
		if err := p.Start(ctx); err != nil {
			return fmt.Errorf("starting tls probe: %w", err)
		}
	}

	metrics.NewSystemGaugeFunc("probes_started", "The number of probes that have started",
		func() float64 {
			return float64(len(m.probes))
		},
	)

	return nil
}

func (m *TlsManager) SetFinalObserver(o process.Observer) {
	m.final = o
}

func (m *TlsManager) Stop() error {
	for _, p := range m.probes {
		if err := p.Stop(); err != nil {
			return fmt.Errorf("stopping tls probe: %w", err)
		}
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

	// ensure the elf gets closed at the end of the scan
	defer func() {
		if err := proc.CloseElf(); err != nil {
			m.logger.Error("closing elf", zap.Error(err))
		}
	}()

	// inform all probes
	for _, p := range m.probes {
		// check if scan was cancelled
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		if proc.Exited() {
			continue
		}

		if err := p.ProcessStarted(ctx, proc); err != nil {
			// if the process was replaced mid scan, just return and let the next one try
			if errors.Is(err, process.ErrProcessReplaced) {
				continue
			}
			return fmt.Errorf("starting process on tls probe: %w", err)
		}
	}

	if m.final != nil {
		if err := m.final.ProcessStarted(ctx, proc); err != nil {
			return fmt.Errorf("starting process on final observer: %w", err)
		}
	}

	return nil
}

func (m *TlsManager) ProcessReplaced(ctx context.Context, proc *process.Process) error {
	ctx, span := tracer.WithoutCancel(ctx, "TlsManager.ProcessReplaced")
	defer span.End()

	proc.CancelScan(process.ErrProcessReplaced)

	if m.final != nil {
		if err := m.final.ProcessReplaced(ctx, proc); err != nil {
			return fmt.Errorf("replacing process on final observer: %w", err)
		}
	}

	// inform all probes
	for _, p := range m.probes {
		if err := p.ProcessReplaced(ctx, proc); err != nil {
			return fmt.Errorf("replacing process on tls probe: %w", err)
		}
	}

	// run process started again
	return m.ProcessStarted(ctx, proc)
}

func (m *TlsManager) ProcessStopped(ctx context.Context, proc *process.Process) error {
	ctx, span := tracer.WithoutCancel(ctx, "TlsManager.ProcessStopped")
	defer span.End()

	// cancel any in-progress scan since the process is stopping
	proc.CancelScan(process.ErrProcessStopped)

	for _, p := range m.probes {
		if err := p.ProcessStopped(ctx, proc); err != nil {
			return fmt.Errorf("stopping process on tls probe: %w", err)
		}
	}

	if m.final != nil {
		if err := m.final.ProcessStopped(ctx, proc); err != nil {
			return fmt.Errorf("stopping process on final observer: %w", err)
		}
	}

	return nil
}
