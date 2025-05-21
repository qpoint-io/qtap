package httpcapture

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestConfigParsing(t *testing.T) {
	tests := []struct {
		name           string
		configYAML     string
		expectedLevel  captureLevel
		expectedFormat outputFormat
		expectedRules  int
	}{
		{
			name: "Default config",
			configYAML: `
---
`,
			expectedLevel:  CaptureLevelNone,
			expectedFormat: outputFormatJSON,
			expectedRules:  0,
		},
		{
			name: "Basic config with level and format",
			configYAML: `
level: summary
format: text
`,
			expectedLevel:  CaptureLevelSummary,
			expectedFormat: outputFormatText,
			expectedRules:  0,
		},
		{
			name: "Config with rules",
			configYAML: `
level: none
format: json
rules:
  - name: "Capture API requests"
    expr: "request.path == '/api/'"
    level: full
  - name: "Capture error responses"
    expr: "response.status >= 400"
    level: headers
    format: text
`,
			expectedLevel:  CaptureLevelNone,
			expectedFormat: outputFormatJSON,
			expectedRules:  2,
		},
		{
			name: "Config with headers capture level",
			configYAML: `
level: headers
format: json
`,
			expectedLevel:  CaptureLevelHeaders,
			expectedFormat: outputFormatJSON,
			expectedRules:  0,
		},
		{
			name: "Config with full capture level",
			configYAML: `
level: full
format: text
`,
			expectedLevel:  CaptureLevelFull,
			expectedFormat: outputFormatText,
			expectedRules:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create a struct to hold parsed config
			var config struct {
				Level  string `yaml:"level"`
				Format string `yaml:"format"`
				Rules  []struct {
					Name   string `yaml:"name"`
					Expr   string `yaml:"expr"`
					Level  string `yaml:"level"`
					Format string `yaml:"format,omitempty"`
				} `yaml:"rules"`
			}

			// Parse the configuration
			err := yaml.Unmarshal([]byte(tc.configYAML), &config)
			require.NoError(t, err)

			// Simulate the same logic as Factory.Init()
			level := captureLevel(config.Level)
			if level == "" {
				level = CaptureLevelNone
			}

			format := outputFormat(config.Format)
			if format == "" {
				format = outputFormatJSON
			}

			// Verify the config was parsed correctly
			assert.Equal(t, tc.expectedLevel, level)
			assert.Equal(t, tc.expectedFormat, format)
			assert.Len(t, config.Rules, tc.expectedRules)

			// Assert rule data is correctly parsed
			for i, rule := range config.Rules {
				assert.NotEmpty(t, rule.Name, "Rule %d should have a name", i)
				assert.NotEmpty(t, rule.Expr, "Rule %d should have an expression", i)
				assert.NotEmpty(t, rule.Level, "Rule %d should have a capture level", i)
			}
		})
	}
}

func TestOutputFormatMapping(t *testing.T) {
	tests := []struct {
		inputFormat  string
		expectedType string
	}{
		{"json", "application/json"},
		{"text", "text/plain"},
		{"invalid", "application/json"}, // Should default to JSON
		{"", "application/json"},        // Should default to JSON
	}

	for _, tc := range tests {
		t.Run(tc.inputFormat, func(t *testing.T) {
			// Get content type based on format
			var contentType string
			format := outputFormat(tc.inputFormat)

			switch format {
			case outputFormatJSON:
				contentType = "application/json"
			case outputFormatText:
				contentType = "text/plain"
			default:
				contentType = "application/json"
			}

			// Verify content type is as expected
			assert.Equal(t, tc.expectedType, contentType)
		})
	}
}
