package tls

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/qpoint-io/qtap/pkg/binutils"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/synq"
	"github.com/sourcegraph/conc"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

const (
	// DefaultCacheTTL is the default TTL for cached scan results
	DefaultCacheTTL = 10 * time.Minute

	// DefaultCacheCleanupInterval is the default interval for cache cleanup
	DefaultCacheCleanupInterval = 5 * time.Minute
)

var (
	ErrDetectNotFound = errors.New("detect not found")
)

// Orchestrator coordinates TLS probe scanning and attachment.
// It manages multiple probes, runs them concurrently, and caches results.
type Orchestrator struct {
	logger *zap.Logger
	probes []Probe
	cache  *ScanCache

	// procAttachments tracks process attachments by PID
	procAttachments *synq.Map[int, *ProcessAttachment]

	// libraryClosers tracks closers for attached shared libraries
	libraryClosers []io.Closer
	libraryMu      sync.Mutex

	// containers tracks scanned containers by container ID.
	containers *synq.Map[string, bool]
	// containerMu is used to ensure that a container is only scanned once.
	containerMu *synq.MutexMap[string]

	// ctx is canceled when the orchestrator is stopped
	ctx    context.Context
	cancel context.CancelFunc
}

// OrchestratorOption configures the Orchestrator
type OrchestratorOption func(*Orchestrator)

// WithCache sets a custom cache for the orchestrator
func WithCache(cache *ScanCache) OrchestratorOption {
	return func(o *Orchestrator) {
		o.cache = cache
	}
}

// NewOrchestrator creates a new Orchestrator with the given scanners
func NewOrchestrator(logger *zap.Logger, probes []Probe, opts ...OrchestratorOption) *Orchestrator {
	o := &Orchestrator{
		logger: logger,
		probes: probes,

		procAttachments: synq.NewMap[int, *ProcessAttachment](),
		containers:      synq.NewMap[string, bool](),
		containerMu:     synq.NewMutexMap[string](),
	}

	for _, opt := range opts {
		opt(o)
	}

	// Create default cache if not provided
	if o.cache == nil {
		o.cache = NewScanCache(DefaultCacheTTL, DefaultCacheCleanupInterval)
	}

	o.ctx, o.cancel = context.WithCancel(context.Background())

	return o
}

// ProcessAttachment tracks a process that has been attached to.
type ProcessAttachment struct {
	exePath    string
	binaryHash string

	// attached indicates if the process has been scanned and attached to
	// TODO: can we just use closer actually
	attached bool

	// closer is the closer for the process attachments
	closer io.Closer
}

// Stop cleans up the orchestrator, stopping the cache and detaching all probes
func (o *Orchestrator) Stop() error {
	o.cancel()
	o.cache.Stop()

	var errs error
	// Detach all per-process attachments
	o.procAttachments.Iter(func(pid int, attachment *ProcessAttachment) bool {
		if attachment.attached {
			if err := attachment.closer.Close(); err != nil {
				errs = multierror.Append(errs, fmt.Errorf("detaching probes for pid %d: %w", pid, err))
			}
		}
		return true
	})

	// Detach all shared library attachments
	o.libraryMu.Lock()
	defer o.libraryMu.Unlock()
	if err := MultiCloser(o.libraryClosers).Close(); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("detaching libraries: %w", err))
	}

	return errs
}

// ProcessStarted scans a process binary and attaches applicable TLS probes.
// It runs all scanners concurrently and caches results by binary hash.
// Returns a combined Detacher for all attached probes.
func (o *Orchestrator) ProcessStarted(ctx context.Context, proc *process.Process) error {
	ctx, span := tracer.WithoutCancel(ctx, "Orchestrator.ProcessStarted")
	defer span.End()
	span.SetAttributes(
		attribute.Int("pid", proc.Pid),
		attribute.String("exe", proc.Exe),
		attribute.String("container_id", proc.ContainerID),
	)

	// Scan the container for shared libraries
	go o.scanContainer(ctx, proc.ContainerID, proc.Root)

	return o.scanAndAttachProcess(ctx, proc, nil, nil)
}

// ProcessReplaced handles when a process's binary is replaced (via exec())
func (o *Orchestrator) ProcessReplaced(ctx context.Context, proc *process.Process) error {
	ctx, span := tracer.WithoutCancel(ctx, "Orchestrator.ProcessReplaced") //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()
	span.SetAttributes(attribute.Int("pid", proc.Pid))

	// TODO: if there is an existing scan in progress, keep it running for the old binary but dont overwrite by pid

	attachment, exists := o.procAttachments.Load(proc.Pid)
	if !exists {
		// ProcessStarted did not run for some reason. Rescan.
		return o.scanAndAttachProcess(ctx, proc, nil, nil)
	}

	ll := o.logger.With(zap.Int("pid", proc.Pid))
	span.SetAttributes(attribute.String("old_hash", attachment.binaryHash))
	span.SetAttributes(attribute.String("old_exe_path", attachment.exePath))

	if attachment.exePath != proc.PidExe {
		// the path has changed. Rescan.
		ll.Debug("executable path has changed, rescanning process",
			zap.String("old_exe_path", attachment.exePath),
			zap.String("new_exe_path", proc.PidExe),
		)

		if err := o.detachProcess(ctx, proc.Pid); err != nil {
			return fmt.Errorf("detaching existing probes: %w", err)
		}
		return o.scanAndAttachProcess(ctx, proc, nil, nil)
	}

	// it is possible that the old binary was moved to a different path and a new one replaced it.
	// check if the binary hash has changed
	// TODO: do we need this?

	return nil
}

// ProcessStopped cleans up when a process exits
func (o *Orchestrator) ProcessStopped(ctx context.Context, proc *process.Process) error {
	ctx, span := tracer.WithoutCancel(ctx, "Orchestrator.ProcessStopped") //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()
	span.SetAttributes(attribute.Int("pid", proc.Pid))

	return o.detachProcess(ctx, proc.Pid)
}

// scanAndAttachProcess scans a process binary and attaches applicable TLS probes.
// fd and bin are optional.
func (o *Orchestrator) scanAndAttachProcess(ctx context.Context, proc *process.Process, fd *os.File, bin *binutils.Binary) error {
	if proc.Exe == "" {
		return nil // not a real process
	}

	if fd == nil {
		var err error
		fd, err = os.Open(proc.PidExe)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				o.logger.Debug("binary not found, skipping scan and attach",
					zap.Int("pid", proc.Pid),
					zap.String("exe", proc.Exe),
					zap.String("pid_exe", proc.PidExe),
				)
				return nil
			}
			return fmt.Errorf("opening binary: %w", err)
		}
	}

	if bin == nil {
		var err error
		bin, err = binutils.ParseBinary(ctx, fd)
		if err != nil {
			return fmt.Errorf("parsing binary: %w", err)
		}
	}

	// Run all scanners concurrently
	closer, detectedProbes, err := o.runProbes(ctx, proc, bin)
	if err != nil {
		fd.Close()
		return err
	}

	attachment := &ProcessAttachment{
		exePath:    proc.PidExe,
		binaryHash: bin.Hash,
	}

	// If no probes were detected, close the binary
	if len(detectedProbes) == 0 {
		fd.Close()
	} else {
		attachment.attached = true
		// create a combined closer that closes the probes first and then the binary fd
		attachment.closer = MultiCloser{closer, fd}
	}

	// Store the attachment result by pid
	o.procAttachments.Store(proc.Pid, attachment)
	return nil
}

// probeResult holds the result of a single probe's detect/scan/attach cycle
type probeResult struct {
	name   string
	result ScanResult
	closer io.Closer
	err    error
}

// runProbes runs all probes concurrently and returns closers for successful attachments
func (o *Orchestrator) runProbes(ctx context.Context, proc *process.Process, elf *binutils.Elf) (io.Closer, []string, error) {
	// Get or create cache entry for this binary
	hash, err := elf.Hash()
	if err != nil {
		return nil, nil, fmt.Errorf("getting hash: %w", err)
	}
	cachedResults := o.cache.GetOrCreate(hash)

	// Channel to collect results from concurrent probes
	resultsChan := make(chan probeResult, len(o.probes))
	var wg conc.WaitGroup

	for _, probe := range o.probes {
		wg.Go(func() {
			result := o.runProbe(ctx, proc, elf, probe, cachedResults)
			resultsChan <- result
		})
	}

	// Wait for all scanners to complete and close the channel
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results
	var (
		closers        MultiCloser
		detectedProbes []string
	)
	for probeRes := range resultsChan {
		if probeRes.err != nil {
			o.logger.Debug("probe failed",
				zap.String("probe", probeRes.name),
				zap.Error(probeRes.err),
			)
			continue
		}

		if probeRes.result != nil && probeRes.result.ProbeDetected() {
			// Record that this probe type was detected
			proc.AddDetectedTLSProbeType(probeRes.name)
			detectedProbes = append(detectedProbes, probeRes.name)
			if probeRes.closer != nil {
				closers = append(closers, probeRes.closer)
			}
		}
	}

	return closers, detectedProbes, nil
}

// runProbe runs a single probe: detect -> scan -> attach
func (o *Orchestrator) runProbe(ctx context.Context, proc *process.Process, elf *binutils.Elf, probe Probe, resultCache *ResultCache) probeResult {
	// Check if this scanner wants to skip caching (e.g., legacy adapters)
	skipCache := false
	if sc, ok := probe.(SkipCaching); ok {
		skipCache = sc.SkipCaching()
	}

	// Check if we have a cached result (unless caching is skipped)
	if !skipCache {
		if result, ok := resultCache.Load(probe.Name()); ok {
			// Attach using cached result
			var closer io.Closer
			var err error
			if legacy, ok := probe.(*LegacyProbeAdapter); ok {
				closer, err = legacy.AttachWithProcess(ctx, proc, elf, result)
			} else {
				closer, err = probe.Attach(ctx, proc.PidExe, elf, result)
			}
			if err != nil {
				return probeResult{name: probe.Name(), err: fmt.Errorf("attaching (cached): %w", err)}
			}
			return probeResult{name: probe.Name(), result: result, closer: closer}
		}
	}

	// Quick detection check
	if !probe.Detect(ctx, bin) {
		return probeResult{name: probe.Name()} // No match, no error
	}

	// Perform full scan
	result, err := probe.Scan(ctx, bin)
	if err != nil {
		return probeResult{name: probe.Name(), err: fmt.Errorf("scanning: %w", err)}
	}

	// Cache the result (unless caching is skipped)
	if !skipCache {
		resultCache.Store(probe.Name(), result)
	}

	if !result.ProbeDetected() {
		return probeResult{name: probe.Name()} // Not detected, nothing to do
	}

	if proc.Exited() {
		o.logger.Debug("process has exited, skipping attachment",
			zap.String("probe", probe.Name()),
			zap.String("exe", proc.Exe),
			zap.Int("pid", proc.Pid),
			zap.String("binary_hash", bin.Hash),
		)
		return probeResult{name: probe.Name(), result: result}
	}

	// Attach probes directly to the binary (static linking)
	var closer io.Closer
	if legacy, ok := probe.(*LegacyProbeAdapter); ok {
		closer, err = legacy.AttachWithProcess(ctx, proc, bin, result)
	} else {
		closer, err = probe.Attach(ctx, proc.PidExe, bin, result)
	}
	if err != nil {
		return probeResult{name: probe.Name(), err: fmt.Errorf("attaching: %w", err)}
	}

	o.logger.Debug("attached TLS probe",
		zap.String("probe", probe.Name()),
		zap.String("exe", proc.Exe),
		zap.Int("pid", proc.Pid),
		zap.String("binary_hash", bin.Hash),
	)

	return probeResult{name: probe.Name(), result: result, closer: closer}
}

// detachProcess removes and closes all attachments for a PID
func (o *Orchestrator) detachProcess(ctx context.Context, pid int) error {
	ctx, span := tracer.Start(ctx, "Orchestrator.detachProcess") //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()
	span.SetAttributes(attribute.Int("pid", pid))

	attachment, exists := o.procAttachments.Load(pid)
	if !exists {
		return nil
	}
	span.SetAttributes(attribute.Bool("was_attached", attachment.attached))

	o.procAttachments.Delete(pid)
	if attachment.closer != nil {
		if err := attachment.closer.Close(); err != nil {
			return fmt.Errorf("detaching probes: %w", err)
		}
	}

	return nil
}

// scanContainer scans a container for shared libraries if it has not been scanned yet
func (o *Orchestrator) scanContainer(ctx context.Context, id, root string) {
	mu := o.containerMu.Get(id)
	mu.Lock()
	defer mu.Unlock()

	scanned, _ := o.containers.Load(id)
	if scanned {
		// already scanned
		return
	}

	ctx, span := tracer.Start(o.ctx, "Orchestrator.scanContainer",
		trace.WithLinks(trace.LinkFromContext(ctx)),
		trace.WithAttributes(attribute.String("container_id", id)),
	) //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()

	var errs error
	// TODO: collect all paths from all probes and attach to them all at once
	// so we don't have to scan the fs multiple times
	for _, probe := range o.probes {
		libs := probe.SharedLibraries()
		if len(libs) == 0 {
			continue
		}

		libPaths, err := FindSharedLibraries(ctx, root, libs)
		if err != nil {
			errs = multierror.Append(errs, fmt.Errorf("scanning for shared libraries for probe %s: %w", probe.Name(), err))
			continue
		}

		for name, paths := range libPaths {
			closer, err := probe.AttachLibrary(ctx, &SharedLibrary{
				Name:  name,
				Paths: paths,
			})
			if err != nil {
				errs = multierror.Append(errs, fmt.Errorf("attaching probes for shared library %s: %w", name, err))
				continue
			}
			o.libraryMu.Lock()
			o.libraryClosers = append(o.libraryClosers, closer)
			o.libraryMu.Unlock()
		}
	}

	o.containers.Store(id, true)
	if errs != nil {
		span.RecordError(errs)
		o.logger.Error("failed to scan container", zap.String("container_id", id), zap.Error(errs))
	}
}

// Probes returns the list of registered probes
func (o *Orchestrator) Probes() []Probe {
	return o.probes
}

// Cache returns the scan cache
func (o *Orchestrator) Cache() *ScanCache {
	return o.cache
}
