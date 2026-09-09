//go:build integration && linux

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/qpoint-io/qtap/pkg/process/monitor"
)

func TestConstructionFailure(t *testing.T) {
	if os.Getenv("QTAP_MONITOR_UNPRIVILEGED_CHILD") == "1" {
		m, err := monitor.New(nil)
		if m != nil {
			_ = m.Stop()
		}
		if !errors.Is(err, syscall.EPERM) && !errors.Is(err, syscall.EACCES) {
			t.Fatalf("expected a returned permission error, got %v", err)
		}
		fmt.Println("application continues after discovery error")
		return
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// A Go test binary may live under a root-only build directory. Give the
	// unprivileged child a temporary executable it can actually reach.
	dir, err := os.MkdirTemp("", "qtap-monitor-permission-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(dir, "consumer.test")
	if err := os.WriteFile(child, data, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), child, "-test.run=^TestConstructionFailure$")
	cmd.Env = append(os.Environ(), "QTAP_MONITOR_UNPRIVILEGED_CHILD=1")
	if os.Geteuid() == 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("unprivileged consumer: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "application continues after discovery error") {
		t.Fatalf("caller did not continue after failed construction:\n%s", output)
	}
}
