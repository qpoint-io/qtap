package errordetection

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestDuration_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{
			name:    "valid string duration - no unit",
			input:   `"150"`,
			want:    150 * time.Millisecond,
			wantErr: false,
		},
		{
			name:    "valid string duration - seconds",
			input:   `"30s"`,
			want:    30 * time.Second,
			wantErr: false,
		},
		{
			name:    "valid string duration - minutes",
			input:   `"5m"`,
			want:    5 * time.Minute,
			wantErr: false,
		},
		{
			name:    "valid numeric duration (milliseconds)",
			input:   `500`,
			want:    500 * time.Millisecond,
			wantErr: false,
		},
		{
			name:    "invalid string duration",
			input:   `"invalid"`,
			want:    0,
			wantErr: true,
		},
		{
			name:    "invalid type (boolean)",
			input:   `true`,
			want:    0,
			wantErr: true,
		},
		{
			name:    "invalid json",
			input:   `{`,
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Duration
			err := d.UnmarshalJSON([]byte(tt.input))

			if tt.wantErr {
				if err == nil {
					t.Errorf("UnmarshalJSON() error = nil, wantErr = true")
				}
				return
			}

			if err != nil {
				t.Errorf("UnmarshalJSON() error = %v, wantErr = false", err)
				return
			}

			if d.Duration != tt.want {
				t.Errorf("UnmarshalJSON() got = %v, want %v", d.Duration, tt.want)
			}
		})
	}
}

func TestDuration_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{
			name:    "valid string duration - no unit",
			input:   `"150"`,
			want:    150 * time.Millisecond,
			wantErr: false,
		},
		{
			name:    "valid string duration - seconds",
			input:   `"30s"`,
			want:    30 * time.Second,
			wantErr: false,
		},
		{
			name:    "valid string duration - minutes",
			input:   `"5m"`,
			want:    5 * time.Minute,
			wantErr: false,
		},
		{
			name:    "valid numeric duration (milliseconds)",
			input:   `500`,
			want:    500 * time.Millisecond,
			wantErr: false,
		},
		{
			name:    "invalid string duration",
			input:   `"invalid"`,
			want:    0,
			wantErr: true,
		},
		{
			name:    "invalid type (boolean)",
			input:   `true`,
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var node yaml.Node
			err := yaml.Unmarshal([]byte(tt.input), &node)
			require.NoError(t, err)

			var d Duration
			err = d.UnmarshalYAML(&node)

			if tt.wantErr {
				if err == nil {
					t.Errorf("UnmarshalYAML() error = nil, wantErr = true")
				}
				return
			}

			if err != nil {
				t.Errorf("UnmarshalYAML() error = %v, wantErr = false", err)
				return
			}

			if d.Duration != tt.want {
				t.Errorf("UnmarshalYAML() got = %v, want %v", d.Duration, tt.want)
			}
		})
	}
}
