package container

import (
	"context"
	"strings"
	"time"

	"github.com/qpoint-io/qtap/pkg/process"
	"go.uber.org/zap"
)

const (
	DefaultRuntimeTimeout  = time.Second * 2
	DefaultStartupTimeout  = time.Second * 5
	DefaultWatchInterval   = time.Second * 15
	HumanContainerIDLength = 12
)

type Accessor interface {
	Start(ctx context.Context) error
	GetByID(containerId string) *Container
}

type Manager struct {
	process.DefaultObserver

	logger *zap.Logger

	accessors []Accessor
	k8s       *KubernetesAccessor
}

func NewManager(logger *zap.Logger, dockerEndpoint, containerdEndpoint, criRuntimeEndpoint string) *Manager {
	ca := &Manager{logger: logger}

	logger = logger.With(zap.String("package", "container"))

	dockerEndpoint = formatContainerSocketEndpoint(dockerEndpoint)
	criRuntimeEndpoint = formatContainerSocketEndpoint(criRuntimeEndpoint)

	dr, err := NewDockerAccessor(logger, dockerEndpoint)
	if err != nil {
		logger.Debug("skipping Docker Engine integration", zap.String("endpoint", dockerEndpoint), zap.String("message", err.Error()))
	} else {
		logger.Info("connected to docker engine", zap.Any("path", strings.TrimPrefix(dockerEndpoint, "unix://")))
		ca.accessors = append(ca.accessors, dr)
	}

	cd, err := NewContainerdAccessor(logger, containerdEndpoint)
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
		c = a.k8s.AddPodToContainer(c)
	}

	return c
}

func (m *Manager) ProcessStarted(p *process.Process) error {
	// discover the container metadata if it exists
	if p.ContainerID != "" && p.ContainerID != "root" {
		container := m.GetByID(p.ContainerID)
		if container != nil {
			p.Container = &process.Container{
				ID:          container.ID,
				Name:        container.TidyName(),
				Labels:      container.Labels,
				Image:       container.Image,
				ImageDigest: container.ImageDigest,
				RootFS:      container.RootFS,
			}

			if pod := container.Pod(); pod != nil {
				p.Pod = &process.Pod{
					Name:        pod.Name,
					Namespace:   pod.Namespace,
					Labels:      pod.Labels,
					Annotations: pod.Annotations,
				}
			}
		}
	}

	return nil
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
