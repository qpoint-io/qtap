package nodetls

import (
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/cilium/ebpf"
	"github.com/qpoint-io/qtap/pkg/binutils"
	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.uber.org/zap"
)

const Name = "nodetls"

var tracer = telemetry.Tracer()

// node.js symbols used for detection
var nodeSymbols []binutils.SymbolSearch

func init() {
	for _, sym := range []string{
		// node < v15.0.0
		"_ZN4node7TLSWrapC2E",
		"_ZN4node7TLSWrap7ClearInE",
		"_ZN4node7TLSWrap8ClearOutE",
		"_ZN4node7TLSWrapD1Ev",

		// node >= v15.0.0
		"_ZN4node6crypto7TLSWrapC2E",
		"_ZN4node6crypto7TLSWrap7ClearInE",
		"_ZN4node6crypto7TLSWrap8ClearOutE",
		"_ZN4node6crypto7TLSWrapD1Ev",
	} {
		nodeSymbols = append(nodeSymbols, binutils.SymbolSearch{
			Name:          sym,
			MatchStrategy: binutils.MatchStrategyPrefix,
		})
	}
}

// list of node.js major versions that are unsupported
var unsupportedMajorVersions = []string{"25"}

// compile time check that Probe implements tls.Probe
var _ tls.Probe = (*Probe)(nil)

// Probe implements tls.Probe for Node.js TLS interception.
type Probe struct {
	logger      *zap.Logger
	symaddrsMap *ebpf.Map
	probeFn     func() []*common.Uprobe
}

type ScanResult struct {
	ContainsNodeTLS bool
	Version         string
	Symaddr         *nodeTlsSymaddr
	Symbols         []elf.Symbol
}

// ProbeName implements tls.ProbeScanResult.
func (r *ScanResult) ProbeName() string {
	return Name
}

// ProbeDetected implements tls.ProbeScanResult.
func (r *ScanResult) ProbeDetected() bool {
	return r.ContainsNodeTLS && r.Symaddr != nil
}

// NewProbe creates a new NodeTLS probe.
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

// Scan analyzes the binary to detect Node.js and gather version info.
// Symbol search for attachment is deferred to Attach() to use probe-specific functions.
func (p *Probe) Scan(ctx context.Context, target *tls.ExeElfScannable) (tls.ProbeScanResult, error) {
	ctx, span := tracer.Start(ctx, "NodeTLSProbe.Scan")
	defer span.End()

	// Check if binary contains node symbols (detection only)
	contains, err := target.Elf.ContainsAnySymbols(ctx, nodeSymbols, elf.SHT_DYNSYM, elf.SHT_SYMTAB)
	if err != nil {
		if errors.Is(err, binutils.ErrNoSymbols) {
			return &ScanResult{ContainsNodeTLS: false}, nil
		}
		return nil, fmt.Errorf("checking for node symbols: %w", err)
	}
	if !contains {
		return &ScanResult{ContainsNodeTLS: false}, nil
	}

	version, err := p.getNodeVersion(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("getting node version: %w", err)
	}

	for _, v := range unsupportedMajorVersions {
		if strings.HasPrefix(version, v) {
			return &ScanResult{ContainsNodeTLS: false}, nil
		}
	}

	// Get symbol offsets based on version
	symaddrs, err := symAddrsFromVersion(version)
	if err != nil {
		return nil, fmt.Errorf("getting symaddrs from version [%s]: %w", version, err)
	}

	// Find the symbols from the binary based on probe functions
	symbols, err := target.Elf.SearchSymbols(ctx, nodeSymbols, elf.SHT_SYMTAB, elf.SHT_DYNSYM)
	if err != nil {
		return nil, fmt.Errorf("searching for symbols: %w", err)
	}

	// Calculate the addresses of the symbols
	symbols = target.Elf.CalculateUprobeAddresses(ctx, symbols)

	return &ScanResult{
		ContainsNodeTLS: true,
		Version:         version,
		Symaddr:         symaddrs,
		Symbols:         symbols,
	}, nil
}

// Attach attaches probes to the process using the scan result.
func (p *Probe) Attach(ctx context.Context, target *tls.ExeLinkAttachable, result tls.ProbeScanResult) (io.Closer, error) {
	ctx, span := tracer.Start(ctx, "NodeTLSProbe.Attach")
	defer span.End()

	ll := p.logger.With(zap.String("exe", target.Path))

	sr, ok := result.(*ScanResult)
	if !ok {
		ll.DPanic("invalid result type", zap.Any("result", result))
		return nil, errors.New("invalid result type: expected *ScanResult")
	}

	var closer tls.MultiCloser

	// push symbol offsets to eBPF map
	mapCloser, err := p.pushSymbolOffsets(target.PID, sr.Symaddr)
	if err != nil {
		return nil, fmt.Errorf("pushing symbol offsets: %w", err)
	}
	closer = append(closer, mapCloser)

	// attach probes
	probeCloser, err := tls.AttachProbes(ctx, ll, target, sr.Symbols, binutils.MatchStrategyPrefix, p.probeFn(), true)
	if err != nil {
		if err := closer.Close(); err != nil {
			ll.Warn("failed to clean up after failed attach", zap.Error(err))
		}
		return nil, fmt.Errorf("attaching probes: %w", err)
	}
	closer = append(closer, probeCloser)

	return closer, nil
}

// SharedLibraries returns nil - Node TLS is compiled into the node binary.
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

// getNodeVersion gets the node version using exec (primary, fast) or binary string search (fallback, slow).
func (p *Probe) getNodeVersion(ctx context.Context, target *tls.ExeElfScannable) (string, error) {
	ctx, span := tracer.Start(ctx, "NodeTLSProbe.getNodeVersion")
	defer span.End()

	ll := p.logger.With(zap.String("path", target.Path))

	// try exec first
	version, err := p.getVersionByExec(ctx, target)
	if err != nil {
		ll.Debug("failed to get node version by exec, trying binary search", zap.Error(err))
	} else {
		return version, nil
	}

	version, err = p.getVersionFromBinary(ctx, target.Elf)
	if err != nil {
		return "", fmt.Errorf("getting node version from binary: %w", err)
	}

	return version, nil
}

// getVersionByExec gets the node version by executing `node --version`.
func (p *Probe) getVersionByExec(ctx context.Context, target *tls.ExeElfScannable) (string, error) {
	ctx, span := tracer.Start(ctx, "NodeTLSProbe.getVersionByExec")
	defer span.End()

	args := []string{"--version"}
	nodePath := "node"
	if len(target.Cmdline) > 0 {
		nodePath = target.Cmdline[0]

		if filepath.Base(nodePath) != "node" {
			// best effort attempt
			nodePath = "/usr/bin/env"
			args = []string{"node", "--version"}
		}
	}
	cmd := exec.CommandContext(ctx, nodePath, args...)
	cmd.Env = []string{"QPOINT_STRATEGY=ignore"} // avoid infinite loop
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Chroot: target.Root,
		Credential: &syscall.Credential{
			Uid: 65534, // nobody
			Gid: 65534, // nogroup
		},
	}

	res, err := cmd.Output()
	if err != nil {
		err = fmt.Errorf("running node version command [%s]: %w", cmd.String(), err)
		span.RecordError(err)
		return "", err
	}

	version := strings.TrimLeft(strings.Trim(string(res), "\n"), "v")
	if version == "" {
		err = errors.New("empty node version")
		span.RecordError(err)
		return "", err
	}

	return version, nil
}

func (p *Probe) getVersionFromBinary(ctx context.Context, ef *binutils.Elf) (string, error) {
	ctx, span := tracer.Start(ctx, "NodeTLSProbe.getVersionFromBinary") //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()

	const nodeVersionPrefix = "node.js/v"

	elfVer, err := ef.SearchString(nodeVersionPrefix, binutils.MatchStrategyPrefix)
	if err != nil {
		return "", fmt.Errorf("searching for node version: %w", err)
	}

	elfVer = strings.TrimPrefix(elfVer, nodeVersionPrefix)
	elfVer = strings.TrimSpace(elfVer)
	if elfVer == "" {
		return "", errors.New("empty node version")
	}

	return elfVer, nil
}

func (p *Probe) pushSymbolOffsets(pid int, symAddr *nodeTlsSymaddr) (io.Closer, error) {
	if symAddr == nil {
		return nil, nil
	}

	err := p.symaddrsMap.Put(uint32(pid), *symAddr)
	if err != nil {
		return nil, fmt.Errorf("pushing symbol for pid %d: %w", pid, err)
	}

	return tls.CloserFunc(func() error {
		if err := p.symaddrsMap.Delete(uint32(pid)); err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
			return fmt.Errorf("deleting nodetls symaddrs map item for pid %d: %w", pid, err)
		}
		return nil
	}), nil
}
