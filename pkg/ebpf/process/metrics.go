package process

import "github.com/qpoint-io/qtap/pkg/telemetry"

var (
	// procEventReadTotal tracks the number of process events read from the ring buffer
	procEventReadTotal = telemetry.Counter("ebpf_proc_events_read",
		telemetry.WithDescription("Total number of process events read from ring buffer"))

	// procEventErrorTotal tracks the number of process event read errors
	procEventErrorTotal = telemetry.Counter("ebpf_proc_events_errors",
		telemetry.WithDescription("Total number of process event read errors"))
)

// trackCacheSize tracks the size of the process cache as an observable gauge
func trackCacheSize(fn func() int) {
	telemetry.ObservableGauge("ebpf_proc_cache_size",
		func() float64 {
			return float64(fn())
		},
		telemetry.WithDescription("Current size of the process cache"),
	)
}

// IncrementProcEventRead increments the process event read counter
func incrementProcEventRead() {
	procEventReadTotal(1)
}

// IncrementProcEventError increments the process event error counter
func incrementProcEventError() {
	procEventErrorTotal(1)
}
