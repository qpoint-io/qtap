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
type CertInjectedCallback func(pid int, path string, rootID uint64)

// CertRemovedCallback is a function type for cert removal callbacks
type CertRemovedCallback func(pid int, path string, rootID uint64)

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

	defer func() {
		c.initialized = true
	}()

	// nothing to do unless we're using the ebpf strategy
	if c.strategy != InjectStrategyEbpf {
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

			return nil
		}

		// something else went wrong
		return fmt.Errorf("error testing write access to /tmp: %w", err)
	}

	// clean up test file
	os.Remove(testFile)

	return nil
}

func (c *Container) Scan(p *process.Process) (bool, error) {
	c.mu.Lock()
	if !c.initialized {
		if err := c.Init(p); err != nil {
			return false, fmt.Errorf("initializing container: %w", err)
		}
	}
	c.mu.Unlock()

	// add the process to the container's pids map
	c.pids.Store(p.Pid, nil)

	// if the file system is read-only, we can't scan for certs
	if c.readOnly {
		return false, nil
	}
	// scan for custom certs
	customCertsFound, err := c.scanCustomCerts(p)
	if err != nil {
		return false, fmt.Errorf("failed to scan for custom certs: %w", err)
	}

	return customCertsFound, nil
}

func (c *Container) Cleanup() error {
	// iterate over the certs and remove them all
	c.certs.Iter(func(key string, cert *Cert) bool {
		// remove the process from the cert
		if err := cert.RemoveAll(); err != nil {
			c.logger.Error("failed to remove cert", zap.Error(err))
		}

		// remove the cert
		c.certs.Delete(key)

		return true
	})

	return nil
}

func (c *Container) AddProcess(pid int) error {
	if _, exists := c.pids.LoadOrInsert(pid, nil); exists {
		return ErrPIDExists
	}

	return nil
}

func (c *Container) RemoveProcess(pid int) {
	// iterate over the certs and remove the process from each
	c.certs.Iter(func(key string, cert *Cert) bool {
		// remove the process from the cert
		if err := cert.Remove(pid); err != nil {
			c.logger.Error("failed to remove process from cert", zap.Error(err))
		}

		// notify any observers
		c.certRemoved(pid, cert.Location)

		// if the cert is empty, remove it
		if cert.IsEmpty() {
			c.certs.Delete(key)
		}

		return true
	})

	// remove the process from the container's pids map
	c.pids.Delete(pid)
}

func (c *Container) IsEmpty() bool {
	return c.pids.Len() == 0
}

// The certInjected method remains the same
func (c *Container) certInjected(pid int, path string) {
	if c.onCertInjected != nil {
		c.onCertInjected(pid, path, c.rootID)
	}
}

func (c *Container) certRemoved(pid int, path string) {
	if c.onCertRemoved != nil {
		c.onCertRemoved(pid, path, c.rootID)
	}
}
