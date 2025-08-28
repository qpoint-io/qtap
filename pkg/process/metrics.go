package process

import "github.com/qpoint-io/qtap/pkg/telemetry/metrics"

var (
	// processAddTotal tracks the number of processes added
	processAddTotal = metrics.NewSystemCounter("added_total", "Total number of processes added")

	// processRemoveTotal tracks the number of processes removed
	processRemoveTotal = metrics.NewSystemCounter("removed_total", "Total number of processes removed")

	// processRenamedTotal tracks the number of processes renamed
	processRenamedTotal = metrics.NewSystemCounter("renamed_total", "Total number of processes renamed")
)

// trackActiveProcessCount tracks the number of active processes as an observable gauge
func trackActiveProcessCount(fn func() int) {
	metrics.NewSystemGaugeFunc("active_total", "Total number of active monitored processes",
		func() float64 {
			return float64(fn())
		},
	)
}

// IncrementProcessAdd increments the process add counter
func incrementProcessAdd() {
	processAddTotal.Inc()
}

// IncrementProcessRemove increments the process remove counter
func incrementProcessRemove() {
	processRemoveTotal.Inc()
}

// IncrementProcessRenamed increments the process renamed counter
func incrementProcessRenamed() {
	processRenamedTotal.Inc()
}
