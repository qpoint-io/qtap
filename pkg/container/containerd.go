package container

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	apievents "github.com/containerd/containerd/api/events"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/events"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/typeurl/v2"
	"go.uber.org/zap"
)

const (
	DefaultContainerdSocketPath = "/run/containerd/containerd.sock"
	defaultNamespace            = "default"
)

var containerNameLabels = []string{
	"nerdctl/name",
	"io.kubernetes.container.name",
}

type Containerd struct {
	logger   *zap.Logger
	mu       sync.RWMutex
	endpoint string
	client   *containerd.Client
	cache    map[string]*Container // TODO: convert to our own map
}

func NewContainerdAccessor(logger *zap.Logger, endpoint string) (*Containerd, error) {
	if endpoint == "" {
		endpoint = DefaultContainerdSocketPath
	}

	// create global client
	opts := []containerd.Opt{containerd.WithTimeout(DefaultRuntimeTimeout)}
	c, err := containerd.New(endpoint, opts...)
	if err != nil {
		return nil, err
	}

	// test connection
	ctx, cancel := context.WithTimeout(context.TODO(), DefaultStartupTimeout)
	defer cancel()
	if _, err := c.Server(ctx); err != nil {
		return nil, err
	}

	return &Containerd{
		logger:   logger,
		endpoint: endpoint,
		client:   c,
		cache:    make(map[string]*Container),
	}, nil
}

// loadContainers loads all containers from the containerd client.
// This expects the context to be namespaced.
func (c *Containerd) loadContainers(ctx context.Context) error {
	containers, err := c.client.Containers(ctx)
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}

	for _, container := range containers {
		cr, err := c.buildContainerRecord(ctx, container)
		if err != nil {
			return fmt.Errorf("build container record: %w", err)
		}

		c.addContainer(cr)
	}

	return nil
}

// loadNamespacedContainers loads all containers from all namespaces.
func (c *Containerd) loadNamespacedContainers(ctx context.Context) error {
	if c.client == nil {
		return errors.New("client not initialized")
	}

	nsList, err := c.client.NamespaceService().List(ctx)
	if err != nil {
		return fmt.Errorf("list namespaces: %w", err)
	}

	for _, ns := range nsList {
		if ns == defaultNamespace {
			continue
		}

		if err := c.loadContainers(namespaces.WithNamespace(ctx, ns)); err != nil {
			return fmt.Errorf("loading namespace: %w", err)
		}
	}

	return nil
}

func (c *Containerd) Start(ctx context.Context) error {
	if err := c.loadNamespacedContainers(ctx); err != nil {
		return fmt.Errorf("load namespaced containers: %w", err)
	}

	go func() {
		c.watchContainerEventsWithRetry(ctx)
	}()
	return nil
}

func (c *Containerd) GetByID(containerID string) *Container {
	if containerID == "" {
		return nil
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	containerID = humanContainerID(containerID)

	return c.cache[containerID]
}

// processContainerCreateEvent loads a container from the containerd client and builds a qpoint container record.
// It then processes the container record and updates the cache.
func (c *Containerd) processContainerCreateEvent(ctx context.Context, id string) error {
	ctr, err := c.client.LoadContainer(ctx, id)
	if err != nil {
		return fmt.Errorf("load container: %w", err)
	}

	cr, err := c.buildContainerRecord(ctx, ctr)
	if err != nil {
		return fmt.Errorf("build container record: %w", err)
	}

	c.addContainer(cr)

	return nil
}

func (c *Containerd) processContainerDelete(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	humanID := humanContainerID(id)
	if cr := c.cache[humanID]; cr != nil {
		reportContainerStopped(cr, "containerd")
	}
	delete(c.cache, humanID)
}

// addContainer takes a qpoint container and updates the cache record to match using the human
// friendly container id (first 12 characters of the container id).
func (c *Containerd) addContainer(cr *Container) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Debug("discovered containerd container", zap.Any("container", cr), zap.Bool("is_sandbox", cr.isSandbox()))

	if cr.isSandbox() {
		return
	}

	humanID := humanContainerID(cr.ID)
	existing := c.cache[humanID]

	// If this is a new container, set start time and report metrics
	if existing == nil {
		cr.SetStartTime(time.Now())
		reportContainerStarted(cr, "containerd")
	}

	c.cache[humanID] = cr
}

// buildContainerRecord builds a qpoint container record from a containerd container.
func (c *Containerd) buildContainerRecord(ctx context.Context, container containerd.Container) (*Container, error) {
	info, err := container.Info(ctx)
	if err != nil {
		return nil, err
	}
	task, err := container.Task(ctx, nil)
	if err != nil {
		c.logger.Info("get task failed", zap.Error(err))
	}

	name := getContainerName(info.Labels)
	cr := &Container{
		ID:     container.ID(),
		Name:   name,
		Image:  info.Image,
		Labels: info.Labels,
	}

	if task != nil {
		cr.RootPID = int(task.Pid())
	}

	// set the rootfs path to the containerd runtime mount path
	ns, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		c.logger.Info("failed to get namespace from context", zap.Error(err))
	} else {
		cr.RootFS = fmt.Sprintf("/run/containerd/io.containerd.runtime.v2.task/%s/%s/rootfs",
			ns, container.ID())
	}

	return cr, nil
}

func (c *Containerd) watchContainerEventsWithRetry(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.watchContainerEvents(ctx)

		time.Sleep(DefaultWatchInterval)
	}
}

func (c *Containerd) watchContainerEvents(ctx context.Context) {
	cl := c.client

	var chMsg <-chan *events.Envelope
	var chErr <-chan error
	var msg *events.Envelope

	chMsg, chErr = cl.Subscribe(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-chErr:
			if errors.Is(err, context.Canceled) {
				return
			}
			c.logger.Error("container event subscription error", zap.Error(err))
			return
		case msg = <-chMsg:
		}

		ns := msg.Namespace
		if ns == "" {
			ns = defaultNamespace
		}

		event, err := typeurl.UnmarshalAny(msg.Event)
		if err != nil {
			continue
		}

		switch ev := event.(type) {
		case *apievents.ContainerCreate:
			c.logger.Debug("container created", zap.String("container", ev.GetID()))
			err = c.processContainerCreateEvent(namespaces.WithNamespace(ctx, ns), ev.GetID())
		case *apievents.ContainerDelete:
			c.logger.Debug("container deleted", zap.String("container", ev.GetID()))
			c.processContainerDelete(ev.GetID())
		case *apievents.TaskCreate:
			c.logger.Debug("task created", zap.String("container", ev.ContainerID))
			err = c.processContainerCreateEvent(namespaces.WithNamespace(ctx, ns), ev.ContainerID)
		case *apievents.TaskStart:
			c.logger.Debug("task started", zap.String("container", ev.ContainerID))
			err = c.processContainerCreateEvent(namespaces.WithNamespace(ctx, ns), ev.ContainerID)
		case *apievents.TaskDelete:
			c.logger.Debug("task deleted", zap.String("container", ev.ContainerID))
			c.processContainerDelete(ev.ContainerID)
		case *apievents.NamespaceCreate:
			c.logger.Debug("namespace created", zap.String("namespace", ev.GetName()))
		case *apievents.NamespaceDelete:
			c.logger.Debug("namespace deleted", zap.String("namespace", ev.GetName()))
		}

		if err != nil {
			c.logger.Error("error processing event", zap.Error(err))
		}
	}
}

func getContainerName(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	for _, key := range containerNameLabels {
		v := labels[key]
		if v != "" {
			return v
		}
	}
	return ""
}
