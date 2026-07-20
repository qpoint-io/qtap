package ca

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/qpoint-io/qtap/internal/tap"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/synq"
	"go.uber.org/zap"
)

var ErrPIDExists = errors.New("pid already exists")

// CertInjectedCallback is a function type for cert injection callbacks
type CertInjectedCallback func(pid int, path string, rootID uint64) error

// CertRemovedCallback is a function type for cert removal callbacks
type CertRemovedCallback func(pid int, path string, rootID uint64) error

type Container struct {
	// ca bytes to inject
	caBytes []byte

	// container ID
	rootID uint64

	// first pid in the container
	pidOne int

	// inject strategy
	strategy InjectStrategy

	// certs
	certs *synq.Map[string, *Cert]

	// pids in the container
	pids *synq.Map[int, any]

	// logger
	logger *zap.Logger

	// bpf objects
	certObjs *tap.CertsObjects
	tapObjs  *tap.TapObjects

	// Callback function for cert injection
	onCertInjected CertInjectedCallback

	// Callback function for cert removal
	onCertRemoved CertRemovedCallback

	// read-only file system
	readOnly bool

	// mutex
	mu sync.Mutex

	// initialized state
	initialized bool
}

func NewContainer(
	rootID uint64,
	pidOne int,
	caBytes []byte,
	strategy InjectStrategy,
	logger *zap.Logger,
	certObjs *tap.CertsObjects,
	captureObjs *tap.TapObjects,
	onCertInjected CertInjectedCallback,
	onCertRemoved CertRemovedCallback,
) *Container {
	c := &Container{
		rootID:         rootID,
		pidOne:         pidOne,
		caBytes:        caBytes,
		strategy:       strategy,
		logger:         logger,
		certObjs:       certObjs,
		tapObjs:        captureObjs,
		certs:          synq.NewMap[string, *Cert](),
		pids:           synq.NewMap[int, any](),
		onCertInjected: onCertInjected,
		onCertRemoved:  onCertRemoved,
	}

	return c
}

func (c *Container) Init(p *process.Process) error {
	// nothing to do it we're already initialized
	if c.initialized {
		return nil
	}

	// nothing to do unless we're using the ebpf strategy
	if c.strategy != InjectStrategyEbpf {
		c.initialized = true
		return nil
	}

	// tmp dir
	tmpDir := filepath.Join("/proc", strconv.Itoa(p.Pid), "root", "tmp")

	// quickheck if /tmp exists and is writable
	testFile := filepath.Join(tmpDir, ".qtap_write_test")
	err := os.WriteFile(testFile, []byte("test"), 0644)
	if err != nil {
		// check if the error message contains "read-only file system"
		if os.IsPermission(err) || (err.Error() != "" && strings.Contains(strings.ToLower(err.Error()), "read-only file system")) {
			// set the container to read-only
			c.readOnly = true

			// get the hostname
			hostname, _ := p.Hostname()

			// log the warning
			c.logger.Warn("read-only file system, skipping cert injection",
				zap.String("hostname", hostname),
				zap.String("container_id", p.ContainerID),
				zap.Uint64("root_id", c.rootID),
				zap.String("pod_id", p.PodID),
			)

			c.initialized = true
			return nil
		}

		// something else went wrong
		return fmt.Errorf("error testing write access to /tmp: %w", err)
	}

	// clean up test file
	os.Remove(testFile)
	c.initialized = true

	return nil
}

func (c *Container) Scan(p *process.Process) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Track the live process before choosing a namespace path.
	c.pids.Store(p.Pid, nil)
	c.updateNamespacePID(-1)

	if !c.initialized {
		if err := c.Init(p); err != nil {
			return false, fmt.Errorf("initializing container: %w", err)
		}
	}

	// if the file system is read-only, we can't scan for certs
	if c.readOnly {
		return false, nil
	}
	// scan for custom certs
	customCertsFound, err := c.scanCustomCerts(p)
	if err != nil {
		cleanupErr := c.removeProcessLocked(p.Pid, false)
		return false, errors.Join(
			fmt.Errorf("failed to scan for custom certs: %w", err),
			cleanupErr,
		)
	}

	return customCertsFound, nil
}

func (c *Container) Cleanup() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updateNamespacePID(-1)

	var errs []error

	// iterate over the certs and remove them all
	c.certs.Iter(func(key string, cert *Cert) bool {
		// remove the process from the cert
		if err := cert.RemoveAll(); err != nil {
			errs = append(errs, fmt.Errorf("cleaning certificate %s: %w", key, err))
			return true
		}

		// remove the cert
		c.certs.Delete(key)

		return true
	})

	return errors.Join(errs...)
}

func (c *Container) AddProcess(pid int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.pids.LoadOrInsert(pid, nil); exists {
		return ErrPIDExists
	}

	return nil
}

func (c *Container) RemoveProcess(pid int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.removeProcessLocked(pid, false)
}

func (c *Container) RemoveProcessPreservingMap(pid int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.removeProcessLocked(pid, true)
}

func (c *Container) removeProcessLocked(pid int, preserveMap bool) error {
	c.updateNamespacePID(pid)

	var errs []error

	// iterate over the certs and remove the process from each
	c.certs.Iter(func(key string, cert *Cert) bool {
		// Revoke TLS termination before changing routing or trust material.
		if err := c.certRemoved(pid, cert.Location); err != nil {
			errs = append(errs, fmt.Errorf("revoking TLS termination for process %d: %w", pid, err))
			return false
		}

		var err error
		if preserveMap {
			err = cert.RemovePreservingMap(pid)
		} else {
			err = cert.Remove(pid)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("removing process %d from certificate %s: %w", pid, key, err))
			return true
		}

		// if the cert is empty, remove it
		if cert.IsEmpty() {
			c.certs.Delete(key)
		}

		return true
	})

	if len(errs) == 0 {
		// remove the process from the container's pids map only after all
		// certificate cleanup succeeds so the operation remains retryable.
		c.pids.Delete(pid)
	}

	return errors.Join(errs...)
}

func (c *Container) IsEmpty() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pids.Len() == 0
}

func (c *Container) NotifyIfCertificateActive(pid int, path string, notify func() error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cert, exists := c.certs.Load(path)
	if !exists || !cert.HasProcess(pid) {
		return nil
	}
	return notify()
}

func (c *Container) updateNamespacePID(exclude int) {
	currentRoot := filepath.Join("/proc", strconv.Itoa(c.pidOne), "root")
	if c.pidOne != exclude {
		if _, err := os.Stat(currentRoot); err == nil {
			return
		}
	}

	replacement := 0
	c.pids.Iter(func(pid int, _ any) bool {
		if pid == exclude {
			return true
		}
		if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid), "root")); err == nil {
			replacement = pid
			return false
		}
		return true
	})
	if replacement == 0 {
		return
	}

	c.pidOne = replacement
	c.certs.Iter(func(_ string, cert *Cert) bool {
		cert.SetNamespacePID(replacement)
		return true
	})
}

// The certInjected method remains the same
func (c *Container) certInjected(pid int, path string) error {
	if c.onCertInjected != nil {
		return c.onCertInjected(pid, path, c.rootID)
	}
	return nil
}

func (c *Container) certRemoved(pid int, path string) error {
	if c.onCertRemoved != nil {
		return c.onCertRemoved(pid, path, c.rootID)
	}
	return nil
}
