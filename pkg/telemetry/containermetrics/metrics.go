// Package containermetrics connects container discovery to qtap's product metrics.
package containermetrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/qpoint-io/qtap/pkg/container"
)

// New registers the container collectors once with reg and returns their handlers.
// Like qtap's other metric constructors, it panics on registration errors.
func New(reg prometheus.Registerer) container.Callbacks {
	factory := promauto.With(reg)
	labels := []string{"runtime", "image", "pod_name", "pod_namespace"}
	opened := factory.NewCounterVec(prometheus.CounterOpts{
		Namespace: "qtap", Subsystem: "container", Name: "open_total", Help: "Total containers opened",
	}, labels)
	closed := factory.NewCounterVec(prometheus.CounterOpts{
		Namespace: "qtap", Subsystem: "container", Name: "close_total", Help: "Total containers closed",
	}, labels)
	active := factory.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "qtap", Subsystem: "container", Name: "active_total", Help: "Currently active containers",
	}, labels)
	restarted := factory.NewCounterVec(prometheus.CounterOpts{
		Namespace: "qtap", Subsystem: "container", Name: "restart_total", Help: "Total container restarts",
	}, labels)
	duration := factory.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "qtap", Subsystem: "container", Name: "duration_seconds", Help: "Container lifetime duration in seconds",
		Buckets: []float64{1, 2, 5, 10, 30, 60, 120, 300, 600, 1800, 3600, 10800, 21600, 43200, 86400},
	}, labels)
	podCount := factory.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "qtap", Subsystem: "container", Name: "pod_container_count", Help: "Number of containers per pod",
	}, []string{"pod_name", "namespace"})

	return container.Callbacks{
		Started: func(c *container.Container, runtime string) {
			values := containerLabels(c, runtime)
			opened.WithLabelValues(values...).Inc()
			active.WithLabelValues(values...).Inc()
			if name := c.Labels[container.ContainerLabelKeyPodName]; name != "" {
				podCount.WithLabelValues(name, c.Labels[container.ContainerLabelKeyPodNamespace]).Inc()
			}
		},
		Stopped: func(c *container.Container, runtime string) {
			values := containerLabels(c, runtime)
			closed.WithLabelValues(values...).Inc()
			active.WithLabelValues(values...).Dec()
			if start := c.GetStartTime(); !start.IsZero() {
				duration.WithLabelValues(values...).Observe(time.Since(start).Seconds())
			}
			if name := c.Labels[container.ContainerLabelKeyPodName]; name != "" {
				podCount.WithLabelValues(name, c.Labels[container.ContainerLabelKeyPodNamespace]).Dec()
			}
		},
		Restarted: func(c *container.Container, runtime string) {
			restarted.WithLabelValues(containerLabels(c, runtime)...).Inc()
		},
	}
}

func containerLabels(c *container.Container, runtime string) []string {
	// Pod identity comes from runtime labels; avoid lazily mutating the cached Pod.
	values := []string{runtime, c.Image, c.Labels[container.ContainerLabelKeyPodName], c.Labels[container.ContainerLabelKeyPodNamespace]}
	for i := 1; i < len(values); i++ {
		if values[i] == "" {
			values[i] = "unknown"
		}
	}
	return values
}
