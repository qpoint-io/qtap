package process

import "github.com/qpoint-io/qtap/pkg/telemetry/metrics"

var (
	processAddTotal     = metrics.NewCounter("added_total", "Total number of processes added")
	processRemoveTotal  = metrics.NewCounter("removed_total", "Total number of processes removed")
	processRenamedTotal = metrics.NewCounter("renamed_total", "Total number of processes renamed")
)

// trackActiveProcessCount tracks the number of active processes as an observable gauge
func trackActiveProcessCount(fn func() int) {
	metrics.NewGaugeFunc("active_total", "Total number of active monitored processes",
		func() float64 {
			return float64(fn())
		},
	)
}
