package qscan

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	classifier "github.com/jonfriesen/trie-url-classifier"
	"github.com/qpoint-io/qtap/pkg/synq"
	"go.uber.org/zap"
)

// BenchmarkShouldSample_Sequential tests sampling performance with sequential requests
func BenchmarkShouldSample_Sequential(b *testing.B) {
	f := setupBenchmarkFactory(b)

	urls := []string{
		"https://api.example.com/users/123",
		"https://api.example.com/users/456",
		"https://api.example.com/posts/789",
		"https://api.example.com/comments/101",
	}

	b.ResetTimer()
	for i := range b.N {
		url := urls[i%len(urls)]
		f.shouldSample(url)
	}
}

// BenchmarkShouldSample_Parallel tests sampling performance with concurrent requests
func BenchmarkShouldSample_Parallel(b *testing.B) {
	f := setupBenchmarkFactory(b)

	urls := []string{
		"https://api.example.com/users/123",
		"https://api.example.com/users/456",
		"https://api.example.com/posts/789",
		"https://api.example.com/comments/101",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			url := urls[i%len(urls)]
			f.shouldSample(url)
			i++
		}
	})
}

// BenchmarkShouldSample_HighContention simulates high contention with same domain
func BenchmarkShouldSample_HighContention(b *testing.B) {
	f := setupBenchmarkFactory(b)

	// All requests to same domain to maximize contention on Sampler.mu
	urls := []string{
		"https://api.example.com/users/1",
		"https://api.example.com/users/2",
		"https://api.example.com/users/3",
		"https://api.example.com/users/4",
		"https://api.example.com/posts/1",
		"https://api.example.com/posts/2",
		"https://api.example.com/comments/1",
		"https://api.example.com/comments/2",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			url := urls[i%len(urls)]
			f.shouldSample(url)
			i++
		}
	})
}

// BenchmarkShouldSample_LowContention simulates low contention with different domains
func BenchmarkShouldSample_LowContention(b *testing.B) {
	f := setupBenchmarkFactory(b)

	// Different domains to minimize contention
	urls := []string{
		"https://api1.example.com/users/123",
		"https://api2.example.com/users/456",
		"https://api3.example.com/posts/789",
		"https://api4.example.com/comments/101",
		"https://api5.example.com/data/202",
		"https://api6.example.com/items/303",
		"https://api7.example.com/records/404",
		"https://api8.example.com/events/505",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			url := urls[i%len(urls)]
			f.shouldSample(url)
			i++
		}
	})
}

// BenchmarkGetOrCreateClassifier_ExistingClassifier tests classifier lookup performance
func BenchmarkGetOrCreateClassifier_ExistingClassifier(b *testing.B) {
	f := setupBenchmarkFactory(b)

	// Pre-populate classifier
	f.getOrCreateClassifier("api.example.com")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			f.getOrCreateClassifier("api.example.com")
		}
	})
}

// BenchmarkGetOrCreateClassifier_NewClassifier tests classifier creation performance
func BenchmarkGetOrCreateClassifier_NewClassifier(b *testing.B) {
	domains := make([]string, b.N)
	for i := range b.N {
		domains[i] = fmt.Sprintf("api%d.example.com", i)
	}

	b.ResetTimer()
	for i := range b.N {
		f := setupBenchmarkFactory(b)
		f.getOrCreateClassifier(domains[i])
	}
}

// BenchmarkGetOrCreateClassifier_ConcurrentCreation tests race-free classifier creation
func BenchmarkGetOrCreateClassifier_ConcurrentCreation(b *testing.B) {
	f := setupBenchmarkFactory(b)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			// Multiple goroutines trying to create the same classifier
			domain := fmt.Sprintf("api%d.example.com", i%10)
			f.getOrCreateClassifier(domain)
			i++
		}
	})
}

// BenchmarkFullPipeline simulates realistic request processing
func BenchmarkFullPipeline(b *testing.B) {
	f := setupBenchmarkFactory(b)

	// Mix of domains and paths simulating realistic traffic
	urls := generateRealisticURLs(1000)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			url := urls[i%len(urls)]
			sampled, reason := f.shouldSample(url)
			// Simulate some work based on sampling result
			if sampled {
				_ = reason // Would normally process the request
			}
			i++
		}
	})
}

// BenchmarkSampler_ShouldSample tests the Sampler component in isolation
func BenchmarkSampler_ShouldSample(b *testing.B) {
	cache := expirable.NewLRU[string, uint32](4096, nil, time.Hour)
	sampler := NewSampler(cache, 100, 0.1)

	keys := []string{
		"api.example.com/users",
		"api.example.com/posts",
		"api.example.com/comments",
		"api.example.com/data",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := keys[i%len(keys)]
			sampler.ShouldSample(key)
			i++
		}
	})
}

// BenchmarkClassifierClassify tests the classifier performance in isolation
func BenchmarkClassifierClassify(b *testing.B) {
	clf := classifier.NewClassifier(
		classifier.WithMinLearningCount(0),
		classifier.WithCardinalityThreshold(0.75),
		classifier.WithMinSamples(2),
		classifier.WithMaxValuesPerNode(100),
		classifier.WithPruneHighCardinality(true),
	)

	paths := []string{
		"/users/123",
		"/users/456",
		"/users/789",
		"/posts/abc",
		"/posts/def",
		"/posts/ghi",
		"/comments/111",
		"/comments/222",
		"/data/item/333",
		"/data/item/444",
	}

	// Train the classifier
	for range 100 {
		for _, path := range paths {
			_, _ = clf.Classify(path)
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			path := paths[i%len(paths)]
			_, _ = clf.Classify(path)
			i++
		}
	})
}

// setupBenchmarkFactory creates a Factory instance for benchmarking
func setupBenchmarkFactory(b *testing.B) *Factory {
	b.Helper()

	config := &QscanConfig{
		CacheTTL:       time.Hour,
		CacheSize:      4096,
		SampleBaseline: 100,
		SampleRate:     0.1,
		Classifier: ClassifierConfig{
			MinLearningCount:     0,
			CardinalityThreshold: 0.75,
			MinSamples:           2,
			MaxValuesPerNode:     100,
			PruneHighCardinality: func() *bool { b := true; return &b }(),
		},
	}

	cache := expirable.NewLRU[string, uint32](config.CacheSize, nil, config.CacheTTL)
	classifiers := synq.NewMap[string, *classifier.Classifier]()

	f := &Factory{
		logger:      zap.NewNop(),
		config:      config,
		sampler:     NewSampler(cache, config.SampleBaseline, config.SampleRate),
		classifiers: classifiers,
	}

	return f
}

// generateRealisticURLs creates a set of realistic URLs for benchmarking
func generateRealisticURLs(count int) []string {
	domains := []string{
		"api.example.com",
		"api.test.com",
		"api.demo.com",
		"service.example.com",
		"backend.test.com",
	}

	paths := []string{
		"/users/%d",
		"/posts/%d",
		"/comments/%d",
		"/data/items/%d",
		"/api/v1/resources/%d",
		"/api/v2/entities/%d",
		"/admin/users/%d",
		"/public/posts/%d",
		"/internal/metrics/%d",
		"/external/webhooks/%d",
	}

	urls := make([]string, count)
	for i := range count {
		domain := domains[i%len(domains)]
		pathTemplate := paths[i%len(paths)]
		path := fmt.Sprintf(pathTemplate, i%1000)
		urls[i] = fmt.Sprintf("https://%s%s", domain, path)
	}

	return urls
}

// BenchmarkCompareContention compares performance at different concurrency levels
func BenchmarkCompareContention(b *testing.B) {
	concurrencyLevels := []int{1, 2, 4, 8, 16, 32, 64}

	for _, level := range concurrencyLevels {
		b.Run(fmt.Sprintf("goroutines-%d", level), func(b *testing.B) {
			f := setupBenchmarkFactory(b)
			urls := generateRealisticURLs(100)

			var wg sync.WaitGroup
			opsPerGoroutine := b.N / level

			b.ResetTimer()
			for i := range level {
				wg.Add(1)
				go func(goroutineID int) {
					defer wg.Done()
					for j := range opsPerGoroutine {
						url := urls[(goroutineID*opsPerGoroutine+j)%len(urls)]
						f.shouldSample(url)
					}
				}(i)
			}
			wg.Wait()
		})
	}
}
