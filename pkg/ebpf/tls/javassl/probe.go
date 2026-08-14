package javassl

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/qpoint-io/qtap/pkg/binutils"
	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.uber.org/zap"
)

const (
	Name             = "javassl"
	AgentInstallPath = "/var/lib/qtap/javassl"
)

var (
	// Matches any file path ending with:
	//   - "java" (e.g., /usr/bin/java)
	//   - "javaw" (though less common on Linux)
	//   - "java" / "javaw" followed by version digits (e.g., java8, java11, java17).
	exeRegex = regexp.MustCompile(`(?:^|/)javaw?(?:\d+)?$`)

	// Matches any lib with:
	//   - libjvm.so
	//   - libjava.so
	//   - libjli.so
	javaLibsRegex = regexp.MustCompile(`^libjvm\.so$|^libjava\.so$|^libjli\.so$`)

	tracer = telemetry.Tracer()
)

// compile time check that Probe implements tls.Probe
var _ tls.Probe = (*Probe)(nil)

// Probe implements tls.Probe for Java SSL interception.
type Probe struct {
	logger           *zap.Logger
	probeFn          func() []*common.Uprobe
	loader           *loader
	sslEngineManager *SslEngineManager
	libqtapSymbols   *libQtapSymbols
	ctx              context.Context
	cancel           context.CancelFunc
}

type ScanResult struct {
	ContainsJavaSSL bool
}

func (r *ScanResult) ProbeName() string {
	return Name
}

func (r *ScanResult) ProbeDetected() bool {
	return r.ContainsJavaSSL
}

func NewProbe(ctx context.Context, logger *zap.Logger, sslEngineManager *SslEngineManager, probeFn func() []*common.Uprobe) *Probe {
	ctx, cancel := context.WithCancel(ctx)
	return &Probe{
		ctx:              ctx,
		cancel:           cancel,
		logger:           logger,
		sslEngineManager: sslEngineManager,
		probeFn:          probeFn,
		loader:           newLoader(ctx, AgentInstallPath),
		libqtapSymbols:   &libQtapSymbols{},
	}
}

func (p *Probe) Name() string {
	return Name
}

// Scan analyzes the binary to find Java SSL symbols and offsets.
func (p *Probe) Scan(ctx context.Context, target *tls.ExeElfScannable) (tls.ProbeScanResult, error) {
	ctx, span := tracer.Start(ctx, "JavaSSLProbe.Scan")
	defer span.End()

	for _, arg := range target.Cmdline {
		if strings.Contains(arg, "qtap-loader.jar") {
			// p.logger.Debug("found qtap-loader.jar in command line - ignoring", zap.Strings("arg", target.Cmdline))
			// this is us installing the agent, stop here to prevent an infinite loop

			// TODO: do not cache this result
			return &ScanResult{
				ContainsJavaSSL: false,
			}, nil
		}
	}

	isJava, err := p.isJavaProcess(ctx, target)
	if err != nil {
		return nil, err
	}

	return &ScanResult{
		ContainsJavaSSL: isJava,
	}, nil
}

// Attach attaches probes to the process using the scan result.
func (p *Probe) Attach(ctx context.Context, target *tls.ExeLinkAttachable, result tls.ProbeScanResult) (io.Closer, error) {
	ctx, span := tracer.Start(ctx, "JavaSSLProbe.Attach")
	defer span.End()

	// ensure the global javassl loader is installed
	if err := p.loader.Install(ctx); err != nil {
		return nil, fmt.Errorf("installing loader: %w", err)
	}

	var closer tls.MultiCloser

	// install the process-specific javassl agent
	runDir := fmt.Sprintf("/run/qtap/%d", target.PID)
	agentCloser, err := p.installAgent(
		ctx,
		filepath.Join(target.Root, runDir),
		runDir,
		target.PID,
	)
	if err != nil {
		if err := agentCloser.Close(); err != nil {
			p.logger.Warn("failed to clean up after failed install", zap.Error(err))
		}
		return nil, fmt.Errorf("installing agent: %w", err)
	}
	closer = append(closer, agentCloser)

	// register the process with the ssl engine manager
	if err := p.sslEngineManager.ProcessStarted(target.PID); err != nil {
		if err := closer.Close(); err != nil {
			p.logger.Warn("failed to clean up after failed sslEngineManager.ProcessStarted", zap.Error(err))
		}
		return nil, fmt.Errorf("registering process with ssl engine manager: %w", err)
	}
	closer = append(closer, tls.CloserFunc(func() error {
		return p.sslEngineManager.ProcessStopped(target.PID)
	}))

	return closer, nil
}

// SharedLibraries is not applicable for Java SSL.
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
	p.cancel()

	var eg multierror.Group
	eg.Go(func() error {
		if err := p.sslEngineManager.Stop(); err != nil {
			return fmt.Errorf("stopping ssl engine manager: %w", err)
		}
		return nil
	})

	eg.Go(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := p.loader.Uninstall(ctx); err != nil {
			return fmt.Errorf("uninstalling loader: %w", err)
		}
		return nil
	})

	return eg.Wait().ErrorOrNil()
}

func (p *Probe) isJavaProcess(ctx context.Context, target *tls.ExeElfScannable) (bool, error) {
	ll := p.logger

	var exe string
	if len(target.Cmdline) > 0 {
		exe = target.Cmdline[0]
		ll = ll.With(zap.String("exe", exe))
	}

	// check the executable name
	if exe != "" && exeRegex.MatchString(exe) {
		ll.Debug("java process - found executable")
		return true, nil
	}

	// check the linked libraries
	libs, err := target.Elf.Ldd(ctx)
	if err != nil {
		return false, err
	}

	// check the linked libraries
	for _, lib := range libs {
		if javaLibsRegex.MatchString(lib) {
			ll.Debug("java process - found linked library", zap.String("lib", lib))
			return true, nil
		}
	}

	return false, nil
}
