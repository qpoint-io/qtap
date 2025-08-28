package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const defaultNamespace = "qtap"

func NewCounter(name, help string) prometheus.Counter {
	return promauto.With(productRegistry).NewCounter(prometheus.CounterOpts{
		Namespace: defaultNamespace,
		Subsystem: getPackageName(1),
		Name:      name,
		Help:      help,
	})
}

func NewCounterFunc(name, help string, function func() float64) prometheus.CounterFunc {
	return promauto.With(productRegistry).NewCounterFunc(prometheus.CounterOpts{
		Namespace: defaultNamespace,
		Subsystem: getPackageName(1),
		Name:      name,
		Help:      help,
	}, function)
}

func NewCounterVec(name, help string, labels []string) *prometheus.CounterVec {
	return promauto.With(productRegistry).NewCounterVec(prometheus.CounterOpts{
		Namespace: defaultNamespace,
		Subsystem: getPackageName(1),
		Name:      name,
		Help:      help,
	}, labels)
}

func NewGauge(name, help string) prometheus.Gauge {
	return promauto.With(productRegistry).NewGauge(prometheus.GaugeOpts{
		Namespace: defaultNamespace,
		Subsystem: getPackageName(1),
		Name:      name,
		Help:      help,
	})
}

func NewGaugeFunc(name, help string, function func() float64) prometheus.GaugeFunc {
	return promauto.With(productRegistry).NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: defaultNamespace,
		Subsystem: getPackageName(1),
		Name:      name,
		Help:      help,
	}, function)
}

func NewGaugeVec(name, help string, labels []string) *prometheus.GaugeVec {
	return promauto.With(productRegistry).NewGaugeVec(prometheus.GaugeOpts{
		Namespace: defaultNamespace,
		Subsystem: getPackageName(1),
		Name:      name,
		Help:      help,
	}, labels)
}

func NewHistogram(name, help string, buckets ...float64) prometheus.Histogram {
	opts := prometheus.HistogramOpts{
		Namespace: defaultNamespace,
		Subsystem: getPackageName(1),
		Name:      name,
		Help:      help,
	}

	if len(buckets) > 0 {
		opts.Buckets = buckets
	}

	return promauto.With(productRegistry).NewHistogram(opts)
}

func NewHistogramVec(name, help string, labels []string, buckets ...float64) *prometheus.HistogramVec {
	opts := prometheus.HistogramOpts{
		Namespace: defaultNamespace,
		Subsystem: getPackageName(1),
		Name:      name,
		Help:      help,
	}

	if len(buckets) > 0 {
		opts.Buckets = buckets
	}

	return promauto.With(productRegistry).NewHistogramVec(opts, labels)
}

func NewSummary(name, help string, objectives map[float64]float64) prometheus.Summary {
	return promauto.With(productRegistry).NewSummary(prometheus.SummaryOpts{
		Namespace:  defaultNamespace,
		Subsystem:  getPackageName(1),
		Name:       name,
		Help:       help,
		Objectives: objectives,
	})
}

func NewSummaryVec(name, help string, labels []string, objectives map[float64]float64) *prometheus.SummaryVec {
	return promauto.With(productRegistry).NewSummaryVec(prometheus.SummaryOpts{
		Namespace:  defaultNamespace,
		Subsystem:  getPackageName(1),
		Name:       name,
		Help:       help,
		Objectives: objectives,
	}, labels)
}
