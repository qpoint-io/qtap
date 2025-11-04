package process

import (
	"context"
	"time"

	"github.com/kamaln7/resolvable"
	"github.com/qpoint-io/qtap/pkg/container"
	"go.uber.org/zap"
)

type Container struct {
	ID          string            `json:"container_id,omitempty"`
	Name        string            `json:"container_name,omitempty"`
	Labels      map[string]string `json:"container_labels,omitempty"`
	Image       string            `json:"container_image,omitempty"`
	ImageDigest string            `json:"container_imageDigest,omitempty"`
	RootFS      string            `json:"-"`
}

func (c Container) Fields() []zap.Field {
	f := []zap.Field{
		// zap.String("containerId", c.ID),
		zap.String("containerName", c.Name),
		// zap.Any("containerLabels", c.Labels),
		zap.String("containerImage", c.Image),
		// zap.String("containerImageDigest", c.ImageDigest),
	}

	return f
}

func (c Container) ControlValues() map[string]any {
	id := c.ID
	if len(id) > 12 {
		id = id[:12]
	}

	v := map[string]any{
		"id":    id,
		"name":  c.Name,
		"image": c.Image,
	}

	if len(c.Labels) > 0 {
		l := make(map[string]any, len(c.Labels))
		for k, v := range c.Labels {
			l[k] = v
		}
		v["labels"] = l
	}

	return v
}

type Pod struct {
	Name        string            `json:"pod_name,omitempty"`
	Namespace   string            `json:"pod_namespace,omitempty"`
	Labels      map[string]string `json:"pod_labels,omitempty"`
	Annotations map[string]string `json:"pod_annotations,omitempty"`
}

func (p Pod) Fields() []zap.Field {
	return []zap.Field{
		zap.String("podName", p.Name),
		zap.String("podNamespace", p.Namespace),
		// zap.String("podUID", p.UID),
		// zap.Any("podLabels", p.Labels),
		// zap.Any("podAnnotations", p.Annotations),
	}
}

func (p Pod) ControlValues() map[string]any {
	v := map[string]any{
		"name":      p.Name,
		"namespace": p.Namespace,
	}

	if len(p.Labels) > 0 {
		l := make(map[string]any, len(p.Labels))
		for k, v := range p.Labels {
			l[k] = v
		}
		v["labels"] = l
	}

	if len(p.Annotations) > 0 {
		a := make(map[string]any, len(p.Annotations))
		for k, v := range p.Annotations {
			a[k] = v
		}
		v["annotations"] = a
	}

	return v
}

type ContainerEnricher struct {
	DefaultObserver
	containerManager *container.Manager
}

func NewContainerEnricher(containerManager *container.Manager) *ContainerEnricher {
	return &ContainerEnricher{
		containerManager: containerManager,
	}
}

func (e *ContainerEnricher) ProcessStarted(ctx context.Context, p *Process) error {
	// discover the container metadata if it exists
	if e.containerManager == nil || p.ContainerID == "" || p.ContainerID == "root" {
		return nil
	}

	// Set up Container resolvable
	p.Container = resolvable.New(func(_ context.Context) (*Container, error) {
		container := e.containerManager.GetByID(p.ContainerID)
		if container == nil {
			return nil, nil
		}
		return &Container{
			ID:          container.ID,
			Name:        container.TidyName(),
			Labels:      container.Labels,
			Image:       container.Image,
			ImageDigest: container.ImageDigest,
			RootFS:      container.RootFS,
		}, nil
	},
		resolvable.WithRetry(),
		resolvable.WithGraceful(),
	).WithContext(ctx)

	// Set up Pod resolvable
	p.Pod = resolvable.New(func(_ context.Context) (*Pod, error) {
		container := e.containerManager.GetByID(p.ContainerID)
		if container == nil {
			return nil, nil
		}
		pod := container.Pod()
		if pod == nil || pod.Name == "" {
			return nil, nil
		}
		return &Pod{
			Name:        pod.Name,
			Namespace:   pod.Namespace,
			Labels:      pod.Labels,
			Annotations: pod.Annotations,
		}, nil
	},
		resolvable.WithRetry(),
		resolvable.WithGraceful(),
		resolvable.WithCacheTTL(5*time.Second),
	).WithContext(ctx)

	return nil
}
