package process

import "github.com/qpoint-io/qtap/pkg/telemetry/metrics"

var (
	processOpenTotal    = metrics.NewCounterVec("open_total", "Total processes opened", []string{"binary", "user"})
	processCloseTotal   = metrics.NewCounterVec("close_total", "Total processes closed", []string{"binary", "user"})
	processActiveTotal  = metrics.NewGaugeVec("active_total", "Currently active processes", []string{"binary", "user"})
	processRenamedTotal = metrics.NewCounterVec("renamed_total", "Total processes renamed", []string{"binary", "user"})
	processDuration     = metrics.NewHistogramVec("duration_seconds", "Process lifetime duration in seconds",
		[]string{"binary", "user"},
		1, 2, 5, 10, 30, 60, 120, 300, 600, 1800, 3600, 10800, 21600, 43200, 86400,
	)
)

// getProcessLabels returns the label values for the process metrics
func getProcessLabels(p *Process) []string {
	binary := p.Binary
	if binary == "" {
		binary = "unknown"
	}

	user := "unknown"
	if u, err := p.User(); err == nil && u != nil {
		user = u.Username
	}

	return []string{binary, user}
}
