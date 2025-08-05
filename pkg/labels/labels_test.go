package labels

import (
	"testing"

	"github.com/qpoint-io/rulekit/set"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name     string
		items    []string
		expected []string
	}{
		{
			name:     "empty labels",
			items:    []string{},
			expected: []string{},
		},
		{
			name:     "single label",
			items:    []string{"production"},
			expected: []string{"production"},
		},
		{
			name:     "multiple labels",
			items:    []string{"production", "web", "api"},
			expected: []string{"production", "web", "api"},
		},
		{
			name:     "duplicate labels",
			items:    []string{"production", "production", "web"},
			expected: []string{"production", "web"},
		},
		{
			name:     "labels with spaces",
			items:    []string{"production server", "web service"},
			expected: []string{"production-server", "web-service"},
		},
		{
			name:     "labels with mixed case",
			items:    []string{"Production", "WEB", "Api"},
			expected: []string{"production", "web", "api"},
		},
		{
			name:     "labels with leading/trailing spaces",
			items:    []string{"  production  ", "  web  "},
			expected: []string{"production", "web"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels := New(tt.items...)
			require.ElementsMatch(t, tt.expected, set.Set[string](labels).Items())
		})
	}
}

func TestLabelSet_Add(t *testing.T) {
	t.Run("add to existing set", func(t *testing.T) {
		labels := New("production")
		labels.Add("web", "api")

		expected := []string{"production", "web", "api"}
		require.ElementsMatch(t, expected, set.Set[string](labels).Items())
	})

	t.Run("add duplicate items", func(t *testing.T) {
		labels := New("production")
		labels.Add("production", "web")

		expected := []string{"production", "web"}
		require.ElementsMatch(t, expected, set.Set[string](labels).Items())
	})

	t.Run("add formatted items", func(t *testing.T) {
		labels := New("production")
		labels.Add("Production Server", "WEB SERVICE", "  api  ")

		expected := []string{"production", "production-server", "web-service", "api"}
		require.ElementsMatch(t, expected, set.Set[string](labels).Items())
	})
}

func TestFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple lowercase",
			input:    "production",
			expected: "production",
		},
		{
			name:     "uppercase to lowercase",
			input:    "PRODUCTION",
			expected: "production",
		},
		{
			name:     "mixed case to lowercase",
			input:    "Production",
			expected: "production",
		},
		{
			name:     "spaces to hyphens",
			input:    "production server",
			expected: "production-server",
		},
		{
			name:     "multiple spaces to single hyphen",
			input:    "production   server",
			expected: "production---server",
		},
		{
			name:     "leading spaces",
			input:    "  production",
			expected: "production",
		},
		{
			name:     "trailing spaces",
			input:    "production  ",
			expected: "production",
		},
		{
			name:     "leading and trailing spaces",
			input:    "  production  ",
			expected: "production",
		},
		{
			name:     "spaces and mixed case",
			input:    "Production Server",
			expected: "production-server",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only spaces",
			input:    "   ",
			expected: "",
		},
		{
			name:     "special characters preserved",
			input:    "prod-server-1",
			expected: "prod-server-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := format(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestLabelSet_Integration(t *testing.T) {
	t.Run("complex label operations", func(t *testing.T) {
		labels := New("Production Server", "WEB API")
		labels.Add("  Database  ", "cache-redis")

		expected := []string{"production-server", "web-api", "database", "cache-redis"}
		require.ElementsMatch(t, expected, set.Set[string](labels).Items())
	})

	t.Run("set operations inherited from set.Set", func(t *testing.T) {
		labels := New("production", "web")

		// Test Contains
		require.True(t, set.Set[string](labels).Contains("production"))
		require.True(t, set.Set[string](labels).Contains("web"))
		require.False(t, set.Set[string](labels).Contains("api"))

		// Test Len
		require.Equal(t, 2, set.Set[string](labels).Len())

		// Test Remove
		set.Set[string](labels).Remove("web")
		require.False(t, set.Set[string](labels).Contains("web"))
		require.Equal(t, 1, set.Set[string](labels).Len())

		// Test Items
		items := set.Set[string](labels).Items()
		require.ElementsMatch(t, []string{"production"}, items)
	})
}
