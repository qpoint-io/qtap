package javassl

/*

	The agent contains per-process libraries and tools for ssl introspection.

*/

import (
	"context"
	"debug/elf"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/cilium/ebpf/link"
	"github.com/qpoint-io/qtap/pkg/binutils"
	"github.com/qpoint-io/qtap/pkg/ebpf/tls"
	"go.uber.org/zap"
)

//go:embed dist/java-ssl.jar
var javaSslJar []byte // the java side (package/class definition) of the native c extension (bridge)

//go:embed dist/qtap.jar
var qtapJar []byte // inserts bytecode at SSL read/write entry/exit points and forwards to the native c extension (bridge)

//go:embed dist/libqtap.so
var jvmiExtension []byte // the native c extension (bridge) that uprobes can attach to

var libQtapSymbolSearch []binutils.SymbolSearch

func init() {
	for _, sym := range []string{
		"ssl_read_entry",
		"ssl_write_entry",
		"ssl_read_exit",
		"ssl_write_exit",
		"ssl_engine_wrap_exit",
		"ssl_engine_unwrap_exit",
	} {
		libQtapSymbolSearch = append(libQtapSymbolSearch, binutils.SymbolSearch{
			Name:          sym,
			MatchStrategy: binutils.MatchStrategyExact,
		})
	}
}

// localDir is the path that is accessible to the user's java process. e.g. /run/qtap/<pid>
// hostDir is the path to the same directory but on the host. e.g. /proc/1/root/var/lib/docker/overlay2/.../merged/run/qtap/<pid>
func (p *Probe) installAgent(ctx context.Context, hostDir, localDir string, pid int) (io.Closer, error) {
	var closer tls.MultiCloser

	if ctx.Err() != nil {
		return closer, ctx.Err()
	}

	ctx, span := tracer.Start(ctx, "javassl.installAgent")
	defer span.End()

	/*

		write files

	*/

	if err := os.MkdirAll(filepath.Dir(hostDir), 0755); err != nil {
		return closer, fmt.Errorf("creating agent parent directory: %w", err)
	}
	// A PID-scoped directory proves cleanup ownership and avoids deleting
	// pre-existing operator data.
	if err := os.Mkdir(hostDir, 0755); err != nil {
		return closer, fmt.Errorf("creating agent directory %s: %w", localDir, err)
	}
	closer = append(closer, tls.CloserFunc(func() error {
		return os.RemoveAll(hostDir)
	}))
	if err := ValidateExecutionBasePath(hostDir); err != nil {
		return closer, fmt.Errorf("validating agent directory %s: %w", localDir, err)
	}

	// write the jvmi extension to the rundir
	jvmiPath := filepath.Join(hostDir, "libqtap.so")
	if err := os.WriteFile(jvmiPath, jvmiExtension, 0644); err != nil {
		return closer, fmt.Errorf("writing jvmi extension: %w", err)
	}

	// write the qtap jar file to the rundir
	qtapJarPath := filepath.Join(hostDir, "qtap.jar")
	if err := os.WriteFile(qtapJarPath, qtapJar, 0644); err != nil {
		return closer, fmt.Errorf("writing qtap jar file: %w", err)
	}

	// write the java-ssl jar file to the rundir
	javaSslJarPath := filepath.Join(hostDir, "java-ssl.jar")
	if err := os.WriteFile(javaSslJarPath, javaSslJar, 0644); err != nil {
		return closer, fmt.Errorf("writing java-ssl jar file: %w", err)
	}

	/*

		attach libqtap.so probes

	*/
	if ctx.Err() != nil {
		return closer, ctx.Err()
	}

	syms, err := p.libqtapSymbols.Get(ctx, jvmiPath)
	if err != nil {
		return closer, fmt.Errorf("getting symbols: %w", err)
	}

	lib, err := link.OpenExecutable(jvmiPath)
	if err != nil {
		return closer, fmt.Errorf("opening libqtap.so: %w", err)
	}

	probeCloser, err := tls.AttachProbes(ctx, p.logger, &tls.ExeLinkAttachable{
		Exe: lib,
		ExeAttachable: tls.ExeAttachable{ // this is used for logging
			PID:  pid,
			Path: jvmiPath,
		},
	}, syms, binutils.MatchStrategyExact, p.probeFn(), false)
	if err != nil {
		return closer, fmt.Errorf("attaching probes: %w", err)
	}
	closer = append(closer, probeCloser)

	/*

		inject the agent into the process

	*/
	if ctx.Err() != nil {
		return closer, ctx.Err()
	}

	// localDir is the path that is accessible to the user's java process.
	// this should be relative to the container java is running in.
	if err := p.injectAgent(ctx, localDir, pid); err != nil {
		return closer, fmt.Errorf("injecting agent: %w", err)
	}

	return closer, nil
}

func (p *Probe) injectAgent(ctx context.Context, dir string, pid int) error {
	ctx, span := tracer.Start(ctx, "javassl.injectAgent")
	defer span.End()

	// run the loader inside our custom JRE environment
	javaHome := p.loader.JavaHome()

	cmd := exec.CommandContext(ctx,
		filepath.Join(javaHome, "bin", "java"),
		"-jar",
		p.loader.JarPath(),
		strconv.Itoa(pid), // use the host PID
		filepath.Join(dir, "qtap.jar"),
		dir,
	)
	cmd.Env = []string{
		"QPOINT_STRATEGY=ignore", // prevent infinite loop
		"JAVA_HOME=" + javaHome,
		"CLASSPATH=" + filepath.Join(javaHome, "lib", "tools.jar"),
		fmt.Sprintf("LD_LIBRARY_PATH=%s:%s:%s",
			filepath.Join(javaHome, "lib"),
			filepath.Join(javaHome, "lib", "server"),
			filepath.Join(javaHome, "lib", "jli")),
		"PATH=" + filepath.Join(javaHome, "bin"),
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		// log the command results to stdout and stderr
		p.logger.Error("javassl agent injection failed",
			zap.Stringer("command", cmd),
			zap.Strings("env", cmd.Env),
			zap.ByteString("output", out),
			zap.Error(err))

		return fmt.Errorf("injecting agent: %w", err)
	}

	return nil
}

type libQtapSymbols struct {
	symbols []elf.Symbol
	mu      sync.Mutex
}

// Get looks up the symbols for libqtap.so and caches them for future calls.
// It requires a path to any libqtap.so file for the initial scan.
func (s *libQtapSymbols) Get(ctx context.Context, path string) ([]elf.Symbol, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// return cached symbols
	if s.symbols != nil {
		return s.symbols, nil
	}

	// searched symbols to use
	var syms []elf.Symbol

	// open the ELF file if we don't have one
	ef, err := binutils.NewElf(ctx, path, "/", false)
	if err != nil {
		return nil, fmt.Errorf("opening libqtap.so: %w", err)
	}
	defer ef.Close()

	syms, err = ef.SearchSymbols(ctx, libQtapSymbolSearch, elf.SHT_SYMTAB, elf.SHT_DYNSYM)
	if err != nil && !errors.Is(err, binutils.ErrNoSymbols) {
		return nil, fmt.Errorf("searching symbols: %w", err)
	}

	syms = ef.CalculateUprobeAddresses(ctx, syms)

	s.symbols = syms

	return syms, nil
}
