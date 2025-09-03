package container

import (
	"time"

	"github.com/qpoint-io/qtap/pkg/telemetry/metrics"
)

var (
	// Container metrics
	containerOpenTotal    = metrics.NewCounterVec("open_total", "Total containers opened", []string{"runtime", "image", "pod_name", "pod_namespace"})
	containerCloseTotal   = metrics.NewCounterVec("close_total", "Total containers closed", []string{"runtime", "image", "pod_name", "pod_namespace"})
	containerActiveTotal  = metrics.NewGaugeVec("active_total", "Currently active containers", []string{"runtime", "image", "pod_name", "pod_namespace"})
	containerRestartTotal = metrics.NewCounterVec("restart_total", "Total container restarts", []string{"runtime", "image", "pod_name", "pod_namespace"})
	containerDuration     = metrics.NewHistogramVec("duration_seconds", "Container lifetime duration in seconds",
		[]string{"runtime", "image", "pod_name", "pod_namespace"},
		1, 2, 5, 10, 30, 60, 120, 300, 600, 1800, 3600, 10800, 21600, 43200, 86400,
	)

	// Pod metrics
	podContainerCount = metrics.NewGaugeVec("pod_container_count", "Number of containers per pod", []string{"pod_name", "namespace"})
)

// getContainerLabels returns the label values for container metrics
func getContainerLabels(c *Container, runtime string) []string {
	image := c.Image
	if image == "" {
		image = "unknown"
	}

	podName := "unknown"
	podNamespace := "unknown"
	if pod := c.Pod(); pod != nil {
		if pod.Name != "" {
			podName = pod.Name
		}
		if pod.Namespace != "" {
			podNamespace = pod.Namespace
		}
	}

	return []string{runtime, image, podName, podNamespace}
}

// reportContainerStarted reports metrics when a container starts
func reportContainerStarted(c *Container, runtime string) {
	labels := getContainerLabels(c, runtime)
	containerOpenTotal.WithLabelValues(labels...).Inc()
	containerActiveTotal.WithLabelValues(labels...).Inc()

	// Update pod container count
	if pod := c.Pod(); pod != nil && pod.Name != "" {
		podContainerCount.WithLabelValues(pod.Name, pod.Namespace).Inc()
	}
}

// reportContainerStopped reports metrics when a container stops
func reportContainerStopped(c *Container, runtime string) {
	labels := getContainerLabels(c, runtime)
	containerCloseTotal.WithLabelValues(labels...).Inc()
	containerActiveTotal.WithLabelValues(labels...).Dec()

	// Report duration if we have a start time
	if !c.startTime.IsZero() {
		containerDuration.WithLabelValues(labels...).Observe(time.Since(c.startTime).Seconds())
	}

	// Update pod container count
	if pod := c.Pod(); pod != nil && pod.Name != "" {
		podContainerCount.WithLabelValues(pod.Name, pod.Namespace).Dec()
	}
}

// reportContainerRestarted reports metrics when a container restarts
func reportContainerRestarted(c *Container, runtime string) {
	labels := getContainerLabels(c, runtime)
	containerRestartTotal.WithLabelValues(labels...).Inc()
}
