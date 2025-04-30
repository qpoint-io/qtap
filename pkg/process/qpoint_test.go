package process

import (
	"regexp"
	"testing"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateTapFilter(t *testing.T) {
	tests := []struct {
		name        string
		filterStr   string
		want        *config.TapFilter
		wantErr     bool
		errContains string
	}{
		{
			name:      "valid contains filter",
			filterStr: "exe.contains:java",
			want: &config.TapFilter{
				Exe:      "java",
				Strategy: config.MatchStrategy_CONTAINS,
			},
			wantErr: false,
		},
		{
			name:      "valid exact filter",
			filterStr: "exe.exact:java",
			want: &config.TapFilter{
				Exe:      "java",
				Strategy: config.MatchStrategy_EXACT,
			},
			wantErr: false,
		},
		{
			name:      "valid prefix filter",
			filterStr: "exe.prefix:java",
			want: &config.TapFilter{
				Exe:      "java",
				Strategy: config.MatchStrategy_PREFIX,
			},
			wantErr: false,
		},
		{
			name:      "valid suffix filter",
			filterStr: "exe.suffix:java",
			want: &config.TapFilter{
				Exe:      "java",
				Strategy: config.MatchStrategy_SUFFIX,
			},
			wantErr: false,
		},
		{
			name:      "valid regex filter",
			filterStr: "exe.regex:java.*",
			want: &config.TapFilter{
				Exe:      "java.*",
				Strategy: config.MatchStrategy_REGEX,
			},
			wantErr: false,
		},
		{
			name:        "invalid format - missing colon",
			filterStr:   "exe.contains",
			wantErr:     true,
			errContains: "invalid filter format",
		},
		{
			name:        "invalid format - missing dot",
			filterStr:   "exe:java",
			wantErr:     true,
			errContains: "invalid match strategy",
		},
		{
			name:        "empty filter string",
			filterStr:   "",
			wantErr:     true,
			errContains: "invalid filter format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := createTapFilter(tt.filterStr)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, got)
			assert.Equal(t, tt.want.Exe, got.Exe)
			assert.Equal(t, tt.want.Strategy, got.Strategy)
		})
	}
}

func TestQpointStrategyFromString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		process *Process
		want    QpointStrategy
		wantErr bool
	}{
		{
			name:    "simple observe strategy",
			input:   "observe",
			process: &Process{Exe: "test"},
			want:    StrategyObserve,
		},
		{
			name:    "simple ignore strategy",
			input:   "ignore",
			process: &Process{Exe: "test"},
			want:    StrategyIgnore,
		},
		{
			name:    "simple audit strategy",
			input:   "audit",
			process: &Process{Exe: "test"},
			want:    StrategyAudit,
		},
		{
			name:    "simple forward strategy",
			input:   "forward",
			process: &Process{Exe: "test"},
			want:    StrategyForward,
		},
		{
			name:    "simple proxy strategy",
			input:   "proxy",
			process: &Process{Exe: "test"},
			want:    StrategyProxy,
		},
		{
			name:    "unknown strategy defaults to observe",
			input:   "unknown",
			process: &Process{Exe: "test"},
			want:    StrategyObserve,
		},
		{
			name:    "matching exact filter",
			input:   "proxy,exe.exact:test",
			process: &Process{Exe: "test"},
			want:    StrategyProxy,
		},
		{
			name:    "non-matching exact filter defaults to observe",
			input:   "proxy,exe.exact:other",
			process: &Process{Exe: "test"},
			want:    StrategyObserve,
		},
		{
			name:    "invalid filter format",
			input:   "proxy,invalid_filter",
			process: &Process{Exe: "test"},
			want:    StrategyObserve,
			wantErr: true,
		},
		{
			name:    "multiple matching filters - first matches",
			input:   "proxy,exe.contains:te,exe.suffix:st",
			process: &Process{Exe: "test"},
			want:    StrategyProxy,
		},
		{
			name:    "multiple matching filters - second matches",
			input:   "proxy,exe.contains:other,exe.suffix:test",
			process: &Process{Exe: "test"},
			want:    StrategyProxy,
		},
		{
			name:    "multiple non-matching filters defaults to observe",
			input:   "proxy,exe.contains:other,exe.suffix:fail",
			process: &Process{Exe: "test"},
			want:    StrategyObserve,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := QpointStrategyFromString(tt.input, tt.process)
			if (err != nil) != tt.wantErr {
				t.Errorf("QpointStrategyFromString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("QpointStrategyFromString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFromConfigFilter(t *testing.T) {
	// TODO
}

func TestExeFilter(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		strategy    config.MatchStrategy
		input       string
		expected    bool
		expectError bool
		errorMsg    string
	}{
		{
			name:        "Exact match - success",
			pattern:     "example.exe",
			strategy:    config.MatchStrategy_EXACT,
			input:       "example.exe",
			expected:    true,
			expectError: false,
		},
		{
			name:        "Exact match - failure",
			pattern:     "example.exe",
			strategy:    config.MatchStrategy_EXACT,
			input:       "other.exe",
			expected:    false,
			expectError: false,
		},
		{
			name:        "Prefix match - success",
			pattern:     "exam",
			strategy:    config.MatchStrategy_PREFIX,
			input:       "example.exe",
			expected:    true,
			expectError: false,
		},
		{
			name:        "Prefix match - failure",
			pattern:     "exam",
			strategy:    config.MatchStrategy_PREFIX,
			input:       "sample.exe",
			expected:    false,
			expectError: false,
		},
		{
			name:        "Suffix match - success",
			pattern:     ".exe",
			strategy:    config.MatchStrategy_SUFFIX,
			input:       "example.exe",
			expected:    true,
			expectError: false,
		},
		{
			name:        "Suffix match - failure",
			pattern:     ".exe",
			strategy:    config.MatchStrategy_SUFFIX,
			input:       "example.txt",
			expected:    false,
			expectError: false,
		},
		{
			name:        "Contains match - success",
			pattern:     "example",
			strategy:    config.MatchStrategy_CONTAINS,
			input:       "example.exe",
			expected:    true,
			expectError: false,
		},
		{
			name:        "Contains match - failure",
			pattern:     "no?",
			strategy:    config.MatchStrategy_CONTAINS,
			input:       "example.txt",
			expected:    false,
			expectError: false,
		},
		{
			name:        "Default match - success",
			pattern:     "example.exe",
			input:       "example.exe",
			expected:    true,
			expectError: false,
		},
		{
			name:        "Unknown strategy",
			pattern:     "something",
			strategy:    "idk",
			input:       "example.exe",
			expected:    false,
			expectError: true,
			errorMsg:    "invalid strategy: idk",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := &ExeFilter{
				pattern:  tt.pattern,
				strategy: tt.strategy,
			}
			result, err := filter.Evaluate(&Process{Exe: tt.input})

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestExeRegexFilter(t *testing.T) {
	tests := []struct {
		name        string
		pattern     *regexp.Regexp
		input       string
		expected    bool
		expectError bool
		errorMsg    string
	}{
		{
			name:        "Regex match - success",
			pattern:     regexp.MustCompile(`^[a-z]+\.exe$`),
			input:       "example.exe",
			expected:    true,
			expectError: false,
		},
		{
			name:        "Regex match - failure",
			pattern:     regexp.MustCompile(`^[a-z]+\.exe$`),
			input:       "example.txt",
			expected:    false,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := &ExeRegexFilter{
				pattern: tt.pattern,
			}
			result, err := filter.Evaluate(&Process{Exe: tt.input})

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestPIDFilter(t *testing.T) {
	filter := &PIDFilter{
		PID: 123,
	}
	result, err := filter.Evaluate(&Process{Pid: 123})
	require.NoError(t, err)
	require.True(t, result)

	result, err = filter.Evaluate(&Process{Pid: 456})
	require.NoError(t, err)
	require.False(t, result)
}
