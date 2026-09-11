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

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
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
	logger    *zap.Logger
	mu        sync.RWMutex
	client    *client.Client
	cache     map[string]*Container // TODO: convert to our own map
	callbacks Callbacks
}

func NewDockerAccessor(logger *zap.Logger, endpoint string, callbacks Callbacks) (*docker, error) {
	opts := []client.Opt{
		client.FromEnv,
	}

	// if a unix socket is provided, check if the endpoint exists and is a socket
	if after, ok := strings.CutPrefix(endpoint, "unix://"); ok {
		filepath := after
		info, err := os.Stat(filepath)
		if err != nil {
			return nil, fmt.Errorf("endpoint %s does not exist: %w", filepath, err)
		}
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("endpoint %s is not a socket", filepath)
		}
	}

	if endpoint != "" {
		opts = append(opts, client.WithHost(endpoint))
	} else if os.Getenv(DefaultDockerHostEnv) == "" {
		opts = append(opts, client.WithHost(DefaultDockerSocketPath))
	}

	c, err := client.New(opts...)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.TODO(), DefaultStartupTimeout)
	defer cancel()
	if _, err := c.Info(ctx, client.InfoOptions{}); err != nil {
		return nil, err
	}

	return &docker{
		logger:    logger,
		client:    c,
		cache:     make(map[string]*Container),
		callbacks: callbacks,
	}, nil
}

func (d *docker) Start(ctx context.Context) error {
	res, err := d.client.ContainerList(ctx, client.ContainerListOptions{
		Filters: make(client.Filters).Add("status", "running"),
	})
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}
	for _, cr := range res.Items {
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

	humanID := humanContainerID(cr.ID)
	existing := d.cache[humanID]

	// If this is a new container, set its discovery time.
	if existing == nil {
		cr.SetStartTime(time.Now())
	}

	d.cache[humanID] = cr
	d.mu.Unlock()

	if existing == nil && d.callbacks.Started != nil {
		d.callbacks.Started(cr, "docker")
	}
}

func (d *docker) handleContainerStop(_ context.Context, containerID string) {
	d.mu.Lock()

	humanID := humanContainerID(containerID)
	cr := d.cache[humanID]
	delete(d.cache, humanID)
	d.mu.Unlock()

	if cr != nil && d.callbacks.Stopped != nil {
		d.callbacks.Stopped(cr, "docker")
	}
}

func (d *docker) handleContainerRestart(_ context.Context, containerID string) {
	if cr := d.GetByID(containerID); cr != nil && d.callbacks.Restarted != nil {
		d.callbacks.Restarted(cr, "docker")
	}
}

func (d *docker) inspectContainer(ctx context.Context, containerID string) (*Container, error) {
	res, err := d.client.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("inspect container %s: %w", containerID, err)
	}

	return containerFromInspect(containerID, res.Container), nil
}

// containerFromInspect maps a docker inspect response onto our runtime-neutral
// Container. Every field it reads is optional on the wire, so each is guarded.
func containerFromInspect(containerID string, data containertypes.InspectResponse) *Container {
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
	// GraphDriver is a pointer and omitempty as of moby/moby/api, so nil is reachable
	if gd := data.GraphDriver; gd != nil && gd.Data != nil {
		if mergedDir, ok := gd.Data["MergedDir"]; ok {
			cr.RootFS = mergedDir
		}
	}

	return cr
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
	var msg events.Message

	res := d.client.Events(ctx, client.EventsListOptions{})

	for {
		select {
		case <-ctx.Done():
			return true
		case err := <-res.Err:
			if errors.Is(err, context.Canceled) {
				return true
			}
			d.logger.Error("docker event subscription error", zap.Error(err))
			return false
		case msg = <-res.Messages:
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
