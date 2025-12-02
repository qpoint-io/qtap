package tls

import (
	"context"
	"io"

	"github.com/cilium/ebpf/link"
	"github.com/hashicorp/go-multierror"
	"github.com/qpoint-io/qtap/pkg/binutils"
)

// Probe describes a TLS probe that can be attached to a shared library or binary target.
type Probe interface {
	// Name returns the unique identifier for this probe (e.g., "openssl")
	Name() string

	// Scan performs detailed analysis of the binary to gather all information needed for attaching probes.
	// If the probe does not detect anything, it should return a ProbeScanResult with ProbeDetected() == false.
	//
	// The returned ProbeScanResult may be cached and reused across process restarts for future Attach calls.
	Scan(ctx context.Context, ef *binutils.Elf) (ProbeScanResult, error)

	// Attach attaches probes to the process using the scan result.
	//
	// Returns a Closer that must be called when the process exits to clean up the probes.
	Attach(ctx context.Context, target *ExeAttachable, result ProbeScanResult) (io.Closer, error)

	// SharedLibraries returns the list of shared libraries this probe can attach to.
	// Each string is a library name prefix (e.g., "libssl.so", "libcrypto.so")
	SharedLibraries() []string

	// TODO: add ScanLibrary() and cache result

	// AttachLibrary attaches probes to a shared library.
	AttachLibrary(ctx context.Context, library *SharedLibrary) (io.Closer, error)
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
	PID  int
	Path string
	Exe  *link.Executable
}

// DetacherCloser wraps a detacher to implement the io.Closer interface.
type DetacherCloser struct {
	Detacher interface {
		Detach() error
	}
}

func (d *DetacherCloser) Close() error {
	return d.Detacher.Detach()
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
