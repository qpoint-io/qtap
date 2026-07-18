package qscan

import (
	"math/rand"
	"sync"
)

// Cache defines the interface for the sampling cache
type Cache interface {
	Get(key string) (uint32, bool)
	Add(key string, value uint32) bool
}

// Sampler encapsulates the sampling cache, configuration, and decision logic
type Sampler struct {
	mu       sync.Mutex
	cache    Cache
	baseline uint32
	rate     float64
}

// NewSampler creates a new Sampler instance with the provided cache, baseline, and rate
func NewSampler(cache Cache, baseline uint32, rate float64) *Sampler {
	return &Sampler{
		cache:    cache,
		baseline: baseline,
		rate:     rate,
	}
}

// ShouldSample performs the sampling logic for a given cache key
func (s *Sampler) ShouldSample(domain string) (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	count, ok := s.cache.Get(domain)
	if !ok {
		cacheOperationsTotal.WithLabelValues("miss").Inc()
		count = 0
	} else {
		cacheOperationsTotal.WithLabelValues("hit").Inc()
	}

	count++

	s.cache.Add(domain, count)

	if count < s.baseline {
		return true, "baseline"
	}

	if rand.Float64() < s.rate {
		return true, "rate"
	}

	return false, "none"
}
