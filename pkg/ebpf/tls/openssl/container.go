package openssl

import (
	"fmt"
	"strings"
	"sync"

	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/synq"
	"go.uber.org/zap"
)

const (
	LibSSL = "libssl.so"
)

type Container struct {
	// pids in the container
	pids *synq.Map[int, any]

	// openssl targets [/path/to/libssl.so]
	targets map[string]*OpenSSLTarget

	// probe creator function
	probeFn func() []*common.Uprobe

	// logger
	logger *zap.Logger

	// initialized
	initialized bool

	// mutex
	mu sync.Mutex
}

func NewContainer(logger *zap.Logger, probeFn func() []*common.Uprobe) *Container {
	return &Container{
		targets: make(map[string]*OpenSSLTarget),
		logger:  logger,
		probeFn: probeFn,
		pids:    synq.NewMap[int, any](),
	}
}

func (c *Container) Init(p *process.Process) error {
	// acquire lock
	c.mu.Lock()
	defer c.mu.Unlock()

	// if we're already initialized, return
	if c.initialized {
		return nil
	}

	// find all of the libssl.o targets on the container
	libs, err := p.FindSharedLibrary(LibSSL)
	if err != nil {
		return fmt.Errorf("finding %s: %w", LibSSL, err)
	}

	// initialize targets for the libs
	for _, lib := range libs {
		// create name by stripping off the p.Root
		name := strings.TrimPrefix(lib, p.Root)

		// create a target
		target := NewOpenSSLTarget(c.logger, name, p.ContainerID, lib, nil, TargetTypeShared, c.probeFn(), nil)

		// start the target
		if err := target.Start(); err != nil {
			return fmt.Errorf("starting openssl target: %w", err)
		}

		// add the target to the container
		c.targets[lib] = target

		// debug
		c.logger.Info("detected OpenSSL shared library",
			zap.String("path", name),
			zap.String("container_id", p.ContainerID),
		)
	}

	// set initialized
	c.initialized = true

	return nil
}

func (c *Container) AddProcess(pid int) {
	// ensure the pid exists
	c.pids.LoadOrInsert(pid, nil)
}

func (c *Container) RemoveProcess(pid int) {
	c.pids.Delete(pid)
}

func (c *Container) IsEmpty() bool {
	return c.pids.Len() == 0
}

func (c *Container) Cleanup() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// stop the targets
	for _, target := range c.targets {
		if err := target.Stop(); err != nil {
			return fmt.Errorf("stopping ssl target: %w", err)
		}
	}

	return nil
}
