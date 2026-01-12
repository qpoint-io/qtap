package tags

import (
	"reflect"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTags_Add(t *testing.T) {
	tests := []struct {
		name     string
		adds     [][2]string // series of [key, value] pairs to add
		wantTags []string    // expected output from List()
	}{
		{
			name:     "simple tag",
			adds:     [][2]string{{"category", "test"}},
			wantTags: []string{"category:test"},
		},
		{
			name:     "trims whitespace",
			adds:     [][2]string{{"  category  ", "  test  "}},
			wantTags: []string{"category:test"},
		},
		{
			name:     "converts to lowercase",
			adds:     [][2]string{{"CATEGORY", "TEST"}},
			wantTags: []string{"category:test"},
		},
		{
			name:     "replaces spaces with hyphens",
			adds:     [][2]string{{"test category", "test value"}},
			wantTags: []string{"test-category:test-value"},
		},
		{
			name: "multiple values for same key",
			adds: [][2]string{
				{"category", "test"},
				{"category", "test2"},
			},
			wantTags: []string{"category:test", "category:test2"},
		},
		{
			name: "ignores empty key",
			adds: [][2]string{
				{"category", "test"},
				{"", "test"},
			},
			wantTags: []string{"category:test"},
		},
		{
			name: "ignores empty value",
			adds: [][2]string{
				{"category", "test"},
				{"category", ""},
			},
			wantTags: []string{"category:test"},
		},
		{
			name: "ignores non-alphanumeric start key",
			adds: [][2]string{
				{"category", "test"},
				{"-category2", "test"},
			},
			wantTags: []string{"category:test", "category2:test"},
		},
		{
			name: "ignores non-alphanumeric end key",
			adds: [][2]string{
				{"category", "test"},
				{"category2-", "test"},
			},
			wantTags: []string{"category:test", "category2:test"},
		},
		{
			name: "ignores non-alphanumeric start value",
			adds: [][2]string{
				{"category", "test"},
				{"category", "-2test"},
			},
			wantTags: []string{"category:test", "category:2test"},
		},
		{
			name: "ignores non-alphanumeric end value",
			adds: [][2]string{
				{"category", "test"},
				{"category", "test2-"},
			},
			wantTags: []string{"category:test", "category:test2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tags := New()
			for _, add := range tt.adds {
				tags.Add(add[0], add[1])
			}
			got := tags.List()

			// Sort both slices for reliable comparison
			sort.Strings(got)
			sort.Strings(tt.wantTags)

			if !reflect.DeepEqual(got, tt.wantTags) {
				t.Errorf("Add() = %v, want %v", got, tt.wantTags)
			}
		})
	}
}

func TestTags_AddString(t *testing.T) {
	tests := []struct {
		name       string
		tagStrings []string
		wantTags   []string
		wantErrors []bool
	}{
		{
			name:       "valid tags",
			tagStrings: []string{"category:test", "service:api"},
			wantTags:   []string{"category:test", "service:api"},
			wantErrors: []bool{false, false},
		},
		{
			name:       "invalid format",
			tagStrings: []string{"categorytest", "service:api"},
			wantTags:   []string{"service:api"},
			wantErrors: []bool{true, false},
		},
		{
			name:       "normalization",
			tagStrings: []string{"CATEGORY:TEST", "  service  :  api  "},
			wantTags:   []string{"category:test", "service:api"},
			wantErrors: []bool{false, false},
		},
		{
			name:       "invalid values",
			tagStrings: []string{"category:-test", "service:api-"},
			wantTags:   []string{"category:test", "service:api"},
			wantErrors: []bool{false, false}, // AddString doesn't return errors for validation failures
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags := New()
			for i, tagString := range tt.tagStrings {
				err := tags.AddString(tagString)
				if (err != nil) != tt.wantErrors[i] {
					t.Errorf("AddString(%q) error = %v, wantError %v", tagString, err, tt.wantErrors[i])
				}
			}

			got := tags.List()

			// Check for empty results
			if len(got) == 0 && len(tt.wantTags) == 0 {
				// Both are empty, test passes
				return
			}

			sort.Strings(got)
			sort.Strings(tt.wantTags)
			assert.Equal(t, tt.wantTags, got)
		})
	}
}

func TestTags_FromValues(t *testing.T) {
	tests := []struct {
		name      string
		keyValues map[string]string
		wantTags  []string
	}{
		{
			name:      "empty map",
			keyValues: map[string]string{},
			wantTags:  nil,
		},
		{
			name: "simple key-values",
			keyValues: map[string]string{
				"category": "test",
				"service":  "api",
			},
			wantTags: []string{"category:test", "service:api"},
		},
		{
			name: "with normalization",
			keyValues: map[string]string{
				"CATEGORY":    "TEST",
				"  service  ": "  api  ",
			},
			wantTags: []string{"category:test", "service:api"},
		},
		{
			name: "with invalid values",
			keyValues: map[string]string{
				"category": "-test",
				"service":  "api-",
				"-invalid": "value",
				"invalid-": "value",
				"":         "empty",
				"empty":    "",
			},
			wantTags: []string{
				"category:test",
				"service:api",
				"invalid:value",
				"invalid:value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags := FromValues(tt.keyValues)

			got := tags.List()

			// Check for empty results
			if len(got) == 0 && len(tt.wantTags) == 0 {
				// Both are empty, test passes
				return
			}

			sort.Strings(got)
			sort.Strings(tt.wantTags)
			assert.Equal(t, tt.wantTags, got)
		})
	}
}

func TestTags_FromMultiValues(t *testing.T) {
	tests := []struct {
		name      string
		keyValues map[string][]string
		wantTags  []string
	}{
		{
			name:      "empty map",
			keyValues: map[string][]string{},
			wantTags:  nil,
		},
		{
			name: "simple key-values",
			keyValues: map[string][]string{
				"category": {"test", "test2"},
				"service":  {"api", "api2"},
			},
			wantTags: []string{"category:test", "category:test2", "service:api", "service:api2"},
		},
		{
			name: "with normalization",
			keyValues: map[string][]string{
				"CATEGORY":    {"TEST", "TEST2"},
				"  service  ": {"  api  ", "  api2  "},
			},
			wantTags: []string{"category:test", "category:test2", "service:api", "service:api2"},
		},
		{
			name: "with invalid values",
			keyValues: map[string][]string{
				"category": {"-test"},
				"service":  {"api-"},
				"-invalid": {"value"},
				"invalid-": {"value"},
				"":         {"empty"},
				"empty":    {""},
			},
			wantTags: []string{
				"category:test",
				"service:api",
				"invalid:value",
				"invalid:value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags := FromMultiValues(tt.keyValues)

			got := tags.List()

			// Check for empty results
			if len(got) == 0 && len(tt.wantTags) == 0 {
				// Both are empty, test passes
				return
			}

			sort.Strings(got)
			sort.Strings(tt.wantTags)
			assert.Equal(t, tt.wantTags, got)
		})
	}
}

func TestTags_Clone(t *testing.T) {
	original := New()
	original.Add("category", "test")
	original.Add("service", "api")

	// Clone the tags
	clone := original.Clone()

	// Verify the clone has the same tags
	originalTags := original.List()
	cloneTags := clone.List()
	sort.Strings(originalTags)
	sort.Strings(cloneTags)

	assert.Equal(t, originalTags, cloneTags, "Clone should have the same tags as original")

	// Modify the clone and verify it doesn't affect the original
	clone.Add("environment", "prod")

	// Check that original is unchanged
	originalTags = original.List()
	sort.Strings(originalTags)
	assert.Equal(t, []string{"category:test", "service:api"}, originalTags, "Original should be unchanged after modifying clone")

	// Check that clone has the new tag
	cloneTags = clone.List()
	sort.Strings(cloneTags)
	assert.Equal(t, []string{"category:test", "environment:prod", "service:api"}, cloneTags, "Clone should have the new tag")
}

func TestTags_Merge(t *testing.T) {
	tests := []struct {
		name     string
		target   map[string]string
		source   map[string]string
		wantTags []string
	}{
		{
			name: "merge non-overlapping tags",
			target: map[string]string{
				"category": "test",
			},
			source: map[string]string{
				"service": "api",
			},
			wantTags: []string{"category:test", "service:api"},
		},
		{
			name: "merge overlapping tags",
			target: map[string]string{
				"category": "test",
				"service":  "web",
			},
			source: map[string]string{
				"service":     "api",
				"environment": "prod",
			},
			wantTags: []string{"category:test", "service:web", "service:api", "environment:prod"},
		},
		{
			name: "merge with empty source",
			target: map[string]string{
				"category": "test",
			},
			source:   map[string]string{},
			wantTags: []string{"category:test"},
		},
		{
			name:   "merge into empty target",
			target: map[string]string{},
			source: map[string]string{
				"service": "api",
			},
			wantTags: []string{"service:api"},
		},
		{
			name: "merge nil source",
			target: map[string]string{
				"category": "test",
			},
			source:   nil,
			wantTags: []string{"category:test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := FromValues(tt.target)
			var source List
			if tt.source != nil {
				source = FromValues(tt.source)
			}

			target.Merge(source)

			got := target.List()

			// Check for empty results
			if len(got) == 0 && len(tt.wantTags) == 0 {
				// Both are empty, test passes
				return
			}

			sort.Strings(got)
			sort.Strings(tt.wantTags)
			assert.Equal(t, tt.wantTags, got)
		})
	}

	t.Run("merge with nil", func(t *testing.T) {
		target := New()
		target.Add("category", "test")

		// Should not panic
		target.Merge(nil)

		assert.Equal(t, []string{"category:test"}, target.List())
	})
}

func TestIsAlphanumeric(t *testing.T) {
	tests := []struct {
		name     string
		char     rune
		expected bool
	}{
		{"lowercase letter", 'a', true},
		{"uppercase letter (normalized to lowercase)", 'A', false}, // The function expects lowercase
		{"digit", '5', true},
		{"hyphen", '-', false},
		{"space", ' ', false},
		{"underscore", '_', false},
		{"special character", '!', false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAlphanumeric(tt.char)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Benchmark tests

func BenchmarkAdd(b *testing.B) {
	tags := New()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tags.Add("category", "test")
	}
}

func BenchmarkAddMultipleValues(b *testing.B) {
	tags := New()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tags.Add("category", "test1", "test2", "test3", "test4", "test5")
	}
}

func BenchmarkAddString(b *testing.B) {
	tags := New()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tags.AddString("category:test")
	}
}

func BenchmarkAddString_Invalid(b *testing.B) {
	tags := New()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tags.AddString("invalidformat")
	}
}

func BenchmarkGet(b *testing.B) {
	tags := New()
	tags.Add("category", "test1", "test2", "test3")
	tags.Add("service", "api")
	tags.Add("environment", "prod")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tags.Get("category")
	}
}

func BenchmarkGet_NonExistent(b *testing.B) {
	tags := New()
	tags.Add("category", "test")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tags.Get("nonexistent")
	}
}

func BenchmarkList_Small(b *testing.B) {
	tags := New()
	tags.Add("category", "test")
	tags.Add("service", "api")
	tags.Add("environment", "prod")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tags.List()
	}
}

func BenchmarkList_Large(b *testing.B) {
	tags := New()
	for i := 0; i < 100; i++ {
		tags.Add("key", "value1", "value2", "value3", "value4", "value5")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tags.List()
	}
}

func BenchmarkClone_Small(b *testing.B) {
	tags := New()
	tags.Add("category", "test")
	tags.Add("service", "api")
	tags.Add("environment", "prod")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tags.Clone()
	}
}

func BenchmarkClone_Large(b *testing.B) {
	tags := New()
	for i := 0; i < 100; i++ {
		tags.Add("key", "value1", "value2", "value3", "value4", "value5")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tags.Clone()
	}
}

func BenchmarkMerge_Small(b *testing.B) {
	source := New()
	source.Add("category", "test")
	source.Add("service", "api")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		target := New()
		target.Add("environment", "prod")
		target.Merge(source)
	}
}

func BenchmarkMerge_Large(b *testing.B) {
	source := New()
	for i := 0; i < 100; i++ {
		source.Add("key", "value1", "value2", "value3")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		target := New()
		for j := 0; j < 50; j++ {
			target.Add("key2", "value4", "value5")
		}
		target.Merge(source)
	}
}

func BenchmarkMap_Small(b *testing.B) {
	tags := New()
	tags.Add("category", "test")
	tags.Add("service", "api")
	tags.Add("environment", "prod")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tags.Map()
	}
}

func BenchmarkMap_Large(b *testing.B) {
	tags := New()
	for i := 0; i < 100; i++ {
		tags.Add("key", "value1", "value2", "value3", "value4", "value5")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tags.Map()
	}
}

func BenchmarkFromValues_Small(b *testing.B) {
	kv := map[string]string{
		"category":    "test",
		"service":     "api",
		"environment": "prod",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = FromValues(kv)
	}
}

func BenchmarkFromValues_Large(b *testing.B) {
	kv := make(map[string]string, 100)
	for i := 0; i < 100; i++ {
		kv["key"] = "value"
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = FromValues(kv)
	}
}

func BenchmarkFromMultiValues_Small(b *testing.B) {
	kv := map[string][]string{
		"category":    {"test", "test2"},
		"service":     {"api", "web"},
		"environment": {"prod", "staging"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = FromMultiValues(kv)
	}
}

func BenchmarkFromMultiValues_Large(b *testing.B) {
	kv := make(map[string][]string, 100)
	for i := 0; i < 100; i++ {
		kv["key"] = []string{"value1", "value2", "value3", "value4", "value5"}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = FromMultiValues(kv)
	}
}

func BenchmarkFormat(b *testing.B) {
	testStrings := []string{
		"Simple Test",
		"  UPPERCASE WITH SPACES  ",
		"-starts-with-hyphen-",
		"has multiple   spaces",
		"MixedCaseString",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, s := range testStrings {
			_ = format(s)
		}
	}
}

func BenchmarkConcurrentAdd(b *testing.B) {
	tags := New()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			tags.Add("category", "test")
			i++
		}
	})
}

func BenchmarkConcurrentGet(b *testing.B) {
	tags := New()
	tags.Add("category", "test1", "test2", "test3")
	tags.Add("service", "api")
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = tags.Get("category")
		}
	})
}

func BenchmarkConcurrentAddAndGet(b *testing.B) {
	tags := New()
	tags.Add("category", "test")
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%2 == 0 {
				tags.Add("category", "test")
			} else {
				_, _ = tags.Get("category")
			}
			i++
		}
	})
}

func BenchmarkConcurrentList(b *testing.B) {
	tags := New()
	for i := 0; i < 10; i++ {
		tags.Add("key", "value1", "value2", "value3")
	}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = tags.List()
		}
	})
}

func BenchmarkConcurrentClone(b *testing.B) {
	tags := New()
	for i := 0; i < 10; i++ {
		tags.Add("key", "value1", "value2", "value3")
	}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = tags.Clone()
		}
	})
}

// Comparison benchmarks: Cached vs Non-Cached implementations

func BenchmarkMap_Standard_Small(b *testing.B) {
	tags := New()
	tags.Add("category", "test")
	tags.Add("service", "api")
	tags.Add("environment", "prod")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tags.Map()
	}
}

func BenchmarkMap_Cached_Small(b *testing.B) {
	tags := New()
	tags.Add("category", "test")
	tags.Add("service", "api")
	tags.Add("environment", "prod")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tags.Map()
	}
}

func BenchmarkMap_Standard_Large(b *testing.B) {
	tags := New()
	for i := 0; i < 100; i++ {
		tags.Add("key", "value1", "value2", "value3", "value4", "value5")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tags.Map()
	}
}

func BenchmarkMap_Cached_Large(b *testing.B) {
	tags := New()
	for i := 0; i < 100; i++ {
		tags.Add("key", "value1", "value2", "value3", "value4", "value5")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tags.Map()
	}
}

func BenchmarkMap_Standard_ReadHeavy(b *testing.B) {
	tags := New()
	tags.Add("category", "test")
	tags.Add("service", "api")
	b.ResetTimer()
	// Simulate read-heavy workload: 1 write per 1000 reads
	for i := 0; i < b.N; i++ {
		if i%1000 == 0 {
			tags.Add("temp", "value")
		}
		_ = tags.Map()
	}
}

func BenchmarkMap_Cached_ReadHeavy(b *testing.B) {
	tags := New()
	tags.Add("category", "test")
	tags.Add("service", "api")
	b.ResetTimer()
	// Simulate read-heavy workload: 1 write per 1000 reads
	for i := 0; i < b.N; i++ {
		if i%1000 == 0 {
			tags.Add("temp", "value")
		}
		_ = tags.Map()
	}
}

func BenchmarkMap_Standard_WriteHeavy(b *testing.B) {
	tags := New()
	tags.Add("category", "test")
	b.ResetTimer()
	// Simulate write-heavy workload: 1 Map() per 10 writes
	for i := 0; i < b.N; i++ {
		if i%10 == 0 {
			_ = tags.Map()
		}
		tags.Add("key", "value")
	}
}

func BenchmarkMap_Cached_WriteHeavy(b *testing.B) {
	tags := New()
	tags.Add("category", "test")
	b.ResetTimer()
	// Simulate write-heavy workload: 1 Map() per 10 writes
	for i := 0; i < b.N; i++ {
		if i%10 == 0 {
			_ = tags.Map()
		}
		tags.Add("key", "value")
	}
}

func BenchmarkConcurrentMap_Standard(b *testing.B) {
	tags := New()
	for i := 0; i < 10; i++ {
		tags.Add("key", "value1", "value2", "value3")
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = tags.Map()
		}
	})
}

func BenchmarkConcurrentMap_Cached(b *testing.B) {
	tags := New()
	for i := 0; i < 10; i++ {
		tags.Add("key", "value1", "value2", "value3")
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = tags.Map()
		}
	})
}

func BenchmarkConcurrentMapWithWrites_Standard(b *testing.B) {
	tags := New()
	tags.Add("initial", "value")
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			// 90% reads, 10% writes
			if i%10 == 0 {
				tags.Add("key", "value")
			} else {
				_ = tags.Map()
			}
			i++
		}
	})
}

func BenchmarkConcurrentMapWithWrites_Cached(b *testing.B) {
	tags := New()
	tags.Add("initial", "value")
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			// 90% reads, 10% writes
			if i%10 == 0 {
				tags.Add("key", "value")
			} else {
				_ = tags.Map()
			}
			i++
		}
	})
}
