package telemetry

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var defaultRegisterer = prometheus.DefaultRegisterer

var (
	onceMu         sync.Mutex
	observableOnce = make(map[string]*sync.Once)
)

// Factory produces metrics.
type Factory interface {
	Counter(name string, opts ...Option) CounterFn
	Gauge(name string, opts ...Option) GaugeFn
	ObservableGauge(name string, fn func() float64, opts ...Option)
}

type factory struct {
	registerer prometheus.Registerer
	gauges     []*prometheus.GaugeVec
}

func Counter(name string, opts ...Option) CounterFn {
	return (&factory{
		registerer: defaultRegisterer,
	}).Counter(name, opts...)
}

func Gauge(name string, opts ...Option) GaugeFn {
	return (&factory{
		registerer: defaultRegisterer,
	}).Gauge(name, opts...)
}

func ObservableGauge(name string, fn func() float64, opts ...Option) {
	onceMu.Lock()
	o, ok := observableOnce[name]
	if !ok {
		o = &sync.Once{}
		observableOnce[name] = o
	}
	onceMu.Unlock()

	o.Do(func() {
		(&factory{
			registerer: defaultRegisterer,
		}).ObservableGauge(name, fn, opts...)
	})
}
