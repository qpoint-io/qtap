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

var _ tls.Probe = (*Probe)(nil)

const (
	Name = "openssl"
	// LibSSL is the shared library name for OpenSSL
	LibSSL = "libssl.so"
)

var tracer = telemetry.Tracer()

// OpenSSL symbolSearch we hook
var symbolSearch []binutils.SymbolSearch

func init() {
	for _, sym := range []string{
		"SSL_read",
		"SSL_read_ex",
		"SSL_write",
		"SSL_write_ex",
		"SSL_free",
		"SSL_new",
		"SSL_set_cert_cb",
	} {
		symbolSearch = append(symbolSearch, binutils.SymbolSearch{
			Name:          sym,
			MatchStrategy: binutils.MatchStrategyExact,
		})
	}
}

// Probe implements tls.Probe for OpenSSL TLS interception.
type Probe struct {
	logger  *zap.Logger
	probeFn func() []*common.Uprobe
}

type OpenSSLScanResult struct {
	// Symbols contains the found SSL symbols with their addresses.
	Symbols []elf.Symbol
}

// ProbeName implements tls.ProbeScanResult.
func (r *OpenSSLScanResult) ProbeName() string {
	return Name
}

// ProbeDetected implements tls.ProbeScanResult.
func (r *OpenSSLScanResult) ProbeDetected() bool {
	return len(r.Symbols) > 0
}

func NewProbe(logger *zap.Logger, probeFn func() []*common.Uprobe) *Probe {
	return &Probe{
		logger:  logger,
		probeFn: probeFn,
	}
}

func (s *Probe) Name() string {
	return Name
}

// Scan analyzes the binary to find symbol addresses.
func (s *Probe) Scan(ctx context.Context, target *tls.ExeElfScannable) (tls.ProbeScanResult, error) {
	ctx, span := tracer.Start(ctx, "OpenSSLScanner.Scan")
	defer span.End()

	// find the symbols from the binary
	syms, err := target.Elf.SearchSymbols(ctx, symbolSearch, elf.SHT_SYMTAB, elf.SHT_DYNSYM)
	if err != nil && !errors.Is(err, binutils.ErrNoSymbols) {
		return nil, fmt.Errorf("searching symbols: %w", err)
	}

	// calculate the addresses of the symbols
	syms = target.Elf.CalculateUprobeAddresses(ctx, syms)

	return &OpenSSLScanResult{
		Symbols: syms,
	}, nil
}

// Attach attaches uprobes to the binary.
func (s *Probe) Attach(ctx context.Context, target *tls.ExeLinkAttachable, result tls.ProbeScanResult) (io.Closer, error) {
	ctx, span := tracer.Start(ctx, "OpenSSLScanner.Attach")
	defer span.End()

	ll := s.logger.With(zap.String("exe", target.Path))

	r, ok := result.(*OpenSSLScanResult)
	if !ok {
		// code error, should never happen
		ll.DPanic("invalid result type", zap.Any("result", result))
		return nil, errors.New("invalid result type: expected *OpenSSLScanResult")
	}

	return tls.AttachProbes(ctx, ll, target, r.Symbols, binutils.MatchStrategyExact, s.probeFn(), true)
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
	ctx, span := tracer.Start(ctx, "OpenSSLScanner.attachLibrary")
	defer span.End()
	span.SetAttributes(attribute.String("name", name), attribute.String("path", path))

	ll := s.logger.With(zap.String("exe", path))

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

	symbols, err := ef.SearchSymbols(ctx, symbolSearch, elf.SHT_SYMTAB, elf.SHT_DYNSYM)
	if err != nil && !errors.Is(err, binutils.ErrNoSymbols) {
		ll.Debug("failed to search symbols", zap.Error(err))
	}
	symbols = ef.CalculateUprobeAddresses(ctx, symbols)

	closer, err := tls.AttachProbes(ctx, ll, ex, symbols, binutils.MatchStrategyExact, s.probeFn(), false)
	if err != nil {
		return nil, fmt.Errorf("attaching probes: %w", err)
	}

	ll.Info("attached OpenSSL probes (shared)",
		zap.String("name", name),
		zap.String("path", path),
	)

	return closer, nil
}

func (s *Probe) SharedLibraries() []string {
	return []string{LibSSL}
}

func (s *Probe) Close() error {
	return nil
}
