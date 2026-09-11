//go:build linux

package process_test

import (
	"os/exec"
	"testing"
)

func TestExternalConsumer(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "go", "test", "-mod=readonly", "./...")
	cmd.Dir = "../../examples/process-monitor"
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("external consumer: %v\n%s", err, output)
	}
}
