package http

import "github.com/qpoint-io/qtap/pkg/telemetry/metrics"

// Static buckets for HTTP metrics
var (
	// Duration buckets in milliseconds - exponential scale with common HTTP timing ranges
	durationBuckets = []float64{
		0.1, 0.5, 1, 2, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000, 60000, 120000, 300000, 600000,
	}

	// Size buckets in bytes - exponential scale covering typical HTTP payload sizes
	sizeBuckets = []float64{
		1, 10, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 25000, 50000, 100000, 250000, 500000, 1000000, 2500000, 5000000, 10000000, 50000000,
	}
)

var (
	requestsTotal    = metrics.NewCounterVec("requests_total", "The total number of requests", []string{"method", "host", "status_code", "protocol"})
	requestsDuration = metrics.NewHistogramVec("requests_duration_ms", "The duration of requests in milliseconds", []string{"method", "host", "status_code", "protocol"}, durationBuckets...)
	requestsSize     = metrics.NewHistogramVec("requests_size_bytes", "The size of requests in bytes", []string{"method", "host", "status_code", "protocol"}, sizeBuckets...)

	responsesTotal    = metrics.NewCounterVec("responses_total", "The total number of responses", []string{"method", "host", "status_code", "protocol"})
	responsesDuration = metrics.NewHistogramVec("responses_duration_ms", "The duration of responses in milliseconds", []string{"method", "host", "status_code", "protocol"}, durationBuckets...)
	responsesSize     = metrics.NewHistogramVec("responses_size_bytes", "The size of responses in bytes", []string{"method", "host", "status_code", "protocol"}, sizeBuckets...)

	duration = metrics.NewHistogram("duration_ms", "The combined duration of requests and responses in milliseconds", durationBuckets...)
)
