package container

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

const (
	DefaultDockerHostEnv    = "DOCKER_HOST"
	DefaultDockerSocketPath = "unix:///var/run/docker.sock"
)

type Action string

const (
	ActionCreate  Action = "create"
	ActionStart   Action = "start"
	ActionRestart Action = "restart"
	ActionStop    Action = "stop"
	ActionDie     Action = "die"
	ActionDestroy Action = "destroy"

	ActionExecCreate Action = "exec_create"
	ActionExecStart  Action = "exec_start"
)

type docker struct {
	logger *zap.Logger
	mu     sync.RWMutex
	client *client.Client
	cache  map[string]*Container // TODO: convert to our own map
}

func NewDockerAccessor(logger *zap.Logger, endpoint string) (*docker, error) {
	opts := []client.Opt{
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	}

	// check if the endpoint exists and is a socket
	if _, err := os.Stat(endpoint); err != nil {
		return nil, fmt.Errorf("endpoint %s does not exist or is not a socket: %w", endpoint, err)
	}

	if endpoint != "" {
		opts = append(opts, client.WithHost(endpoint))
	} else if os.Getenv(DefaultDockerHostEnv) == "" {
		opts = append(opts, client.WithHost(DefaultDockerSocketPath))
	}

	c, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.TODO(), DefaultStartupTimeout)
	defer cancel()
	if _, err := c.Info(ctx); err != nil {
		return nil, err
	}

	return &docker{
		logger: logger,
		client: c,
		cache:  make(map[string]*Container),
	}, nil
}

func (d *docker) Start(ctx context.Context) error {
	containers, err := d.client.ContainerList(ctx, containertypes.ListOptions{
		Filters: filters.NewArgs(filters.Arg("status", "running")),
	})
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}
	for _, cr := range containers {
		d.handleContainerEvent(ctx, cr.ID)
	}

	go func() {
		d.watchContainerEventsWithRetry(ctx)
	}()

	return nil
}

func (d *docker) GetByID(containerID string) *Container {
	if containerID == "" {
		return nil
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	containerID = humanContainerID(containerID)

	return d.cache[containerID]
}

func (d *docker) Close() error {
	return d.client.Close()
}

func (d *docker) handleContainerEvent(ctx context.Context, containerID string) {
	cr, err := d.inspectContainer(ctx, containerID)
	if err != nil {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	humanID := humanContainerID(cr.ID)
	existing := d.cache[humanID]

	// If this is a new container, set start time and report metrics
	if existing == nil {
		cr.SetStartTime(time.Now())
		reportContainerStarted(cr, "docker")
	}

	d.cache[humanID] = cr
}

func (d *docker) handleContainerStop(_ context.Context, containerID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	humanID := humanContainerID(containerID)
	if cr := d.cache[humanID]; cr != nil {
		reportContainerStopped(cr, "docker")
		delete(d.cache, humanID)
	}
}

func (d *docker) handleContainerRestart(_ context.Context, containerID string) {
	d.mu.RLock()
	humanID := humanContainerID(containerID)
	if cr := d.cache[humanID]; cr != nil {
		reportContainerRestarted(cr, "docker")
	}
	d.mu.RUnlock()
}

func (d *docker) inspectContainer(ctx context.Context, containerID string) (*Container, error) {
	c := d.client

	data, err := c.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("inspect container %s: %w", containerID, err)
	}

	cr := &Container{
		ID:          containerID,
		Name:        data.Name,
		ImageDigest: data.Image,
	}
	if conf := data.Config; conf != nil {
		cr.Image = conf.Image
		cr.Labels = conf.Labels
	}
	if state := data.State; state != nil && state.Pid != 0 {
		cr.RootPID = state.Pid
	}

	// extract RootFS path from GraphDriver data
	if data.GraphDriver.Data != nil {
		if mergedDir, ok := data.GraphDriver.Data["MergedDir"]; ok {
			cr.RootFS = mergedDir
		}
	}

	return cr, nil
}

func (d *docker) watchContainerEventsWithRetry(ctx context.Context) {
	backoff := time.Second
	maxBackoff := time.Minute

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if d.watchContainerEvents(ctx) {
			backoff = time.Second
		} else {
			d.logger.Error("docker event subscription failed", zap.Duration("backoff", backoff))
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				if backoff < maxBackoff {
					backoff *= 2
					if backoff > maxBackoff {
						backoff = maxBackoff
					}
				}
			}
		}
	}
}

func (d *docker) watchContainerEvents(ctx context.Context) bool {
	c := d.client

	var chMsg <-chan events.Message
	var chErr <-chan error
	var msg events.Message

	chMsg, chErr = c.Events(ctx, events.ListOptions{})

	for {
		select {
		case <-ctx.Done():
			return true
		case err := <-chErr:
			if errors.Is(err, context.Canceled) {
				return true
			}
			d.logger.Error("docker event subscription error", zap.Error(err))
			return false
		case msg = <-chMsg:
		}

		if msg.Type != events.ContainerEventType {
			continue
		}
		switch Action(msg.Action) {
		case ActionStart:
			d.handleContainerEvent(ctx, msg.Actor.ID)
		case ActionRestart:
			d.handleContainerRestart(ctx, msg.Actor.ID)
			d.handleContainerEvent(ctx, msg.Actor.ID)
		case ActionStop, ActionDie, ActionDestroy:
			d.handleContainerStop(ctx, msg.Actor.ID)
		}

		if strings.HasPrefix(string(msg.Action), string(ActionExecCreate)+": ") ||
			strings.HasPrefix(string(msg.Action), string(ActionExecStart)+": ") {
			d.handleContainerEvent(ctx, msg.Actor.ID)
		}
	}
}
