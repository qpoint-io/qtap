//go:build linux

package container_test

import (
	"os/exec"
	"testing"
)

func TestExternalMonitor(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "go", "test", "-mod=readonly", "./...")
	cmd.Dir = "../../examples/container-monitor"
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("external container monitor: %v\n%s", err, output)
	}
}
