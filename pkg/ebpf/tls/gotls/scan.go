package gotls

import (
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"strings"

	version "github.com/hashicorp/go-version"
	"github.com/qpoint-io/qtap/pkg/binutils"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls/gotls/gobin"
	"go.uber.org/zap"
)

const (
	// symbols
	SymbolSSLRead             = "crypto/tls.(*Conn).Read"
	SymbolSSLWrite            = "crypto/tls.(*Conn).Write"
	SymbolInternalSyscallConn = "google.golang.org/grpc/credentials/internal.syscallConn,net.Conn"
	SymbolTLSConn             = "itab.*crypto/tls.Conn,net.Conn"
	SymbolNetTCPConn          = "itab.*net.TCPConn,net.Conn"
)

var (
	// custom error if we can't ascertain the symaddrs
	ErrIncompleteSymaddrs = errors.New("incomplete symaddrs")

	// search for the types that will give us a papertrail to the file descriptors
	symbolSearch = []binutils.SymbolSearch{
		{Name: SymbolSSLRead, MatchStrategy: binutils.MatchStrategySuffix},
		{Name: SymbolSSLWrite, MatchStrategy: binutils.MatchStrategySuffix},
		{Name: SymbolInternalSyscallConn, MatchStrategy: binutils.MatchStrategySuffix},
		{Name: SymbolTLSConn, MatchStrategy: binutils.MatchStrategySuffix},
		{Name: SymbolNetTCPConn, MatchStrategy: binutils.MatchStrategySuffix},
	}

	// func search
	funcSearch = map[string]any{
		SymbolSSLRead:  nil,
		SymbolSSLWrite: nil,
	}
)

// scanner is responsible for analyzing binaries to find symbols, offsets, and functions
type scanner struct {
	logger *zap.Logger
}

// ScanResult represents the cached result of a GoTLS scan.
// Implements tls.ProbeScanResult interface.
type ScanResult struct {
	ContainsGoTLS  bool
	CurrentVersion string
	Symbols        []elf.Symbol
	Symaddrs       *GoTLSSymAddr
	SSLFuncOffsets []*gobin.Func
}

// ProbeName implements tls.ProbeScanResult.
func (r *ScanResult) ProbeName() string {
	return Name
}

// ProbeDetected implements tls.ProbeScanResult.
func (r *ScanResult) ProbeDetected() bool {
	return r.ContainsGoTLS
}

// scan performs all of the binary analysis to locate symbols, offsets, and functions
func scan(ctx context.Context, logger *zap.Logger, ef *binutils.Elf) (*ScanResult, error) {
	s := &scanner{
		logger: logger,
	}

	r := &ScanResult{}

	f, err := ef.Elf(ctx)
	if err != nil {
		return nil, err
	}

	currentVersion, err := s.getVersion(f)
	if err != nil {
		return nil, fmt.Errorf("getting version: %w", err)
	}
	r.CurrentVersion = currentVersion.String()

	if !gobin.SupportedGoVersion(r.CurrentVersion) {
		return nil, fmt.Errorf("unsupported Go version: %s", r.CurrentVersion)
	}
	r.ContainsGoTLS = true

	symbols, err := s.populateSymbols(ctx, ef)
	if err != nil {
		return nil, fmt.Errorf("populating symbols: %w", err)
	}
	r.Symbols = symbols

	symaddrs, err := s.populateSymaddrs(ctx, currentVersion, symbols)
	if err != nil {
		return nil, fmt.Errorf("populating symbol addresses: %w", err)
	}
	r.Symaddrs = symaddrs

	sslFuncOffsets, err := s.getFunctionOffsets(ctx, ef, funcSearch)
	if err != nil {
		return nil, fmt.Errorf("getting or finding function offsets: %w", err)
	}
	r.SSLFuncOffsets = sslFuncOffsets

	return r, nil
}

func (s *scanner) getVersion(f *elf.File) (*version.Version, error) {
	currentVersion, err := gobin.GetGoDetails(f)
	if err != nil {
		return nil, err
	}
	return version.NewVersion(strings.TrimPrefix(currentVersion, "go"))
}

func (s *scanner) populateSymbols(ctx context.Context, ef *binutils.Elf) ([]elf.Symbol, error) {
	ctx, span := tracer.Start(ctx, "gotls.populateSymbols")
	defer span.End()

	symbols, err := ef.SearchSymbols(ctx, symbolSearch, elf.SHT_SYMTAB)
	if err != nil {
		if strings.Contains(err.Error(), "section .symtab not found") {
			return nil, binutils.ErrNoSymbols
		}
		return nil, fmt.Errorf("searching symbols: %w", err)
	}
	return symbols, nil
}

func (s *scanner) populateSymaddrs(ctx context.Context, currentVersion *version.Version, symbols []elf.Symbol) (*GoTLSSymAddr, error) {
	ctx, span := tracer.Start(ctx, "gotls.populateSymaddrs") //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()

	symaddrs := &GoTLSSymAddr{}
	if err := InjectPrecomputedSymAddrsFromVersion(currentVersion, symaddrs); err != nil {
		return nil, fmt.Errorf("getting symbol addresses: %w", err)
	}
	symaddrs.populateCommonTypeAddrs(symbols)

	if err := symaddrs.validateSymaddrs(); err != nil {
		return nil, ErrIncompleteSymaddrs
	}
	return symaddrs, nil
}

func (s *scanner) getFunctionOffsets(ctx context.Context, ef *binutils.Elf, funcSearch map[string]any) ([]*gobin.Func, error) {
	sslFuncOffsets, err := findFunctionOffsets(ctx, ef, funcSearch)
	if err != nil {
		return nil, fmt.Errorf("getting function offsets: %w", err)
	}

	return sslFuncOffsets, nil
}

// findFunctionOffsets searches for function offsets in the binary
func findFunctionOffsets(ctx context.Context, ef *binutils.Elf, symbols map[string]any) ([]*gobin.Func, error) {
	ctx, span := tracer.Start(ctx, "gotls.findFunctionOffsets")
	defer span.End()

	symbolSearch := make([]binutils.SymbolSearch, 0, len(symbols))
	for k := range symbols {
		symbolSearch = append(symbolSearch, binutils.SymbolSearch{Name: k, MatchStrategy: binutils.MatchStrategyExact})
	}

	syms, err := ef.SearchSymbols(ctx, symbolSearch, elf.SHT_SYMTAB)
	if err != nil {
		return nil, fmt.Errorf("searching symbols: %w", err)
	}

	f, err := ef.Elf(ctx)
	if err != nil {
		return nil, err
	}

	result, err := gobin.FindFunctionsUnStripped(f, syms)
	if err != nil {
		// if the symbols are missing, try to find them stripped
		if errors.Is(err, elf.ErrNoSymbols) {
			return gobin.FindFunctionsStripped(f, symbols)
		}
		return nil, fmt.Errorf("finding functions: %w", err)
	}

	return result, nil
}
