package coordinator

import (
	"context"

	"github.com/qpoint-io/qtap/pkg/broker"
	"github.com/qpoint-io/qtap/pkg/coordinator/core"
	"github.com/qpoint-io/qtap/pkg/coordinator/plugins"
	"github.com/qpoint-io/qtap/pkg/log"
	"github.com/qpoint-io/qtap/pkg/synq"
	"go.uber.org/zap"
)

type Coordinator struct {
	Core    *broker.Broker
	Plugins *broker.Broker

	processes  *synq.Map[int, *Process]
	containers *synq.Map[string, *Container]

	ctx    context.Context
	cancel context.CancelFunc
	logger *zap.Logger
}

type CoordinatorOpt func(*Coordinator)

func WithLogger(logger *zap.Logger) CoordinatorOpt {
	return func(c *Coordinator) {
		c.logger = logger
	}
}

func NewCoordinator(ctx context.Context, opts ...CoordinatorOpt) *Coordinator {
	ctx, cancel := context.WithCancel(ctx)
	c := &Coordinator{
		Core:       broker.NewBroker(),
		Plugins:    broker.NewBroker(),
		logger:     zap.L(),
		containers: synq.NewMap[string, *Container](),
		processes:  synq.NewMap[int, *Process](),
		ctx:        ctx,
		cancel:     cancel,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Coordinator) Start() error {
	events, err := c.Core.Subscribe(c.ctx, "tap", nil)
	if err != nil {
		return err
	}

	go func() {
		for {
			select {
			case <-c.ctx.Done():
				return
			case event := <-events:
				go func() {
					if err := c.handleEvent(event); err != nil {
						c.logger.Error("error handling event", zap.Error(err))
					}
				}()
			}
		}
	}()
	return nil
}

func (c *Coordinator) Stop() error {
	c.cancel()
	c.Core.Stop()
	c.Plugins.Stop()
	return nil
}

func (c *Coordinator) handleEvent(msg *broker.EventMessage) error {
	switch ev := msg.Data.(type) {
	case *core.ProcessStarted:
		return c.handleProcessStarted(ev)
	case *core.ProcessStopped:
		return c.handleProcessStopped(ev)
	case *core.ContainerStarted:
		return c.handleContainerStarted(ev)
	case *core.ContainerStopped:
		return c.handleContainerStopped(ev)
	case *core.ConnectionOpened:
		return c.handleConnectionOpened(ev)
	case *core.ConnectionClosed:
		return c.handleConnectionClosed(ev)
	}

	c.logger.Warn("dropped event with no handler", zap.String("topic", msg.Topic))
	return nil
}

func (c *Coordinator) handleProcessStarted(event *core.ProcessStarted) error {
	c.logger.Log(log.QpointLevel, "process started",
		zap.Int("pid", event.PID),
		zap.String("exe", event.Exe),
		zap.String("container_id", event.ContainerID),
	)

	process := &Process{
		PID:         event.PID,
		Exe:         event.Exe,
		ContainerID: event.ContainerID,
	}
	c.processes.Store(event.PID, process)

	return nil
}

func (c *Coordinator) handleProcessStopped(event *core.ProcessStopped) error {
	c.logger.Log(log.QpointLevel, "process stopped", zap.Int("pid", event.PID))
	return nil
}

func (c *Coordinator) handleContainerStarted(event *core.ContainerStarted) error {
	c.logger.Log(log.QpointLevel, "container started",
		zap.String("id", event.ID),
		zap.String("name", event.Name),
		zap.String("image", event.Image),
	)

	c.containers.Store(event.ID, &Container{
		ID:    event.ID,
		Name:  event.Name,
		Image: event.Image,
	})
	return nil
}

func (c *Coordinator) handleContainerStopped(event *core.ContainerStopped) error {
	c.logger.Log(log.QpointLevel, "container stopped", zap.String("id", event.ID))

	c.containers.Delete(event.ID)
	return nil
}

func (c *Coordinator) handleConnectionOpened(event *core.ConnectionOpened) error {
	c.logger.Log(log.QpointLevel, "connection opened", zap.String("id", event.ID))

	ev := &plugins.ConnectionOpened{
		ID:              event.ID,
		SourceIP:        event.SourceIP,
		SourcePort:      event.SourcePort,
		DestinationIP:   event.DestinationIP,
		DestinationPort: event.DestinationPort,
	}
	if process, ok := c.processes.Load(event.PID); ok {
		ev.Process = &plugins.Process{
			PID: process.PID,
			Exe: process.Exe,
		}

		if process.ContainerID != "" {
			if container, ok := c.containers.Load(process.ContainerID); ok {
				ev.Container = &plugins.Container{
					ID:    container.ID,
					Name:  container.Name,
					Image: container.Image,
				}
			}
		}
	}
	c.Plugins.Broadcast(ev)
	return nil
}

func (c *Coordinator) handleConnectionClosed(event *core.ConnectionClosed) error {
	c.logger.Log(log.QpointLevel, "connection closed", zap.String("id", event.ID))

	// TODO: opened closed updated should all share the same structure
	ev := &plugins.ConnectionClosed{
		ID: event.ID,
	}
	c.Plugins.Broadcast(ev)
	return nil
}

type Container struct {
	ID    string
	Name  string
	Image string
}

type Process struct {
	PID         int
	Exe         string
	ContainerID string
}

// type atomic[T any] struct {
// 	v  T // resolvable.V[T]
// 	mu sync.Mutex
// }

// func (a *atomic[T]) Load() T {
// 	a.mu.Lock()
// 	defer a.mu.Unlock()
// 	return a.v
// }

// func (a *atomic[T]) Store(v T) {
// 	a.mu.Lock()
// 	defer a.mu.Unlock()
// 	a.v = v
// }
