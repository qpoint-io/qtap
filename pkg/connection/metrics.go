package connection

import (
	"github.com/qpoint-io/qtap/pkg/telemetry/metrics"
)

func registerConnectionMetrics(m *Manager) {
	metrics.NewGaugeFunc("active_total", "The total number of active connections",
		func() float64 {
			return float64(m.connections.Len())
		},
	)
}
