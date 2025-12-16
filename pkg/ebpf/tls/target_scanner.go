package tls

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/hashicorp/go-multierror"
	"github.com/kamaln7/resolvable"
	"github.com/qpoint-io/qtap/pkg/binutils"
	"github.com/qpoint-io/qtap/pkg/process"
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
	// scanGroup ensures that we don't scan the same target multiple times concurrently.
	scanGroup synq.SingleFlight[string, *ScanResult]
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

	var eg multierror.Group
	for _, probe := range s.probes {
		eg.Go(func() error {
			if err := probe.Close(); err != nil {
				return fmt.Errorf("closing probe %s: %w", probe.Name(), err)
			}
			return nil
		})
	}

	return eg.Wait().ErrorOrNil()
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
	defer elf.Close()

	hash, err := elf.Hash(ctx)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("getting hash: %w", err)
	}
	span.SetAttributes(attribute.String("hash", hash))

	// check if we have a cached result for this hash
	if cached, ok := s.cache.LoadAndRenew(hash); ok {
		if cached.Mtime == elf.Mtime().Unix() {
			span.SetAttributes(attribute.String("cache_status", "hit"))
			return cached, nil
		}

		// the cached file has a different mtime, we should re-scan
		s.cache.ExpireRecord(hash)
		span.SetAttributes(attribute.String("cache_status", "mtime_mismatch"))
	} else {
		span.SetAttributes(attribute.String("cache_status", "miss"))
	}

	// queue a scan. scanGroup ensures that we don't scan the same target multiple times concurrently.
	return s.scanGroup.Do(hash, func() (*ScanResult, error) {
		// the cache may have been updated by the time our scan is executed.
		// check again:
		if cached, ok := s.cache.LoadAndRenew(hash); ok {
			if cached.Mtime == elf.Mtime().Unix() {
				span.SetAttributes(attribute.String("cache_status", "hit"))
				return cached, nil
			}
		}

		res := &ScanResult{
			Hash:         hash,
			Mtime:        elf.Mtime().Unix(),
			ProbeResults: make(map[string]ProbeScanResult),
		}
		scannable := &ExeElfScannable{
			ExeScannable: *target,
			Elf:          elf,
		}
		s.logger.Debug("scanning target",
			zap.String("path", target.Path),
			zap.String("hash", hash),
			zap.Time("mtime", elf.Mtime()),
		)
		for probeName, probe := range s.probes {
			probeRes, err := probe.Scan(ctx, scannable)
			if err != nil {
				err = &process.TlsProbeError{
					ProbeName: probeName,
					Err:       err,
				}
				span.RecordError(err)
				return nil, err
			}
			res.ProbeResults[probeName] = probeRes
		}

		s.cache.Store(hash, res)
		return res, nil
	})
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

		// TODO: we can further optimize this by passing the resolvable down the stack
		// as some probes won't need the executable.
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
			err = &process.TlsProbeError{
				ProbeName: probeName,
				Err:       err,
			}
			span.RecordError(err)
			return nil, err
		}
		closers = append(closers, closer)
	}

	return closers, nil
}

type ContainerScanResult struct {
	// SharedLibraries is a map of probe name to (library path -> scan result)
	SharedLibraries map[string]map[string]ProbeScanResult
}

// ScanContainer scans a container for shared libraries.
func (s *targetScanner) ScanContainer(ctx context.Context, id, root string) (*ContainerScanResult, error) {
	ctx, span := tracer.Start(ctx, "TargetScanner.ScanContainer")
	span.SetAttributes(
		attribute.String("container_id", id),
		attribute.String("root", root),
	)
	defer span.End()

	// NOTE: we assume that probes do not share libraries with each other.
	// In fact, currently only OpenSSL implements shared library scanning and attachment.
	// This keeps the caching logic simple.

	libResults := make(map[string]map[string]ProbeScanResult)

	for _, probe := range s.probes {
		libSearch := probe.SharedLibraries()
		if len(libSearch) == 0 {
			continue
		}
		paths, err := FindSharedLibrary(ctx, root, libSearch)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("scanning filesystem for shared libraries: %w", err)
		}

		for _, path := range paths {
			res, err := s.scanLibrary(ctx, probe, path)
			if err != nil {
				return nil, err
			}
			if libResults[probe.Name()] == nil {
				libResults[probe.Name()] = make(map[string]ProbeScanResult)
			}
			libResults[probe.Name()][path] = res
		}
	}

	return &ContainerScanResult{
		SharedLibraries: libResults,
	}, nil
}

func (s *targetScanner) scanLibrary(ctx context.Context, probe Probe, path string) (ProbeScanResult, error) {
	ctx, span := tracer.Start(ctx, "TargetScanner.scanLibrary")
	defer span.End()
	span.SetAttributes(attribute.String("path", path))

	ef, err := binutils.NewElf(ctx, path, "/", false)
	if err != nil {
		return nil, fmt.Errorf("opening library: %w", err)
	}
	defer ef.Close()

	hash, err := ef.Hash(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting hash: %w", err)
	}

	if cached, ok := s.cache.LoadAndRenew(hash); ok {
		res := cached.ProbeResults[probe.Name()]
		if res != nil {
			return res, nil
		}

		s.logger.Debug("invalid cached library scan result",
			zap.String("hash", hash),
			zap.String("probe", probe.Name()),
			zap.Any("result", cached.ProbeResults),
		)
		// rescan
	}

	res, err := probe.ScanLibrary(ctx, ef)
	if err != nil {
		err = &process.TlsProbeError{
			ProbeName: probe.Name(),
			Err:       fmt.Errorf("scanning library: %w", err),
		}
		span.RecordError(err)
		return nil, err
	}

	s.cache.Store(hash, &ScanResult{
		Hash:         hash,
		Mtime:        ef.Mtime().Unix(),
		ProbeResults: map[string]ProbeScanResult{probe.Name(): res},
	})
	return res, nil
}

func (s *targetScanner) AttachContainer(ctx context.Context, res *ContainerScanResult) (io.Closer, error) {
	if res == nil || len(res.SharedLibraries) == 0 {
		return nil, nil
	}

	ctx, span := tracer.Start(ctx, "TargetScanner.AttachContainer")
	defer span.End()

	var closers MultiCloser
	for probeName, libs := range res.SharedLibraries {
		probe := s.probes[probeName]
		if probe == nil {
			s.logger.Info("unrecognized probe in scan result", zap.String("probe_name", probeName))
			continue
		}

		for path, res := range libs {
			if !res.ProbeDetected() {
				continue
			}

			ex, err := link.OpenExecutable(path)
			if err != nil {
				err = &process.TlsProbeError{
					ProbeName: probeName,
					Err:       fmt.Errorf("opening library %s: %w", path, err),
				}
				span.RecordError(err)
				return nil, err
			}

			closer, err := probe.AttachLibrary(ctx, &ExeLibraryAttachable{
				Path: path,
				Exe:  ex,
			}, res)
			if err != nil {
				err = &process.TlsProbeError{
					ProbeName: probeName,
					Err:       fmt.Errorf("attaching library %s: %w", path, err),
				}
				span.RecordError(err)
				return nil, err
			}
			closers = append(closers, closer)
		}
	}

	return closers, nil
}
