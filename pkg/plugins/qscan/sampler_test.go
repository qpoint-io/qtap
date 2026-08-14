package qscan

import (
	"testing"
)

// mockCache implements the Cache interface for testing
type mockCache struct {
	data map[string]uint32
}

func newMockCache() *mockCache {
	return &mockCache{data: make(map[string]uint32)}
}

func (m *mockCache) Get(key string) (uint32, bool) {
	val, ok := m.data[key]
	return val, ok
}

func (m *mockCache) Add(key string, value uint32) bool {
	m.data[key] = value
	return true
}

func TestSampler_ShouldSample_Baseline(t *testing.T) {
	cache := newMockCache()
	sampler := NewSampler(cache, 3, 0.0) // baseline=3, rate=0 (never sample after baseline)

	// Count is incremented before comparison, so with baseline=3:
	// request 1: count becomes 1, 1 < 3 → baseline
	// request 2: count becomes 2, 2 < 3 → baseline
	// request 3: count becomes 3, 3 < 3 is false → rate check (rate=0 → none)
	for i := 1; i <= 2; i++ {
		shouldSample, reason := sampler.ShouldSample("example.com")
		if !shouldSample {
			t.Errorf("request %d: expected shouldSample=true, got false", i)
		}
		if reason != "baseline" {
			t.Errorf("request %d: expected reason='baseline', got '%s'", i, reason)
		}
	}

	// 3rd request should not be sampled (count=3 is not < baseline=3, rate=0)
	shouldSample, reason := sampler.ShouldSample("example.com")
	if shouldSample {
		t.Errorf("request 3: expected shouldSample=false, got true")
	}
	if reason != "none" {
		t.Errorf("request 3: expected reason='none', got '%s'", reason)
	}
}

func TestSampler_ShouldSample_RateAlways(t *testing.T) {
	cache := newMockCache()
	sampler := NewSampler(cache, 2, 1.0) // baseline=2, rate=1.0 (always sample after baseline)

	// First request: count becomes 1, 1 < 2 → baseline
	shouldSample, reason := sampler.ShouldSample("example.com")
	if !shouldSample || reason != "baseline" {
		t.Errorf("request 1: expected (true, 'baseline'), got (%v, '%s')", shouldSample, reason)
	}

	// Subsequent requests should be sampled due to rate=1.0
	// request 2: count becomes 2, 2 < 2 is false → rate check (rate=1.0 → rate)
	for i := 2; i <= 5; i++ {
		shouldSample, reason := sampler.ShouldSample("example.com")
		if !shouldSample {
			t.Errorf("request %d: expected shouldSample=true, got false", i)
		}
		if reason != "rate" {
			t.Errorf("request %d: expected reason='rate', got '%s'", i, reason)
		}
	}
}

func TestSampler_ShouldSample_DifferentDomains(t *testing.T) {
	cache := newMockCache()
	sampler := NewSampler(cache, 3, 0.0) // baseline=3, rate=0

	// Each domain should have its own counter
	domains := []string{"a.com", "b.com", "c.com"}

	// First request per domain: count becomes 1, 1 < 3 → baseline
	for _, domain := range domains {
		shouldSample, reason := sampler.ShouldSample(domain)
		if !shouldSample || reason != "baseline" {
			t.Errorf("domain %s first request: expected (true, 'baseline'), got (%v, '%s')", domain, shouldSample, reason)
		}
	}

	// Second request per domain: count becomes 2, 2 < 3 → baseline
	for _, domain := range domains {
		shouldSample, reason := sampler.ShouldSample(domain)
		if !shouldSample || reason != "baseline" {
			t.Errorf("domain %s second request: expected (true, 'baseline'), got (%v, '%s')", domain, shouldSample, reason)
		}
	}

	// Third request per domain: count becomes 3, 3 < 3 is false → rate=0 → none
	for _, domain := range domains {
		shouldSample, reason := sampler.ShouldSample(domain)
		if shouldSample {
			t.Errorf("domain %s third request: expected shouldSample=false, got true", domain)
		}
		if reason != "none" {
			t.Errorf("domain %s third request: expected reason='none', got '%s'", domain, reason)
		}
	}
}

func TestSampler_ShouldSample_ZeroBaseline(t *testing.T) {
	cache := newMockCache()
	sampler := NewSampler(cache, 0, 1.0) // baseline=0, rate=1.0

	// With baseline=0, first request should use rate sampling
	shouldSample, reason := sampler.ShouldSample("example.com")
	if !shouldSample {
		t.Errorf("expected shouldSample=true with rate=1.0, got false")
	}
	if reason != "rate" {
		t.Errorf("expected reason='rate', got '%s'", reason)
	}
}

func TestSampler_ShouldSample_CounterIncrement(t *testing.T) {
	cache := newMockCache()
	sampler := NewSampler(cache, 10, 0.0)

	domain := "test.com"

	// Make 5 requests
	for range 5 {
		sampler.ShouldSample(domain)
	}

	// Verify counter was incremented
	count, ok := cache.Get(domain)
	if !ok {
		t.Fatal("expected domain to be in cache")
	}
	if count != 5 {
		t.Errorf("expected count=5, got %d", count)
	}
}
