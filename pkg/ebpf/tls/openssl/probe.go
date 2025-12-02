package openssl

import (
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"io"

	"github.com/cilium/ebpf/link"
	"github.com/qpoint-io/qtap/pkg/binutils"
	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

const (
	// LibSSL is the shared library name for OpenSSL
	LibSSL = "libssl.so"
)

var tracer = telemetry.Tracer()

// OpenSSL symbols we hook
var symbols []binutils.SymbolSearch

func init() {
	for _, sym := range []string{
		"SSL_read",
		"SSL_write",
		"SSL_read_ex",
		"SSL_write_ex",
		"SSL_free",
	} {
		symbols = append(symbols, binutils.SymbolSearch{
			Name:             sym,
			MatchStrategy:    binutils.MatchStrategyExact,
			CalculateAddress: true,
		})
	}
}

// Probe implements tls.Scanner and tls.LibraryAttacher for OpenSSL TLS interception.
// It handles both statically linked OpenSSL and dynamically linked libssl.so.
type Probe struct {
	logger  *zap.Logger
	probeFn func() []*common.Uprobe
}

// OpenSSLScanResult implements tls.ScanResult and tls.LibraryScanResult for OpenSSL.
type OpenSSLScanResult struct {
	// Symbols contains the found SSL symbols with their addresses.
	Symbols []elf.Symbol
}

// ProbeType implements tls.ScanResult.
func (r *OpenSSLScanResult) ProbeType() string {
	return "openssl"
}

func (r *OpenSSLScanResult) ProbeDetected() bool {
	return len(r.Symbols) > 0
}

// NewProbe creates a new OpenSSL probe.
func NewProbe(logger *zap.Logger, probeFn func() []*common.Uprobe) *Probe {
	return &Probe{
		logger:  logger,
		probeFn: probeFn,
	}
}

func (s *Probe) Name() string {
	return "openssl"
}

func (s *Probe) Symbols() ([]binutils.SymbolSearch, []elf.SectionType) {
	return symbols, []elf.SectionType{elf.SHT_SYMTAB}
}

// Scan analyzes the binary to find symbol addresses.
func (s *Probe) Scan(ctx context.Context, bin *binutils.Binary) (tls.ScanResult, error) {
	ctx, span := tracer.WithoutCancel(ctx, "OpenSSLScanner.Scan") //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()

	result := &OpenSSLScanResult{}

	// Check for static linking: symbols with non-zero Value AND Size
	for _, sym := range bin.Symtab {
		for _, sslSym := range sslSymbols {
			if sym.Name == sslSym && sym.Value != 0 && sym.Size != 0 {
				result.Symbols = append(result.Symbols, sym)
			}
		}
	}

	return result, nil
}

// Attach attaches uprobes to the binary.
// pidExe is the path to /proc/PID/exe for opening the executable.
// For shared libraries, the Orchestrator calls AttachLibrary instead.
func (s *Probe) Attach(ctx context.Context, pidExe string, bin *binutils.Binary, result tls.ScanResult) (io.Closer, error) {
	ctx, span := tracer.WithoutCancel(ctx, "OpenSSLScanner.Attach")
	defer span.End()
	span.SetAttributes(attribute.String("pid_exe", pidExe))

	r, ok := result.(*OpenSSLScanResult)
	if !ok {
		// code error, should never happen
		s.logger.DPanic("invalid result type", zap.Any("result", result))
		return nil, errors.New("invalid result type: expected *OpenSSLScanResult")
	}

	// Open the executable for uprobe attachment
	ex, err := link.OpenExecutable(pidExe)
	if err != nil {
		return nil, fmt.Errorf("opening executable: %w", err)
	}

	// Calculate uprobe addresses
	// TODO: can we do this in Scan() and cache?
	symbols := bin.CalculateUprobeAddresses(r.Symbols)

	// Create probes
	probes := s.probeFn()

	var closer tls.MultiCloser
	// Attach each probe
	for _, probe := range probes {
		for _, sym := range symbols {
			if sym.Name == probe.Function {
				if !probe.IsRet {
					s.logger.Debug("attaching OpenSSL probe !IsRet",
						zap.String("pid_exe", pidExe),
						zap.String("function", probe.Function),
						zap.Uint64("address", sym.Value),
					)
				}

				if err := probe.Attach(ctx, ex, sym.Value); err != nil {
					return nil, fmt.Errorf("attaching probe %s: %w", probe.Function, err)
				}
				closer = append(closer, &tls.DetacherCloser{Detacher: probe})
				break
			}
		}
	}

	return closer, nil
}

// AttachLibrary implements tls.LibraryAttacher.
// Attaches uprobes to a shared libssl.so library.
// Called by the Orchestrator for shared library attachment.
func (s *Probe) AttachLibrary(ctx context.Context, library *tls.SharedLibrary) (io.Closer, error) {
	var closers tls.MultiCloser
	for _, path := range library.Paths {
		closer, err := s.attachLibrary(ctx, library.Name, path)
		if err != nil {
			return nil, err
		}
		closers = append(closers, closer)
	}
	return closers, nil
}

func (s *Probe) attachLibrary(ctx context.Context, name, path string) (io.Closer, error) {
	ctx, span := tracer.WithoutCancel(ctx, "OpenSSLScanner.attachLibrary")
	defer span.End()
	span.SetAttributes(attribute.String("name", name), attribute.String("path", path))

	// Open the library executable
	ex, err := link.OpenExecutable(path)
	if err != nil {
		return nil, fmt.Errorf("opening library: %w", err)
	}

	// Parse the library to find symbols
	ef, err := binutils.NewElf(ctx, path, "/", false)
	if err != nil {
		return nil, fmt.Errorf("parsing library ELF: %w", err)
	}
	defer ef.Close()

	// Search for SSL symbols
	search := make([]binutils.SymbolSearch, len(sslSymbols))
	for i, sym := range sslSymbols {
		search[i] = binutils.SymbolSearch{
			Name:          sym,
			MatchStrategy: binutils.MatchStrategyExact,
		}
	}
	// TODO: can we have all probes return their symbols in Scan()
	// and have the orchestrator run a single search?

	symbols, err := ef.SearchSymbols(ctx, search, elf.SHT_SYMTAB, elf.SHT_DYNSYM)
	if err != nil && !errors.Is(err, binutils.ErrNoSymbols) {
		s.logger.Debug("failed to search symbols", zap.Error(err))
	}

	// Calculate uprobe addresses
	symbols = ef.CalculateUprobeAddresses(ctx, symbols)

	// Create and attach probes
	probes := s.probeFn()

	var closer tls.MultiCloser
	for _, probe := range probes {
		for _, sym := range symbols {
			if sym.Name == probe.Function {
				if !probe.IsRet {
					s.logger.Debug("attaching OpenSSL probe (shared)",
						zap.String("name", name),
						zap.String("path", path),
						zap.String("function", probe.Function),
						zap.Uint64("address", sym.Value),
					)
				}

				if err := probe.Attach(ctx, ex, sym.Value); err != nil {
					return nil, fmt.Errorf("attaching probe %s: %w", probe.Function, err)
				}
				closer = append(closer, &tls.DetacherCloser{Detacher: probe})
				break
			}
		}
	}

	s.logger.Info("attached OpenSSL probes (shared)",
		zap.String("name", name),
		zap.String("path", path),
	)

	return closer, nil
}

func (s *Probe) SharedLibraries() []string {
	return []string{LibSSL}
}
