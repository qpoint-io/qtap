package tls

import (
	"context"
	"io"

	"github.com/cilium/ebpf/link"
	"github.com/hashicorp/go-multierror"
	"github.com/qpoint-io/qtap/pkg/binutils"
)

// Probe describes a TLS probe that can be attached to a binary such as OpenSSL.
//
// The lifecycle is: Symbol Search -> Scan -> Attach (if indicated by ScanResult)
type Probe interface {
	// Name returns the unique identifier for this probe (e.g., "openssl", "gotls")
	Name() string

	// Scan performs detailed analysis of the binary to gather all information needed for attaching probes.
	// Return a `nil` ScanResult if the probe does not detect anything.
	//
	// The returned ScanResult may be cached and reused across process restarts for future Attach calls.
	Scan(ctx context.Context, elf *binutils.Elf) (ScanResult, error)

	// Attach attaches probes to the process using the scan result.
	//
	// Returns a Closer that must be called when the process exits to clean up the probes.
	Attach(ctx context.Context, ex *link.Executable, result ScanResult) (io.Closer, error)

	// SharedLibraries returns the list of shared libraries this probe can attach to.
	// Each string is a library name prefix (e.g., "libssl.so", "libcrypto.so")
	SharedLibraries() []string

	// TODO: add ScanLibrary() and cache result
	// TODO should we just reuse Scan/Attach

	// AttachLibrary attaches probes to a shared library.
	AttachLibrary(ctx context.Context, library *SharedLibrary) (io.Closer, error)
}

// ScanResult contains the results of scanning a binary for TLS probe attachment points.
// Each probe defines its own concrete type that implements this interface.
// ScanResults are cached by binary and reused across process restarts.
type ScanResult interface {
	// ProbeType returns the probe name that produced this result (e.g., "openssl", "gotls")
	ProbeType() string

	// ProbeDetected returns true if the probe was detected in the binary.
	ProbeDetected() bool
}

// SharedLibrary represents a shared library that a probe can attach to.
type SharedLibrary struct {
	// Name corresponds to the library name prefix returned by SharedLibraries().
	Name string

	// Paths is a list of paths where this library was located.
	Paths []string
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
