package tls

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/kamaln7/resolvable"
	"github.com/qpoint-io/qtap/pkg/binutils"
	"github.com/qpoint-io/qtap/pkg/synq"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

const (
	// DefaultCacheTTL is the default TTL for cached scan results
	DefaultCacheTTL = 10 * time.Minute

	// DefaultCacheCleanupInterval is the default interval for cache cleanup
	DefaultCacheCleanupInterval = 5 * time.Minute
)

//go:generate go tool go.uber.org/mock/mockgen --destination=target_scanner_mock.go --package tls . TargetScanner
type TargetScanner interface {
	Scan(ctx context.Context, target *ExeScannable) (*ScanResult, error)
	Attach(ctx context.Context, attachable *ExeAttachable, res *ScanResult) (io.Closer, error)
	ScanContainer(ctx context.Context, id, root string) (*ContainerScanResult, error)
	AttachContainer(ctx context.Context, res *ContainerScanResult) (io.Closer, error)
	Close() error
}

type ExeScannable struct {
	// Path is the path to the executable
	Path string
	// Cmdline is the command+args that was used to start this process
	Cmdline []string
	// Root is the container root filesystem path for this process
	Root string
}

// targetScanner coordinates scanning and attachment across multiple probes.
type targetScanner struct {
	logger *zap.Logger
	probes map[string]Probe
	cache  *synq.TTLCache[string, *ScanResult]
}

// TargetScannerOption configures the TargetScanner
type TargetScannerOption func(*targetScanner)

// NewTargetScanner creates a new TargetScanner with the given probes
func NewTargetScanner(logger *zap.Logger, probes []Probe, opts ...TargetScannerOption) *targetScanner {
	o := &targetScanner{
		logger: logger,
		probes: make(map[string]Probe, len(probes)),
		cache:  synq.NewTTLCache[string, *ScanResult](DefaultCacheTTL, DefaultCacheCleanupInterval),
	}

	for _, probe := range probes {
		o.logger.Debug("registering TLS probe", zap.String("probe", probe.Name()))
		o.probes[probe.Name()] = probe
	}

	for _, opt := range opts {
		opt(o)
	}

	return o
}

func (s *targetScanner) Close() error {
	s.cache.Stop()
	for _, probe := range s.probes {
		if err := probe.Close(); err != nil {
			return fmt.Errorf("closing probe %s: %w", probe.Name(), err)
		}
	}
	return nil
}

// ScanResult is the result of scanning a target
type ScanResult struct {
	Hash  string
	Mtime int64
	// ProbeResults is a map of probe name to scan result
	ProbeResults map[string]ProbeScanResult
}

func (s *targetScanner) Scan(ctx context.Context, target *ExeScannable) (*ScanResult, error) {
	ctx, span := tracer.Start(ctx, "TargetScanner.Scan")
	defer span.End()
	span.SetAttributes(attribute.String("path", target.Path))

	// Open ELF if not already provided
	elf, err := binutils.NewElf(ctx, target.Path, "/", false)
	if err != nil {
		return nil, fmt.Errorf("opening executable: %w", err)
	}

	hash, err := elf.Hash(ctx)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("getting hash: %w", err)
	}
	span.SetAttributes(attribute.String("hash", hash))

	if cached, ok := s.cache.LoadAndRenew(hash); ok {
		if cached.Mtime == elf.Mtime() {
			span.SetAttributes(attribute.String("cache_status", "hit"))
			return cached, nil
		}

		// the cached file has a different mtime, we should re-scan
		s.cache.ExpireRecord(hash)
		span.SetAttributes(attribute.String("cache_status", "mtime_mismatch"))
	}
	span.SetAttributes(attribute.String("cache_status", "miss"))

	// TODO: check if there is an in-progress scan for this hash
	// if so, wait for it. if it returns an error, try again. otherwise, return the cached result.

	res := &ScanResult{
		Hash:         hash,
		Mtime:        elf.Mtime(),
		ProbeResults: make(map[string]ProbeScanResult),
	}
	scannable := &ExeElfScannable{
		ExeScannable: *target,
		Elf:          elf,
	}
	for probeName, probe := range s.probes {
		probeRes, err := probe.Scan(ctx, scannable)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scanning probe %s: %w", probeName, err)
		}
		res.ProbeResults[probeName] = probeRes
	}

	s.cache.Store(hash, res)
	return res, nil
}

func (s *targetScanner) Attach(ctx context.Context, attachable *ExeAttachable, res *ScanResult) (io.Closer, error) {
	if len(res.ProbeResults) == 0 {
		return nil, nil
	}

	ctx, span := tracer.Start(ctx, "TargetScanner.Attach")
	defer span.End()
	span.SetAttributes(
		attribute.Int("pid", attachable.PID),
		attribute.String("path", attachable.Path),
	)

	// lazy open the executable
	// if no probes indicated detection, we won't open the executable
	openEx := resolvable.New(func(ctx context.Context) (*link.Executable, error) {
		return link.OpenExecutable(attachable.Path)
	}, resolvable.WithRetry())

	var closers MultiCloser
	for probeName, probeRes := range res.ProbeResults {
		if !probeRes.ProbeDetected() {
			continue
		}

		probe := s.probes[probeName]
		if probe == nil {
			s.logger.Warn("unrecognized probe in scan result", zap.String("probe_name", probeName))
			continue
		}

		ex, err := openEx(ctx)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("opening executable: %w", err)
		}

		closer, err := probe.Attach(ctx, &ExeLinkAttachable{
			ExeAttachable: *attachable,
			Exe:           ex,
		}, probeRes)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("attaching probe %s: %w", probeName, err)
		}
		closers = append(closers, closer)
	}

	return closers, nil
}

type ContainerScanResult struct {
	// SharedLibraries is a map of shared libraries by probe name
	SharedLibraries map[string][]*SharedLibrary
}

// ScanContainer scans a container for shared libraries.
func (s *targetScanner) ScanContainer(ctx context.Context, id, root string) (*ContainerScanResult, error) {
	ctx, span := tracer.Start(ctx, "TargetScanner.ScanContainer")
	span.SetAttributes(
		attribute.String("container_id", id),
		attribute.String("root", root),
	)
	defer span.End()

	res := &ContainerScanResult{
		SharedLibraries: make(map[string][]*SharedLibrary),
	}

	// TODO: we can further optimize this by collecting all libraries from all probes
	// and scanning the fs only once.
	for _, probe := range s.probes {
		libs := probe.SharedLibraries()
		if len(libs) == 0 {
			continue
		}

		libPaths, err := FindSharedLibraries(ctx, root, libs)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scanning for shared libraries for probe %s: %w", probe.Name(), err)
		}

		if len(libPaths) > 0 {
			res.SharedLibraries[probe.Name()] = libPaths
		}
	}

	return res, nil
}

func (s *targetScanner) AttachContainer(ctx context.Context, res *ContainerScanResult) (io.Closer, error) {
	if len(res.SharedLibraries) == 0 {
		return nil, nil
	}

	ctx, span := tracer.Start(ctx, "TargetScanner.AttachContainer")
	defer span.End()

	var closers MultiCloser
	for probeName, libs := range res.SharedLibraries {
		probe := s.probes[probeName]
		if probe == nil {
			s.logger.Warn("unrecognized probe in scan result", zap.String("probe_name", probeName))
			continue
		}

		for _, lib := range libs {
			closer, err := probe.AttachLibrary(ctx, lib)
			if err != nil {
				span.RecordError(err)
				return nil, fmt.Errorf("attaching library %s for probe %s: %w", lib.Name, probeName, err)
			}
			closers = append(closers, closer)
		}
	}

	return closers, nil
}
