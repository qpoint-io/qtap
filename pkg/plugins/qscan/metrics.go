package qscan

import "github.com/qpoint-io/qtap/pkg/telemetry/metrics"

// Static buckets for qscan metrics
var (
	// Size buckets in bytes - exponential scale covering typical document sizes
	sizeBuckets = []float64{
		1, 10, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 25000, 50000, 100000, 250000, 500000, 1000000, 2500000, 5000000, 10000000, 50000000,
	}
)

var (
	// Sampling metrics
	scansAttemptedTotal  = metrics.NewSystemCounter("scans_attempted_total", "The total number of requests processed by the qscan plugin")
	scansSampledTotal    = metrics.NewSystemCounterVec("scans_sampled_total", "The total number of requests that passed sampling", []string{"reason"})
	scansNotSampledTotal = metrics.NewSystemCounter("scans_not_sampled_total", "The total number of requests that didn't pass sampling")

	// Storage metrics
	scansStoredTotal = metrics.NewSystemCounterVec("scans_stored_total", "The total number of documents stored", []string{"status"})
	scansStoredSize  = metrics.NewSystemHistogram("scans_stored_size_bytes", "The size of stored documents in bytes", sizeBuckets...)

	// Cache metrics
	cacheOperationsTotal = metrics.NewSystemCounterVec("cache_operations_total", "The total number of cache operations", []string{"operation"})
)
