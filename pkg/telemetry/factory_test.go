package telemetry

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

// mockRegisterer implements prometheus.Registerer for testing
type mockRegisterer struct {
	registerCalled   bool
	unregisterCalled bool
}

func (m *mockRegisterer) Register(c prometheus.Collector) error {
	m.registerCalled = true
	return nil
}

func (m *mockRegisterer) MustRegister(cs ...prometheus.Collector) {
	m.registerCalled = true
}

func (m *mockRegisterer) Unregister(c prometheus.Collector) bool {
	m.unregisterCalled = true
	return true
}

func TestFactory_Counter(t *testing.T) {
	// Test global function
	counterFn := Counter("test_counter", WithDescription("Test counter"), WithLabels("label1", "label2"))
	assert.NotNil(t, counterFn, "Counter function should not be nil")

	// Test factory instance with custom registerer
	mockReg := &mockRegisterer{}
	f := &factory{registerer: mockReg}

	counterFn = f.Counter("test_counter", WithDescription("Test counter"), WithLabels("label1", "label2"))
	assert.NotNil(t, counterFn, "Counter function should not be nil")
	assert.True(t, mockReg.registerCalled, "Register should be called")
}

func TestFactory_Gauge(t *testing.T) {
	// Test global function
	gaugeFn := Gauge("test_gauge", WithDescription("Test gauge"), WithLabels("label1", "label2"))
	assert.NotNil(t, gaugeFn, "Gauge function should not be nil")

	// Test factory instance with custom registerer
	mockReg := &mockRegisterer{}
	f := &factory{registerer: mockReg}

	gaugeFn = f.Gauge("test_gauge", WithDescription("Test gauge"), WithLabels("label1", "label2"))
	assert.NotNil(t, gaugeFn, "Gauge function should not be nil")
	assert.True(t, mockReg.registerCalled, "Register should be called")

	// Test gauge slice appending
	assert.Len(t, f.gauges, 1, "Gauge should be appended to the gauge slice")
}

func TestFactory_ObservableGauge(t *testing.T) {
	// Test global function
	observableFn := func() float64 { return 123.45 }
	ObservableGauge("test_observable", observableFn, WithDescription("Test observable gauge"))

	// Since this doesn't return anything, we're just ensuring it doesn't panic

	// Test factory instance with custom registerer
	mockReg := &mockRegisterer{}
	f := &factory{registerer: mockReg}

	f.ObservableGauge("test_observable", observableFn, WithDescription("Test observable gauge"))
	assert.True(t, mockReg.registerCalled, "Register should be called")
}

func TestFactory_ObservableGauge_PanicsWithLabels(t *testing.T) {
	observableFn := func() float64 { return 123.45 }

	assert.Panics(t, func() {
		ObservableGauge("test_observable", observableFn, WithLabels("label1", "label2"))
	}, "ObservableGauge should panic when labels are provided")
}

func TestFactory_ObservableGauge_MultipleCalls(t *testing.T) {
	// Test global function
	observableFn := func() float64 { return 123.45 }
	ObservableGauge("test_observable", observableFn, WithDescription("Test observable gauge"))

	// second call /w same name
	ObservableGauge("test_observable", observableFn, WithDescription("Test observable gauge"))

	// Since this doesn't return anything, we're just ensuring it doesn't panic

	// Test factory instance with custom registerer
	mockReg := &mockRegisterer{}
	f := &factory{registerer: mockReg}

	f.ObservableGauge("test_observable", observableFn, WithDescription("Test observable gauge"))
	assert.True(t, mockReg.registerCalled, "Register should be called")
}
