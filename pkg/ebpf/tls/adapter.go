package tls

import (
	"context"
	"io"
	"sync"

	"github.com/qpoint-io/qtap/pkg/binutils"
	"github.com/qpoint-io/qtap/pkg/process"
)

// LegacyProbeAdapter wraps a legacy TlsProbe to implement the Scanner interface.
// This is a compatibility layer that allows gradual migration to the new architecture.
//
// Trade-offs:
//   - Detection/scanning happen during Attach (not in Detect/Scan phases)
//   - Binary-hash caching is bypassed (legacy probes have their own caching)
//   - The legacy probe's Start() must be called separately before use
//   - The legacy probe's Stop() must be called separately at shutdown
//
// Usage:
//
//	probe := openssl.NewOpenSSLManager(logger, probeFn)
//	probe.Start(ctx) // Must call Start before use
//	adapter := tls.NewLegacyProbeAdapter("openssl", probe)
//	orchestrator := tls.NewOrchestrator(logger, []tls.Scanner{adapter})
type LegacyProbeAdapter struct {
	probe TlsProbe
	name  string

	// Track attached processes for cleanup
	mu       sync.Mutex
	attached map[int]struct{}
	stopped  bool
}

// NewLegacyProbeAdapter creates an adapter that wraps a legacy TlsProbe.
// The probe's Start() method must be called before the adapter is used.
func NewLegacyProbeAdapter(name string, probe TlsProbe) *LegacyProbeAdapter {
	return &LegacyProbeAdapter{
		name:     name,
		probe:    probe,
		attached: make(map[int]struct{}),
	}
}

// Name returns the scanner name
func (a *LegacyProbeAdapter) Name() string {
	return a.name
}

// Detect always returns true since legacy probes do their own detection in ProcessStarted.
// This means every binary will proceed to Scan/Attach, where the legacy probe decides.
func (a *LegacyProbeAdapter) Detect(ctx context.Context, bin *binutils.Binary) bool {
	// Legacy probes do detection in ProcessStarted
	// We return true to let them run their logic
	return true
}

// Scan returns a placeholder result since legacy probes do their own scanning in ProcessStarted.
// The result is not cached by the orchestrator (caching is disabled for legacy adapters).
func (a *LegacyProbeAdapter) Scan(ctx context.Context, bin *binutils.Binary) (ScanResult, error) {
	return &legacyPlaceholderResult{name: a.name}, nil
}

// Attach implements Scanner interface but is not used for legacy adapters.
// Use AttachWithProcess instead via the LegacyAttacher interface.
func (a *LegacyProbeAdapter) Attach(ctx context.Context, pidExe string, bin *binutils.Binary, result ScanResult) (io.Closer, error) {
	// Legacy adapters need the full process, so the orchestrator should call AttachWithProcess
	return nil, nil
}

// AttachWithProcess delegates to the legacy probe's ProcessStarted method.
// This is called by the orchestrator for legacy adapters that need the full process.
// Returns a Detacher that will call ProcessStopped when closed.
func (a *LegacyProbeAdapter) AttachWithProcess(ctx context.Context, proc *process.Process, bin *binutils.Binary, result ScanResult) (io.Closer, error) {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return nil, nil
	}
	a.mu.Unlock()

	// Delegate to the legacy probe
	if err := a.probe.ProcessStarted(ctx, proc); err != nil {
		return nil, err
	}

	// Track this attachment
	a.mu.Lock()
	a.attached[proc.Pid] = struct{}{}
	a.mu.Unlock()

	return &legacyProbeDetacher{
		adapter: a,
		proc:    proc,
	}, nil
}

func (a *LegacyProbeAdapter) AttachLibrary(ctx context.Context, library *SharedLibrary) (io.Closer, error) {
	// Legacy probes handle shared libraries indepdenently.
	return nil, nil
}

func (a *LegacyProbeAdapter) SharedLibraries() []string {
	// Legacy probes handle shared libraries indepdenently.
	return nil
}

// Probe returns the underlying legacy probe for lifecycle management
func (a *LegacyProbeAdapter) Probe() TlsProbe {
	return a.probe
}

// HandleProcessReplaced should be called when a process is replaced.
// This delegates to the legacy probe's ProcessReplaced method.
func (a *LegacyProbeAdapter) HandleProcessReplaced(ctx context.Context, proc *process.Process) error {
	a.mu.Lock()
	_, wasAttached := a.attached[proc.Pid]
	a.mu.Unlock()

	if !wasAttached {
		return nil
	}

	return a.probe.ProcessReplaced(ctx, proc)
}

// Stop marks the adapter as stopped and calls the legacy probe's Stop method.
// After Stop is called, Attach will return nil detachers.
func (a *LegacyProbeAdapter) Stop() error {
	a.mu.Lock()
	a.stopped = true
	a.attached = make(map[int]struct{})
	a.mu.Unlock()

	return a.probe.Stop()
}

// legacyPlaceholderResult is a minimal ScanResult for legacy probes
type legacyPlaceholderResult struct {
	name     string
	detected bool
}

func (r *legacyPlaceholderResult) ProbeType() string {
	return r.name
}

func (r *legacyPlaceholderResult) ProbeDetected() bool {
	return r.detected
}

// legacyProbeDetacher calls ProcessStopped on the legacy probe when closed
type legacyProbeDetacher struct {
	adapter *LegacyProbeAdapter
	proc    *process.Process
}

func (d *legacyProbeDetacher) Close() error {
	d.adapter.mu.Lock()
	delete(d.adapter.attached, d.proc.Pid)
	stopped := d.adapter.stopped
	d.adapter.mu.Unlock()

	if stopped {
		return nil
	}

	return d.adapter.probe.ProcessStopped(context.Background(), d.proc)
}

// LegacyAdapterOption configures a LegacyProbeAdapter
type LegacyAdapterOption func(*LegacyProbeAdapter)

// SkipCaching is a marker interface that Scanner implementations can implement
// to indicate that their results should not be cached by the orchestrator.
// LegacyProbeAdapter implements this because legacy probes have their own caching.
type SkipCaching interface {
	SkipCaching() bool
}

// SkipCaching returns true to indicate that this adapter's results should not be cached.
// Legacy probes manage their own caching, so the orchestrator should not cache their results.
func (a *LegacyProbeAdapter) SkipCaching() bool {
	return true
}
