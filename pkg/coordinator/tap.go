package coordinator

import (
	"context"
	"fmt"

	"github.com/qpoint-io/qtap/pkg/broker"
	"github.com/qpoint-io/qtap/pkg/coordinator/core"
	"github.com/qpoint-io/qtap/pkg/coordinator/plugins"
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

func (c *Coordinator) handleEvent(event *broker.Event) error {
	switch event.Topic {
	case core.ProcessStarted{}.Topic():
		return handleEvent(event, c.handleProcessStarted)
	case core.ProcessStopped{}.Topic():
		return handleEvent(event, c.handleProcessStopped)
	case core.ContainerStarted{}.Topic():
		return handleEvent(event, c.handleContainerStarted)
	case core.ContainerStopped{}.Topic():
		return handleEvent(event, c.handleContainerStopped)
	case core.ConnectionOpened{}.Topic():
		return handleEvent(event, c.handleConnectionOpened)
	case core.ConnectionClosed{}.Topic():
		return handleEvent(event, c.handleConnectionClosed)
	}

	c.logger.Warn("dropped event with no handler", zap.String("topic", event.Topic))
	return nil
}

func (c *Coordinator) handleProcessStarted(event *core.ProcessStarted) error {
	c.logger.Info("process started", zap.Int("pid", event.PID))

	process := &Process{
		PID: event.PID,
		Exe: event.Exe,
	}
	if event.ContainerID != "" && event.ContainerID != "root" {
		if container, ok := c.containers.Load(event.ContainerID); ok {
			process.Container = container
		} else {
			// we will backfill the container info once we get the container started event
			// TODO: resolvable
			process.Container = &Container{
				ID: event.ContainerID,
			}
		}
	}
	c.processes.Store(event.PID, process)

	pluginsEv := &plugins.ProcessStarted{
		Process: &plugins.Process{
			PID: event.PID,
			Exe: event.Exe,
		},
	}
	if process.Container != nil {
		pluginsEv.Process.Container = &plugins.Container{
			ID:    process.Container.ID,
			Name:  process.Container.Name,
			Image: process.Container.Image,
		}
	}
	c.Plugins.Broadcast(pluginsEv.Topic(), pluginsEv)
	return nil
}

func (c *Coordinator) handleProcessStopped(event *core.ProcessStopped) error {
	c.logger.Info("process stopped", zap.Int("pid", event.PID))
	return nil
}

func (c *Coordinator) handleContainerStarted(event *core.ContainerStarted) error {
	c.logger.Info("container started",
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
	c.logger.Info("container stopped", zap.String("id", event.ID))
	c.containers.Delete(event.ID)
	return nil
}

func (c *Coordinator) handleConnectionOpened(event *core.ConnectionOpened) error {
	c.logger.Info("connection opened", zap.String("id", event.ID))

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

		if process.Container != nil {
			if container, ok := c.containers.Load(process.Container.ID); ok {
				ev.Process.Container = &plugins.Container{
					ID:    container.ID,
					Name:  container.Name,
					Image: container.Image,
				}
			}
		}
	}
	c.Plugins.Broadcast(ev.Topic(), ev)
	return nil
}

func (c *Coordinator) handleConnectionClosed(event *core.ConnectionClosed) error {
	c.logger.Info("connection closed", zap.String("id", event.ID))

	ev := &plugins.ConnectionClosed{
		ID: event.ID,
	}
	c.Plugins.Broadcast(ev.Topic(), ev)
	return nil
}

func handleEvent[T any](event *broker.Event, fn func(event *T) error) error {
	ev, ok := event.Data.(*T)
	if !ok {
		return fmt.Errorf("invalid event data type: %T, expected %T", event.Data, new(*T))
	}
	return fn(ev)
}

type Container struct {
	ID    string
	Name  string
	Image string
}

type Process struct {
	PID       int
	Exe       string
	Container *Container // TODO: convert to resolvable.V[*Container]
	// actually keep separate, replace with just ContainerID here
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
