package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTapFilterValidate(t *testing.T) {
	tests := []struct {
		name        string
		filter      TapFilter
		expectError bool
		errorMsg    string
	}{
		{
			name:        "Valid exact match",
			filter:      TapFilter{Exe: "example.exe", Strategy: MatchStrategy_EXACT},
			expectError: false,
		},
		{
			name:        "Valid prefix match",
			filter:      TapFilter{Exe: "pre", Strategy: MatchStrategy_PREFIX},
			expectError: false,
		},
		{
			name:        "Valid suffix match",
			filter:      TapFilter{Exe: "suffix", Strategy: MatchStrategy_SUFFIX},
			expectError: false,
		},
		{
			name:        "Valid regex match",
			filter:      TapFilter{Exe: "^[a-z]+\\.exe$", Strategy: MatchStrategy_REGEX},
			expectError: false,
		},
		{
			name:        "Empty exe",
			filter:      TapFilter{Exe: "", Strategy: MatchStrategy_EXACT},
			expectError: true,
			errorMsg:    "exe must not be empty",
		},
		{
			name:        "Invalid regex",
			filter:      TapFilter{Exe: "[invalid(", Strategy: MatchStrategy_REGEX},
			expectError: true,
			errorMsg:    "invalid regex for exe:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.filter.Validate()

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestTapFilterPack(t *testing.T) {
	tests := []struct {
		name     string
		filter   TapFilter
		expected uint8
	}{
		{
			name: "No filters set",
			filter: TapFilter{
				Exe:      "test",
				Strategy: MatchStrategy_EXACT,
			},
			expected: SkipDataFlag | SkipDNSFlag | SkipTLSFlag | SkipHTTPFlag,
		},
		{
			name: "All filters set",
			filter: TapFilter{
				Exe:      "test",
				Strategy: MatchStrategy_EXACT,
				Only:     []FilterLevel{FilterLevel_DATA, FilterLevel_DNS, FilterLevel_TLS, FilterLevel_HTTP},
			},
			expected: SkipDataFlag | SkipDNSFlag | SkipTLSFlag | SkipHTTPFlag,
		},
		{
			name: "Only Middleware filter",
			filter: TapFilter{
				Exe:      "test",
				Strategy: MatchStrategy_EXACT,
				Only:     []FilterLevel{FilterLevel_DATA},
			},
			expected: SkipDataFlag,
		},
		{
			name: "Only DNS filter",
			filter: TapFilter{
				Exe:      "test",
				Strategy: MatchStrategy_EXACT,
				Only:     []FilterLevel{FilterLevel_DNS},
			},
			expected: SkipDNSFlag,
		},
		{
			name: "Only TLS filter",
			filter: TapFilter{
				Exe:      "test",
				Strategy: MatchStrategy_EXACT,
				Only:     []FilterLevel{FilterLevel_TLS},
			},
			expected: SkipTLSFlag,
		},
		{
			name: "Only HTTP filter",
			filter: TapFilter{
				Exe:      "test",
				Strategy: MatchStrategy_EXACT,
				Only:     []FilterLevel{FilterLevel_HTTP},
			},
			expected: SkipHTTPFlag,
		},
		{
			name: "Middleware and TLS filters",
			filter: TapFilter{
				Exe:      "test",
				Strategy: MatchStrategy_EXACT,
				Only:     []FilterLevel{FilterLevel_DATA, FilterLevel_TLS},
			},
			expected: SkipDataFlag | SkipTLSFlag,
		},
		{
			name: "DNS and HTTP filters",
			filter: TapFilter{
				Exe:      "test",
				Strategy: MatchStrategy_EXACT,
				Only:     []FilterLevel{FilterLevel_DNS, FilterLevel_HTTP},
			},
			expected: SkipDNSFlag | SkipHTTPFlag,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.filter.Pack()
			require.Equal(t, tt.expected, result, "Packed flags do not match expected value")
		})
	}
}
