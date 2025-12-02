package tls

import (
	"context"
	"errors"
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

var (
	ErrDetectNotFound = errors.New("detect not found")
)

// TargetScanner coordinates scanning and attachment across multiple probes.
type TargetScanner struct {
	logger *zap.Logger
	probes map[string]Probe
	cache  *synq.TTLCache[string, *ScanResult]
}

// TargetScannerOption configures the TargetScanner
type TargetScannerOption func(*TargetScanner)

// NewTargetScanner creates a new TargetScanner with the given probes
func NewTargetScanner(logger *zap.Logger, probes []Probe, opts ...TargetScannerOption) *TargetScanner {
	o := &TargetScanner{
		logger: logger,
		probes: make(map[string]Probe, len(probes)),
		cache:  synq.NewTTLCache[string, *ScanResult](DefaultCacheTTL, DefaultCacheCleanupInterval),
	}

	for _, probe := range probes {
		o.probes[probe.Name()] = probe
	}

	for _, opt := range opts {
		opt(o)
	}

	return o
}

func (m *TargetScanner) Close() error {
	m.cache.Stop()
	return nil
}

// ScanResult is the result of scanning a target
type ScanResult struct {
	Path string
	Hash string
	// ProbeResults is a map of probe name to scan result
	ProbeResults map[string]ProbeScanResult
}

func (m *TargetScanner) Scan(ctx context.Context, path string) (*ScanResult, error) {
	elf, err := binutils.NewElf(ctx, path, "/", false)
	if err != nil {
		return nil, fmt.Errorf("opening executable: %w", err)
	}

	hash, err := elf.Hash(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting hash: %w", err)
	}

	// TODO: use cache

	res := &ScanResult{
		Path:         path,
		Hash:         hash,
		ProbeResults: make(map[string]ProbeScanResult),
	}
	for probeName, probe := range m.probes {
		probeRes, err := probe.Scan(ctx, elf)
		if err != nil {
			return nil, fmt.Errorf("scanning probe %s: %w", probeName, err)
		}
		res.ProbeResults[probeName] = probeRes
	}

	return res, nil
}

func (m *TargetScanner) Attach(ctx context.Context, pid int, path string, res *ScanResult) (io.Closer, error) {
	// lazy open the executable
	// if no probes indicated detection, we won't open the executable
	openEx := resolvable.New(func(ctx context.Context) (*link.Executable, error) {
		return link.OpenExecutable(path)
	}, resolvable.WithRetry())

	var closers MultiCloser
	for probeName, probeRes := range res.ProbeResults {
		if !probeRes.ProbeDetected() {
			continue
		}

		probe := m.probes[probeName]
		if probe == nil {
			return nil, fmt.Errorf("probe %s not found", probeName)
		}

		ex, err := openEx(ctx)
		if err != nil {
			return nil, fmt.Errorf("opening executable: %w", err)
		}

		closer, err := probe.Attach(ctx, &ExeAttachable{
			PID:  pid,
			Path: path,
			Exe:  ex,
		}, probeRes)
		if err != nil {
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
func (s *TargetScanner) ScanContainer(ctx context.Context, id, root string) (*ContainerScanResult, error) {
	ctx, span := tracer.WithoutCancel(ctx, "TargetScanner.ScanContainer") //nolint:ineffassign,wastedassign,staticcheck
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

func (s *TargetScanner) AttachContainer(ctx context.Context, res *ContainerScanResult) (io.Closer, error) {
	ctx, span := tracer.WithoutCancel(ctx, "TargetScanner.AttachContainer")
	defer span.End()

	var closers MultiCloser
	for probeName, libs := range res.SharedLibraries {
		probe := s.probes[probeName]
		if probe == nil {
			return nil, fmt.Errorf("probe %s not found", probeName)
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
