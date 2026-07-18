package gobin

import (
	"errors"
	"fmt"
	"testing"
)

func TestGetOffset(t *testing.T) {
	tests := []struct {
		name       string
		structPath string
		property   string
		version    string
		want       uint32
		wantErr    error
	}{
		{
			name:       "basic FD Sysfd lookup",
			structPath: "internal/poll.FD",
			property:   "Sysfd",
			version:    "1.19.0",
			want:       16,
			wantErr:    nil,
		},
		{
			name:       "runtime.g goid with old version",
			structPath: "runtime.g",
			property:   "goid",
			version:    "1.15.0",
			want:       152,
			wantErr:    nil,
		},
		{
			name:       "runtime.g goid with new version",
			structPath: "runtime.g",
			property:   "goid",
			version:    "1.23.0",
			want:       160,
			wantErr:    nil,
		},
		{
			name:       "invalid struct path",
			structPath: "does.not.exist",
			property:   "field",
			version:    "1.19.0",
			want:       0,
			wantErr:    ErrStructNotFound,
		},
		{
			name:       "invalid property",
			structPath: "internal/poll.FD",
			property:   "InvalidField",
			version:    "1.19.0",
			want:       0,
			wantErr:    ErrPropertyNotFound,
		},
		{
			name:       "version too old",
			structPath: "internal/poll.FD",
			property:   "Sysfd",
			version:    "1.13.0",
			want:       0,
			wantErr:    ErrNoValidOffset,
		},
		{
			name:       "version too new",
			structPath: "internal/poll.FD",
			property:   "Sysfd",
			version:    "1.29.0",
			want:       16,
			wantErr:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetOffset(tt.structPath, tt.property, tt.version)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("GetOffset() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GetOffset() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1   string
		v2   string
		want int
	}{
		{"1.19.0", "1.19.0", 0},
		{"1.19.0", "1.20.0", -1},
		{"1.20.0", "1.19.0", 1},
		{"1.19.1", "1.19.0", 1},
		{"1.19.0", "1.19.1", -1},
		{"1.19", "1.19.0", 0},
		{"1.19.0.0", "1.19.0", 0},
		{"1.23.0", "1.22.0", 1},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s vs %s", tt.v1, tt.v2), func(t *testing.T) {
			got := compareVersions(tt.v1, tt.v2)
			if got != tt.want {
				t.Errorf("compareVersions(%q, %q) = %v, want %v", tt.v1, tt.v2, got, tt.want)
			}
		})
	}
}
