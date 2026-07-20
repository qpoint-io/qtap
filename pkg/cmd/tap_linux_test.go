//go:build linux

package cmd

import "testing"

func TestEgressControllerEnabledFromEnv(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: "true", want: true},
		{value: "1", want: true},
		{value: "false", want: false},
		{value: "0", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Setenv("ENABLE_EGRESS_CONTROLLER", tt.value)
			if got := egressControllerEnabledFromEnv(); got != tt.want {
				t.Fatalf("egressControllerEnabledFromEnv() = %t, want %t", got, tt.want)
			}
		})
	}
}
