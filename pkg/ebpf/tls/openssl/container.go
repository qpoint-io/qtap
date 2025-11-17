package openssl

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/synq"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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

	// hasOpenSSL indicates if this container has OpenSSL libraries
	hasOpenSSL bool

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

func (c *Container) Init(ctx context.Context, p *process.Process) error {
	ctx, span := tracer.Start(context.TODO(), "Container.Init",
		trace.WithLinks(trace.LinkFromContext(ctx)),
		trace.WithNewRoot(),
	)
	span.SetAttributes(attribute.String("container_id", p.ContainerID))
	defer span.End()
	// acquire lock
	c.mu.Lock()
	defer c.mu.Unlock()

	// if we're already initialized, return
	if c.initialized {
		return nil
	}

	// find all of the libssl.o targets on the container
	libs, err := p.FindSharedLibrary(ctx, LibSSL)
	if err != nil {
		return fmt.Errorf("finding %s: %w", LibSSL, err)
	}

	// initialize targets for the libs
	for _, lib := range libs {
		err := telemetry.TraceFn(tracer, ctx, "AttachSharedTarget", func(ctx context.Context, span trace.Span) error {
			span.SetAttributes(attribute.String("lib", lib))
			// create name by stripping off the p.Root
			name := strings.TrimPrefix(lib, p.Root)

			// create a target
			target := NewOpenSSLTarget(c.logger, name, p.ContainerID, lib, nil, TargetTypeShared, c.probeFn(), nil)

			// start the target
			if err := target.Start(ctx); err != nil {
				return err
			}
			span.SetAttributes(attribute.String("target", name))

			// add the target to the container
			c.targets[lib] = target

			// mark that this container has OpenSSL
			c.hasOpenSSL = true

			// debug
			c.logger.Info("detected OpenSSL shared library",
				zap.String("path", name),
				zap.String("container_id", p.ContainerID),
			)

			// register that the process has been detected to contain openssl probe endpoints
			p.AddDetectedTLSProbeType("openssl")
			span.SetAttributes(attribute.String("openssl_detected", "true"))
			return nil
		})
		if err != nil {
			return fmt.Errorf("attaching shared target: %w", err)
		}
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

func (c *Container) HasOpenSSL() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hasOpenSSL
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
