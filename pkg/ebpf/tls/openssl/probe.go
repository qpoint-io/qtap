package openssl

import (
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"io"

	"github.com/qpoint-io/qtap/pkg/binutils"
	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.uber.org/zap"
)

var _ tls.Probe = (*Probe)(nil)

const (
	Name = "openssl"
	// LibSSL is the shared library name for OpenSSL
	LibSSL = "libssl"
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
		"SSL_set_fd",
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

func (s *Probe) SharedLibraries() string {
	return LibSSL
}

func (s *Probe) ScanLibrary(ctx context.Context, ef *binutils.Elf) (tls.ProbeScanResult, error) {
	ctx, span := tracer.Start(ctx, "OpenSSLScanner.ScanLibrary")
	defer span.End()

	syms, err := ef.SearchSymbols(ctx, symbolSearch, elf.SHT_SYMTAB, elf.SHT_DYNSYM)
	if err != nil && !errors.Is(err, binutils.ErrNoSymbols) {
		return nil, fmt.Errorf("searching symbols: %w", err)
	}
	syms = ef.CalculateUprobeAddresses(ctx, syms)

	return &OpenSSLScanResult{
		Symbols: syms,
	}, nil
}

// Attaches uprobes to a shared libssl.so library.
func (s *Probe) AttachLibrary(ctx context.Context, target *tls.ExeLibraryAttachable, result tls.ProbeScanResult) (io.Closer, error) {
	ctx, span := tracer.Start(ctx, "OpenSSLScanner.attachLibrary")
	defer span.End()

	ll := s.logger.With(zap.String("path", target.Path))

	r, ok := result.(*OpenSSLScanResult)
	if !ok {
		// code error, should never happen
		ll.DPanic("invalid result type", zap.Any("result", result))
		return nil, errors.New("invalid result type: expected *OpenSSLScanResult")
	}

	closer, err := tls.AttachProbes(ctx, ll, target, r.Symbols, binutils.MatchStrategyExact, s.probeFn(), false)
	if err != nil {
		return nil, fmt.Errorf("attaching probes: %w", err)
	}

	ll.Debug("attached OpenSSL probes (shared)")

	return closer, nil
}

func (s *Probe) Close() error {
	return nil
}
