// pkg/services/eventstore/otel/metrics.go
package otel

import "github.com/qpoint-io/qtap/pkg/telemetry"

var submittedRecords = telemetry.Counter(
	"tap_otel_records_submitted",
	telemetry.WithDescription("The number of records submitted to OpenTelemetry"))

func incrementSubmittedRecords() {
	submittedRecords(1)
}
