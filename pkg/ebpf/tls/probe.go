package tls

import (
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"io"

	"github.com/cilium/ebpf/link"
	"github.com/hashicorp/go-multierror"
	"github.com/qpoint-io/qtap/pkg/binutils"
	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	"go.uber.org/zap"
)

// Probe describes a TLS probe that can be attached to a shared library or binary target.
//
//go:generate go tool go.uber.org/mock/mockgen --destination=probe_mock.go --package tls . Probe
type Probe interface {
	// Name returns the unique identifier for this probe (e.g., "openssl")
	Name() string

	// Scan performs detailed analysis of the binary to gather all information needed for attaching probes.
	// If the probe does not detect anything, it should return a ProbeScanResult with ProbeDetected() == false.
	//
	// The returned ProbeScanResult may be cached and reused across process restarts for future Attach calls.
	Scan(ctx context.Context, target *ExeElfScannable) (ProbeScanResult, error)

	// Attach attaches probes to the process using the scan result.
	//
	// Returns a Closer that must be called when the process exits to clean up the probes.
	Attach(ctx context.Context, target *ExeLinkAttachable, result ProbeScanResult) (io.Closer, error)

	// SharedLibraries returns the list of shared libraries this probe can attach to.
	// Each string is a library name prefix (e.g., "libssl.so", "libcrypto.so")
	SharedLibraries() []string

	// TODO: add ScanLibrary() and cache result

	// AttachLibrary attaches probes to a shared library.
	AttachLibrary(ctx context.Context, library *SharedLibrary) (io.Closer, error)

	// Close cleans up any global resources used by the probe.
	Close() error
}

// ExeScannable contains information about a binary available during the scan phase.
type ExeElfScannable struct {
	ExeScannable

	// Elf is the parsed ELF file. This the primary source of information in most cases.
	Elf *binutils.Elf
}

// ProbeScanResult contains the results of scanning a binary for TLS probe attachment points.
// Each probe defines its own concrete type that implements this interface.
type ProbeScanResult interface {
	// ProbeName returns the name of the probe that produced this result
	ProbeName() string

	// ProbeDetected returns true if the probe was detected in the binary.
	ProbeDetected() bool
}

type ExeAttachable struct {
	// PID is the process ID of the process that is being attached.
	PID int
	// Path is the path to the executable of the process that is being attached.
	Path string
	// Root is the root fs path of the process that is being attached.
	Root string
}

type ExeLinkAttachable struct {
	ExeAttachable

	Exe *link.Executable
}

// MultiCloser wraps multiple closers into a single Closer.
type MultiCloser []io.Closer

// Close closes all wrapped closers, collecting any errors
func (m MultiCloser) Close() error {
	var errs error
	for _, c := range m {
		if c == nil {
			continue
		}
		if err := c.Close(); err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	return errs
}

// AttachProbes is helper for attaching uprobes to a pre-processed list of symbols.
func AttachProbes[T interface {
	*link.Executable | *ExeLinkAttachable
}](
	ctx context.Context,
	logger *zap.Logger,
	target T,
	symbols []elf.Symbol,
	matchStrategy binutils.MatchStrategy,
	probes []*common.Uprobe,
	skipZeroAddresses bool,
) (io.Closer, error) {
	ctx, span := tracer.Start(ctx, "tls.AttachProbes")
	defer span.End()

	var closer MultiCloser
	var filter func(*elf.Symbol) bool

	if skipZeroAddresses {
		filter = func(sym *elf.Symbol) bool {
			if sym.Value == 0 || sym.Size == 0 {
				return false
			}
			return true
		}
	}

	var (
		exe *link.Executable
		ll  = logger
	)

	switch t := any(target).(type) {
	case *link.Executable:
		exe = t
	case *ExeLinkAttachable:
		exe = t.Exe
		ll = ll.With(zap.Int("pid", t.PID))
	}

	for _, probe := range probes {
		ll := ll.With(
			zap.String("function", probe.Function),
			zap.String("probe", probe.ID()),
			zap.String("ebpf_prog", probe.Prog.String()),
		)
		syms, err := binutils.FindSymbols(
			symbols, binutils.SymbolSearch{
				Name:          probe.Function,
				MatchStrategy: matchStrategy,
			}, filter)
		if err != nil {
			if !errors.Is(err, binutils.ErrNoSymbols) {
				ll.Debug("failed to find symbols", zap.Error(err))
			}
			continue
		}

		for _, sym := range syms {
			ll.Debug("attaching probe",
				zap.String("symbol", sym.Name),
				zap.Uint64("address", sym.Value),
				zap.Bool("is_ret", probe.IsRet),
			)
			if err := probe.Attach(ctx, exe, sym.Value); err != nil {
				return nil, fmt.Errorf("attaching probe %s: %w", probe.Function, err)
			}
			closer = append(closer, probe)
		}
	}

	return closer, nil
}

// CloserFunc is a helper type to implement the io.Closer interface.
type CloserFunc func() error

func (c CloserFunc) Close() error {
	return c()
}
