package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const systemNamespace = "qtap_system"

// System metric constructors - these use the system registry and namespace

func NewSystemCounter(name, help string) prometheus.Counter {
	return promauto.With(systemRegistry).NewCounter(prometheus.CounterOpts{
		Namespace: systemNamespace,
		Subsystem: getPackageName(1),
		Name:      name,
		Help:      help,
	})
}

func NewSystemCounterFunc(name, help string, function func() float64) prometheus.CounterFunc {
	return promauto.With(systemRegistry).NewCounterFunc(prometheus.CounterOpts{
		Namespace: systemNamespace,
		Subsystem: getPackageName(1),
		Name:      name,
		Help:      help,
	}, function)
}

func NewSystemCounterVec(name, help string, labels []string) *prometheus.CounterVec {
	return promauto.With(systemRegistry).NewCounterVec(prometheus.CounterOpts{
		Namespace: systemNamespace,
		Subsystem: getPackageName(1),
		Name:      name,
		Help:      help,
	}, labels)
}

func NewSystemGauge(name, help string) prometheus.Gauge {
	return promauto.With(systemRegistry).NewGauge(prometheus.GaugeOpts{
		Namespace: systemNamespace,
		Subsystem: getPackageName(1),
		Name:      name,
		Help:      help,
	})
}

func NewSystemGaugeFunc(name, help string, function func() float64) prometheus.GaugeFunc {
	return promauto.With(systemRegistry).NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: systemNamespace,
		Subsystem: getPackageName(1),
		Name:      name,
		Help:      help,
	}, function)
}

func NewSystemGaugeVec(name, help string, labels []string) *prometheus.GaugeVec {
	return promauto.With(systemRegistry).NewGaugeVec(prometheus.GaugeOpts{
		Namespace: systemNamespace,
		Subsystem: getPackageName(1),
		Name:      name,
		Help:      help,
	}, labels)
}

func NewSystemHistogram(name, help string, buckets ...float64) prometheus.Histogram {
	opts := prometheus.HistogramOpts{
		Namespace: systemNamespace,
		Subsystem: getPackageName(1),
		Name:      name,
		Help:      help,
	}

	if len(buckets) > 0 {
		opts.Buckets = buckets
	}

	return promauto.With(systemRegistry).NewHistogram(opts)
}

func NewSystemHistogramVec(name, help string, labels []string, buckets ...float64) *prometheus.HistogramVec {
	opts := prometheus.HistogramOpts{
		Namespace: systemNamespace,
		Subsystem: getPackageName(1),
		Name:      name,
		Help:      help,
	}

	if len(buckets) > 0 {
		opts.Buckets = buckets
	}

	return promauto.With(systemRegistry).NewHistogramVec(opts, labels)
}

func NewSystemSummary(name, help string, objectives map[float64]float64) prometheus.Summary {
	return promauto.With(systemRegistry).NewSummary(prometheus.SummaryOpts{
		Namespace:  systemNamespace,
		Subsystem:  getPackageName(1),
		Name:       name,
		Help:       help,
		Objectives: objectives,
	})
}

func NewSystemSummaryVec(name, help string, labels []string, objectives map[float64]float64) *prometheus.SummaryVec {
	return promauto.With(systemRegistry).NewSummaryVec(prometheus.SummaryOpts{
		Namespace:  systemNamespace,
		Subsystem:  getPackageName(1),
		Name:       name,
		Help:       help,
		Objectives: objectives,
	}, labels)
}
