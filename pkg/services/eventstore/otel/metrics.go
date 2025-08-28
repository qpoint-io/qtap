// pkg/services/eventstore/otel/metrics.go
package otel

import "github.com/qpoint-io/qtap/pkg/telemetry/metrics"

var submittedRecords = metrics.NewCounter("records_submitted", "The number of records submitted to OpenTelemetry")

func incrementSubmittedRecords() {
	submittedRecords.Inc()
}
