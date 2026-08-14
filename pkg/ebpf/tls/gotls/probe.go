package gotls

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/qpoint-io/qtap/pkg/binutils"
	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls/gotls/gobin"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.uber.org/zap"
)

const Name = "gotls"

var tracer = telemetry.Tracer()

// compile time check that Probe implements tls.Probe
var _ tls.Probe = (*Probe)(nil)

// Probe implements tls.Probe for Go TLS interception.
type Probe struct {
	logger      *zap.Logger
	symaddrsMap *ebpf.Map
	probeFn     func() []*common.Uprobe
}

// NewProbe creates a new GoTLS probe.
func NewProbe(logger *zap.Logger, symaddrsMap *ebpf.Map, probeFn func() []*common.Uprobe) *Probe {
	return &Probe{
		logger:      logger,
		symaddrsMap: symaddrsMap,
		probeFn:     probeFn,
	}
}

func (p *Probe) Name() string {
	return Name
}

// Scan analyzes the binary to find Go TLS symbols and offsets.
func (p *Probe) Scan(ctx context.Context, target *tls.ExeElfScannable) (tls.ProbeScanResult, error) {
	ctx, span := tracer.Start(ctx, "GoTLSProbe.Scan")
	defer span.End()

	sr, err := scan(ctx, p.logger, target.Elf)
	if err != nil {
		if errors.Is(err, binutils.ErrNoSymbols) ||
			errors.Is(err, ErrIncompleteSymaddrs) ||
			errors.Is(err, gobin.ErrUnsupportedGoVersion) ||
			errors.Is(err, gobin.ErrNotGoExecutable) {
			return &ScanResult{
				ContainsGoTLS: false,
			}, nil
		}

		return nil, err
	}

	return sr, nil
}

// Attach attaches probes to the process using the scan result.
func (p *Probe) Attach(ctx context.Context, target *tls.ExeLinkAttachable, result tls.ProbeScanResult) (io.Closer, error) {
	ctx, span := tracer.Start(ctx, "GoTLSProbe.Attach") //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()

	sr, ok := result.(*ScanResult)
	if !ok {
		p.logger.DPanic("invalid result type", zap.Any("result", result))
		return nil, errors.New("invalid result type: expected *ScanResult")
	}

	var closer tls.MultiCloser

	// push symbol offsets to eBPF map (keyed by PID)
	mapCloser, err := p.pushSymbolOffsets(target.PID, sr.Symaddrs)
	if err != nil {
		return nil, fmt.Errorf("pushing symbol offsets: %w", err)
	}
	closer = append(closer, mapCloser)

	// Attach probes
	probeCloser, err := p.attachProbes(target.Exe, sr.SSLFuncOffsets)
	closer = append(closer, probeCloser)
	if err != nil {
		if err := closer.Close(); err != nil {
			p.logger.Warn("failed to clean up after failed attach", zap.Error(err))
		}
		return nil, fmt.Errorf("attaching probes: %w", err)
	}

	return closer, nil
}

// SharedLibraries returns nil - Go TLS is statically compiled into binaries.
func (p *Probe) SharedLibraries() string {
	return ""
}

func (p *Probe) ScanLibrary(ctx context.Context, ef *binutils.Elf) (tls.ProbeScanResult, error) {
	return nil, nil
}

func (p *Probe) AttachLibrary(ctx context.Context, target *tls.ExeLibraryAttachable, result tls.ProbeScanResult) (io.Closer, error) {
	return nil, nil
}

func (p *Probe) Close() error {
	return nil
}

func (p *Probe) pushSymbolOffsets(pid int, symAddr *GoTLSSymAddr) (io.Closer, error) {
	if symAddr == nil {
		return nil, nil
	}

	err := p.symaddrsMap.Put(uint32(pid), *symAddr)
	if err != nil {
		return nil, fmt.Errorf("pushing symbol for pid %d: %w", pid, err)
	}

	return tls.CloserFunc(func() error {
		if err := p.symaddrsMap.Delete(uint32(pid)); err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
			return fmt.Errorf("deleting gotls symaddrs map item for pid %d: %w", pid, err)
		}
		return nil
	}), nil
}

func (p *Probe) attachProbes(ex *link.Executable, sslFuncOffsets []*gobin.Func) (io.Closer, error) {
	probes := p.probeFn()
	var closer tls.MultiCloser

	for _, sslFunc := range sslFuncOffsets {
		for _, probe := range probes {
			if strings.HasPrefix(sslFunc.Name, probe.Function) {
				probeCloser, err := p.attachProbe(ex, sslFunc, probe)
				if err != nil {
					return closer, fmt.Errorf("attaching probe for function %s (symbol: %s, isReturn: %v): %w",
						sslFunc.Name, probe.Function, probe.IsRet, err)
				}
				closer = append(closer, probeCloser)
			}
		}
	}

	return closer, nil
}

func (p *Probe) attachProbe(ex *link.Executable, sslFunc *gobin.Func, probe *common.Uprobe) (io.Closer, error) {
	var closer tls.MultiCloser

	if probe.IsRet {
		for _, offset := range sslFunc.ReturnOffsets {
			ln, err := ex.Uprobe(sslFunc.Name, probe.Prog, &link.UprobeOptions{Address: offset})
			if err != nil {
				return closer, fmt.Errorf("attaching return probe to %s: %w", sslFunc.Name, err)
			}
			closer = append(closer, ln)
		}
	} else {
		ln, err := ex.Uprobe(sslFunc.Name, probe.Prog, &link.UprobeOptions{Address: sslFunc.Offset})
		if err != nil {
			return closer, fmt.Errorf("attaching entry probe to %s: %w", sslFunc.Name, err)
		}
		closer = append(closer, ln)
	}

	return closer, nil
}
