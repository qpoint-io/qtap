package containermetrics_test

import (
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/qpoint-io/qtap/pkg/container"
	"github.com/qpoint-io/qtap/pkg/telemetry/containermetrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetrics(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	callbacks := containermetrics.New(reg)
	c := &container.Container{Image: "nginx", Labels: map[string]string{
		container.ContainerLabelKeyPodName:      "web",
		container.ContainerLabelKeyPodNamespace: "apps",
	}}
	c.SetStartTime(time.Now().Add(-2 * time.Second))
	before := *c
	callbacks.Started(c, "docker")
	afterStart, err := reg.Gather()
	require.NoError(t, err)
	for _, family := range afterStart {
		if family.GetName() == "qtap_container_active_total" || family.GetName() == "qtap_container_pod_container_count" {
			require.Len(t, family.Metric, 1)
			assert.Equal(t, 1.0, family.Metric[0].GetGauge().GetValue())
		}
	}
	callbacks.Restarted(c, "docker")
	callbacks.Stopped(c, "docker")
	assert.Equal(t, before, *c, "reporting must not mutate the container")

	// A second live container has no image or pod identity and no start time.
	unknown := &container.Container{}
	callbacks.Started(unknown, "containerd")
	callbacks.Stopped(unknown, "containerd")
	callbacks.Started(unknown, "containerd")
	want := map[string]struct {
		help   string
		kind   string
		values map[string]float64
	}{
		"qtap_container_open_total":          {"Total containers opened", "COUNTER", map[string]float64{"docker": 1, "containerd": 2}},
		"qtap_container_close_total":         {"Total containers closed", "COUNTER", map[string]float64{"docker": 1, "containerd": 1}},
		"qtap_container_active_total":        {"Currently active containers", "GAUGE", map[string]float64{"docker": 0, "containerd": 1}},
		"qtap_container_restart_total":       {"Total container restarts", "COUNTER", map[string]float64{"docker": 1}},
		"qtap_container_pod_container_count": {"Number of containers per pod", "GAUGE", map[string]float64{"": 0}},
		"qtap_container_duration_seconds":    {"Container lifetime duration in seconds", "HISTOGRAM", map[string]float64{"docker": 1}},
	}
	families, err := reg.Gather()
	require.NoError(t, err)
	require.Len(t, families, len(want))
	for _, family := range families {
		t.Run(family.GetName(), func(t *testing.T) {
			expected, ok := want[family.GetName()]
			require.True(t, ok, "unexpected metric family")
			assert.Equal(t, expected.help, family.GetHelp())
			assert.Equal(t, expected.kind, family.GetType().String())
			require.Len(t, family.Metric, len(expected.values))
			values := make(map[string]float64)
			for _, metric := range family.Metric {
				labels := make(map[string]string)
				for _, label := range metric.Label {
					labels[label.GetName()] = label.GetValue()
				}
				runtime := labels["runtime"]
				switch runtime {
				case "docker":
					assert.Equal(t, map[string]string{"runtime": "docker", "image": "nginx", "pod_name": "web", "pod_namespace": "apps"}, labels)
				case "containerd":
					assert.Equal(t, map[string]string{"runtime": "containerd", "image": "unknown", "pod_name": "unknown", "pod_namespace": "unknown"}, labels)
				default:
					assert.Equal(t, map[string]string{"pod_name": "web", "namespace": "apps"}, labels)
				}
				switch expected.kind {
				case "COUNTER":
					values[runtime] = metric.GetCounter().GetValue()
				case "GAUGE":
					values[runtime] = metric.GetGauge().GetValue()
				case "HISTOGRAM":
					histogram := metric.GetHistogram()
					values[runtime] = float64(histogram.GetSampleCount())
					assert.GreaterOrEqual(t, histogram.GetSampleSum(), 2.0)
					var buckets []float64
					for _, bucket := range histogram.Bucket {
						buckets = append(buckets, bucket.GetUpperBound())
					}
					assert.Equal(t, []float64{1, 2, 5, 10, 30, 60, 120, 300, 600, 1800, 3600, 10800, 21600, 43200, 86400}, buckets)
				}
			}
			assert.Equal(t, expected.values, values)
		})
	}
}

func TestConcurrentCallbacks(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	callbacks := containermetrics.New(reg)
	c := &container.Container{Labels: map[string]string{container.ContainerLabelKeyPodName: "web"}}
	var wg sync.WaitGroup
	for _, runtime := range []string{"docker", "containerd"} {
		wg.Go(func() {
			callbacks.Started(c, runtime)
			callbacks.Stopped(c, runtime)
		})
	}
	wg.Wait()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() == "qtap_container_pod_container_count" {
			require.Len(t, family.Metric, 1)
			metric := family.Metric[0]
			assert.Zero(t, metric.GetGauge().GetValue())
			assert.Equal(t, "namespace", metric.Label[0].GetName())
			assert.Empty(t, metric.Label[0].GetValue())
			return
		}
	}
	t.Fatal("missing pod container count")
}
