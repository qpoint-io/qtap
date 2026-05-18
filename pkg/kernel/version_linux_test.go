//go:build linux

package kernel

import "testing"

func TestParseRelease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		release string
		want    VersionInfo
	}{
		{
			name:    "semantic version with flavor",
			release: "6.8.0-85-generic",
			want:    VersionInfo{Kernel: 6, Major: 8, Minor: 0, Flavor: "-85-generic"},
		},
		{
			name:    "version without patch keeps flavor",
			release: "3.12-1-amd64",
			want:    VersionInfo{Kernel: 3, Major: 12, Flavor: "-1-amd64"},
		},
		{
			name:    "plain semantic version",
			release: "5.10.0",
			want:    VersionInfo{Kernel: 5, Major: 10, Minor: 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseRelease(tt.release)
			if err != nil {
				t.Fatalf("ParseRelease() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ParseRelease() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseReleaseRejectsInvalidVersion(t *testing.T) {
	t.Parallel()

	if _, err := ParseRelease("not-a-kernel"); err == nil {
		t.Fatal("ParseRelease() expected error")
	}
}

func TestCompareVersion(t *testing.T) {
	t.Parallel()

	minimum := VersionInfo{Kernel: 5, Major: 10, Minor: 0}
	if got := CompareVersion(VersionInfo{Kernel: 5, Major: 9, Minor: 9}, minimum); got >= 0 {
		t.Fatalf("CompareVersion() = %d, want < 0", got)
	}
	if got := CompareVersion(VersionInfo{Kernel: 5, Major: 10, Minor: 0}, minimum); got != 0 {
		t.Fatalf("CompareVersion() = %d, want 0", got)
	}
	if got := CompareVersion(VersionInfo{Kernel: 6, Major: 0, Minor: 0}, minimum); got <= 0 {
		t.Fatalf("CompareVersion() = %d, want > 0", got)
	}
}
