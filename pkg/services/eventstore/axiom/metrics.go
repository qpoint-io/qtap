package axiom

import "github.com/qpoint-io/qtap/pkg/telemetry"

var (
	submittedRecords = telemetry.Counter(
		"tap_axiom_records_submitted",
		telemetry.WithDescription("The number of records submitted to Axiom"))

	ingestedRecords = telemetry.Counter(
		"tap_axiom_records_ingested",
		telemetry.WithDescription("The number of records successfully ingested by Axiom"))

	failedRecords = telemetry.Counter(
		"tap_axiom_records_failed",
		telemetry.WithDescription("The number of records that failed to be ingested by Axiom"))
)

func incrementSubmittedRecords() {
	submittedRecords(1)
}

func incrementIngestedRecords(count float64) {
	ingestedRecords(count)
}

func incrementFailedRecords(count float64) {
	failedRecords(count)
}
