package container

import (
	"errors"
	"strings"
)

// Error definitions
var (
	ErrNoManager         = errors.New("no manager reference available for update")
	ErrContainerNotFound = errors.New("container not found")
	ErrPodNotFound       = errors.New("pod not found")
)

// ManagerInterface defines the interface for refreshing container and pod data
type ManagerInterface interface {
	GetByID(containerID string) *Container
	RefreshPodByNamespace(name, namespace string) (*Pod, error)
}

type Container struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels"`

	RootPID     int    `json:"rootPid"`
	Image       string `json:"image"`
	ImageDigest string `json:"imageDigest"`
	RootFS      string `json:"-"`

	p       *Pod
	manager ManagerInterface
}

func (c Container) TidyName() string {
	return strings.TrimLeft(c.Name, "/")
}

// when a pod is created, the container runtime first creates a "sandbox" container
// that sets up the shared Linux namespaces (network, IPC, etc.) for the pod. Other
// containers in the pod then join these namespaces. This function helps identify
// these special sandbox containers by their labels.
func (c Container) IsSandbox() bool {
	if len(c.Labels) == 0 {
		return false
	}

	return c.Labels["io.cri-containerd.kind"] == "sandbox" ||
		c.Labels["io.kubernetes.docker.type"] == "sandbox" ||
		c.Labels["io.kubernetes.docker.type"] == "podsandbox"
}

func (c *Container) Pod() *Pod {
	if c.p != nil {
		return c.p
	}

	var p Pod
	p.LoadFromContainer(c)
	c.p = &p

	return &p
}

func (c *Container) SetPod(p *Pod) {
	c.p = p
}

// SetManager sets the manager reference for this container
func (c *Container) SetManager(m ManagerInterface) {
	c.manager = m
}

// Update refreshes the container data from the underlying container runtime
func (c *Container) Update() error {
	if c.manager == nil {
		return ErrNoManager
	}

	fresh := c.manager.GetByID(c.ID)
	if fresh == nil {
		return ErrContainerNotFound
	}

	// Update all fields except the manager reference
	c.Name = fresh.Name
	c.Labels = fresh.Labels
	c.RootPID = fresh.RootPID
	c.Image = fresh.Image
	c.ImageDigest = fresh.ImageDigest
	c.RootFS = fresh.RootFS
	c.p = fresh.p

	// Ensure the updated pod also has the manager reference
	if c.p != nil {
		c.p.manager = c.manager
	}

	return nil
}

const (
	ContainerLabelKeyPodName      = "io.kubernetes.pod.name"
	ContainerLabelKeyPodNamespace = "io.kubernetes.pod.namespace"
	ContainerLabelKeyPodUID       = "io.kubernetes.pod.uid"
)

type Pod struct {
	Name        string
	Namespace   string
	UID         string
	Labels      map[string]string
	Annotations map[string]string

	manager ManagerInterface
}

func (p *Pod) LoadFromContainer(c *Container) {
	labels := c.Labels
	if len(labels) == 0 {
		return
	}
	p.Name = labels[ContainerLabelKeyPodName]
	p.Namespace = labels[ContainerLabelKeyPodNamespace]
	p.UID = labels[ContainerLabelKeyPodUID]
}

// SetManager sets the manager reference for this pod
func (p *Pod) SetManager(m ManagerInterface) {
	p.manager = m
}

// Update refreshes the pod data from the underlying Kubernetes runtime
func (p *Pod) Update() error {
	if p.manager == nil {
		return ErrNoManager
	}

	fresh, err := p.manager.RefreshPodByNamespace(p.Name, p.Namespace)
	if err != nil {
		return err
	}

	if fresh == nil {
		return ErrPodNotFound
	}

	// Update all fields except the manager reference
	p.UID = fresh.UID
	p.Labels = fresh.Labels
	p.Annotations = fresh.Annotations

	return nil
}
