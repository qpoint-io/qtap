package container

import (
	"strings"
	"time"
)

type Container struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels"`

	RootPID     int    `json:"rootPid"`
	Image       string `json:"image"`
	ImageDigest string `json:"imageDigest"`
	RootFS      string `json:"-"`

	startTime time.Time
	p         *Pod
}

func (c Container) TidyName() string {
	return strings.TrimLeft(c.Name, "/")
}

// when a pod is created, the container runtime first creates a "sandbox" container
// that sets up the shared Linux namespaces (network, IPC, etc.) for the pod. Other
// containers in the pod then join these namespaces. This function helps identify
// these special sandbox containers by their labels.
func (c Container) isSandbox() bool {
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
	p.loadFromContainer(c)
	c.p = &p

	return &p
}

func (c *Container) setPod(p *Pod) {
	c.p = p
}

// SetStartTime sets the container start time
func (c *Container) SetStartTime(t time.Time) {
	c.startTime = t
}

// GetStartTime returns the container start time
func (c Container) GetStartTime() time.Time {
	return c.startTime
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
	startTime   time.Time
}

func (p *Pod) loadFromContainer(c *Container) {
	labels := c.Labels
	if len(labels) == 0 {
		return
	}
	p.Name = labels[ContainerLabelKeyPodName]
	p.Namespace = labels[ContainerLabelKeyPodNamespace]
	p.UID = labels[ContainerLabelKeyPodUID]
}

// SetStartTime sets the pod start time
func (p *Pod) SetStartTime(t time.Time) {
	p.startTime = t
}

// GetStartTime returns the pod start time
func (p Pod) GetStartTime() time.Time {
	return p.startTime
}
