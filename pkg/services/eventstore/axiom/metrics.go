package axiom

import "github.com/qpoint-io/qtap/pkg/telemetry/metrics"

var (
	submittedRecords = metrics.NewCounter("records_submitted", "The number of records submitted to Axiom")

	ingestedRecords = metrics.NewCounter("records_ingested", "The number of records successfully ingested by Axiom")

	failedRecords = metrics.NewCounter("records_failed", "The number of records that failed to be ingested by Axiom")
)

func incrementSubmittedRecords() {
	submittedRecords.Inc()
}

func incrementIngestedRecords(count float64) {
	ingestedRecords.Add(count)
}

func incrementFailedRecords(count float64) {
	failedRecords.Add(count)
}
