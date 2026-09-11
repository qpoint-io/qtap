package container

import (
	"context"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	DefaultRuntimeTimeout  = time.Second * 2
	DefaultStartupTimeout  = time.Second * 5
	HumanContainerIDLength = 12
)

type Accessor interface {
	Start(ctx context.Context) error
	GetByID(containerId string) *Container
}

// Callbacks receives discovery reporting events, not an authoritative runtime
// lifecycle stream. Nil handlers are ignored. Handlers run synchronously after
// cache locks are released and must return promptly and treat records as read-only.
// Different runtime accessors may invoke handlers concurrently.
type Callbacks struct {
	Started   func(c *Container, runtime string)
	Stopped   func(c *Container, runtime string)
	Restarted func(c *Container, runtime string)
}

type Manager struct {
	logger *zap.Logger

	accessors []Accessor
	k8s       *KubernetesAccessor
}

func NewManager(logger *zap.Logger, dockerEndpoint, containerdEndpoint, criRuntimeEndpoint string, callbacks Callbacks) *Manager {
	ca := &Manager{logger: logger}

	logger = logger.With(zap.String("package", "container"))

	dockerEndpoint = formatContainerSocketEndpoint(dockerEndpoint)
	criRuntimeEndpoint = formatContainerSocketEndpoint(criRuntimeEndpoint)

	dr, err := NewDockerAccessor(logger, dockerEndpoint, callbacks)
	if err != nil {
		logger.Debug("skipping Docker Engine integration", zap.String("endpoint", dockerEndpoint), zap.String("message", err.Error()))
	} else {
		logger.Info("connected to docker engine", zap.Any("path", strings.TrimPrefix(dockerEndpoint, "unix://")))
		ca.accessors = append(ca.accessors, dr)
	}

	cd, err := NewContainerdAccessor(logger, containerdEndpoint, callbacks)
	if err != nil {
		logger.Debug("skipping containerd integration", zap.String("endpoint", containerdEndpoint), zap.String("message", err.Error()))
	} else {
		logger.Info("connected to containerd", zap.Any("path", containerdEndpoint))
		ca.accessors = append(ca.accessors, cd)
	}

	k8s, criEndpoint, errs := NewKubernetesAccessor(logger, criRuntimeEndpoint)
	if len(errs) > 0 {
		logger.Debug("skipping kubernetes integration", zap.Errors("messages", errs))
	} else {
		logger.Info("connected to kubernetes runtime service", zap.Any("path", strings.TrimPrefix(criEndpoint, "unix://")))
		ca.k8s = k8s
	}

	return ca
}

func (a *Manager) Start(ctx context.Context) error {
	for _, e := range a.accessors {
		if err := e.Start(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (a *Manager) GetByID(containerID string) *Container {
	var c *Container
	for _, e := range a.accessors {
		c = e.GetByID(containerID)
		if c != nil {
			break
		}
	}

	if c == nil {
		return nil
	}

	if c.ID != "" && a.k8s != nil {
		c = a.k8s.addPodToContainer(c)
	}

	return c
}

func formatContainerSocketEndpoint(raw string) string {
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http") {
		return raw
	}
	if strings.HasPrefix(raw, "unix://") {
		return raw
	}
	return "unix://" + raw
}

func humanContainerID(containerID string) string {
	if len(containerID) > HumanContainerIDLength {
		return containerID[:HumanContainerIDLength]
	}
	return containerID
}
