package httpcapture

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

func TestHttpCaptureConfigParsing(t *testing.T) {
	tests := []struct {
		name           string
		configYAML     string
		expectedLevel  CaptureLevel
		expectedFormat OutputFormat
		expectedRules  int
	}{
		{
			name: "Default config",
			configYAML: `
---
`,
			expectedLevel:  CaptureLevelNone,
			expectedFormat: OutputFormatJSON,
			expectedRules:  0,
		},
		{
			name: "Basic config with level and format",
			configYAML: `
level: summary
format: text
`,
			expectedLevel:  CaptureLevelSummary,
			expectedFormat: OutputFormatText,
			expectedRules:  0,
		},
		{
			name: "Config with rules",
			configYAML: `
level: none
format: json
rules:
  - name: "Capture API requests"
    expr: "request.path startsWith '/api/'"
    level: full
  - name: "Capture error responses"
    expr: "response.status >= 400"
    level: headers
    format: text
`,
			expectedLevel:  CaptureLevelNone,
			expectedFormat: OutputFormatJSON,
			expectedRules:  2,
		},
		{
			name: "Config with headers capture level",
			configYAML: `
level: headers
format: json
`,
			expectedLevel:  CaptureLevelHeaders,
			expectedFormat: OutputFormatJSON,
			expectedRules:  0,
		},
		{
			name: "Config with full capture level",
			configYAML: `
level: full
format: text
`,
			expectedLevel:  CaptureLevelFull,
			expectedFormat: OutputFormatText,
			expectedRules:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Parse the YAML config
			var node yaml.Node
			err := yaml.Unmarshal([]byte(tc.configYAML), &node)
			require.NoError(t, err)

			// Create a factory and initialize it with the config
			factory := &Factory{}
			logger, _ := zap.NewDevelopment()
			factory.Init(logger, node)

			// Verify the config was parsed correctly
			assert.Equal(t, tc.expectedLevel, factory.config.Level)
			assert.Equal(t, tc.expectedFormat, factory.config.Format)
			assert.Len(t, factory.config.Rules, tc.expectedRules)
		})
	}
}

func TestCaptureLevelConstants(t *testing.T) {
	tests := []struct {
		level CaptureLevel
		value string
		valid bool
	}{
		{CaptureLevelNone, "none", true},
		{CaptureLevelSummary, "summary", true},
		{CaptureLevelHeaders, "headers", true},
		{CaptureLevelFull, "full", true},
		{CaptureLevel("invalid"), "invalid", false},
	}

	for _, tc := range tests {
		t.Run(string(tc.level), func(t *testing.T) {
			// Verify that the string value matches the expected value
			assert.Equal(t, tc.value, string(tc.level))

			// Verify the capture level is one of the valid constants
			is_valid := tc.level == CaptureLevelNone ||
				tc.level == CaptureLevelSummary ||
				tc.level == CaptureLevelHeaders ||
				tc.level == CaptureLevelFull

			assert.Equal(t, tc.valid, is_valid)
		})
	}
}

func TestOutputFormatConstants(t *testing.T) {
	tests := []struct {
		format OutputFormat
		value  string
		valid  bool
	}{
		{OutputFormatJSON, "json", true},
		{OutputFormatText, "text", true},
		{OutputFormat("invalid"), "invalid", false},
	}

	for _, tc := range tests {
		t.Run(string(tc.format), func(t *testing.T) {
			// Verify that the string value matches the expected value
			assert.Equal(t, tc.value, string(tc.format))

			// Verify the output format is one of the valid constants
			is_valid := tc.format == OutputFormatJSON || tc.format == OutputFormatText

			assert.Equal(t, tc.valid, is_valid)
		})
	}
}
