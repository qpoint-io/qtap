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
