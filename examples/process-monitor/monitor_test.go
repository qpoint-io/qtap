//go:build integration && linux

package main

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/process"
)

type observedProcess struct {
	kind     string
	pid      int
	existing bool
	exe      string
	exitCode int
}

type lifecycleObserver struct {
	process.DefaultObserver
	ctx      context.Context
	childExe string
	mu       sync.Mutex
	tracked  map[int]bool
	events   chan observedProcess
}

func (o *lifecycleObserver) ProcessStarted(_ context.Context, p *process.Process) error {
	o.mu.Lock()
	if p.Pid != os.Getpid() && p.ExeFilename == o.childExe {
		o.tracked[p.Pid] = true
	}
	o.mu.Unlock()
	return o.record("started", p)
}

func (o *lifecycleObserver) ProcessReplaced(_ context.Context, p *process.Process) error {
	return o.record("replaced", p)
}

func (o *lifecycleObserver) ProcessStopped(_ context.Context, p *process.Process) error {
	return o.record("stopped", p)
}

func (o *lifecycleObserver) record(kind string, p *process.Process) error {
	o.mu.Lock()
	tracked := o.tracked[p.Pid]
	o.mu.Unlock()
	if !tracked {
		return nil
	}
	event := observedProcess{kind: kind, pid: p.Pid, existing: p.PredatesQpoint}
	// Read only fields relevant to this transition. For example, ExitCode is
	// written on exit and must not be sampled from a start/replace callback.
	switch kind {
	case "replaced":
		event.exe = p.Exe
	case "stopped":
		event.exitCode = p.ExitCode
	}
	select {
	case o.events <- event:
	case <-o.ctx.Done():
	}
	return nil
}

func awaitEvent(t *testing.T, events <-chan observedProcess, pid int, kind string) observedProcess {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-events:
			if event.pid == pid && event.kind == kind {
				return event
			}
		case <-timer.C:
			t.Fatalf("no %s callback for pid %d", kind, pid)
		}
	}
}

// Each program constructs only one manager, even though the test exercises
// several lifecycle transitions.
func TestProcessDiscovery(t *testing.T) {
	target := exec.CommandContext(t.Context(), "sleep", "30")
	if err := target.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = target.Process.Kill()
		_ = target.Wait()
	})

	m, err := process.NewMonitor(nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Stop(); err != nil {
			t.Error(err)
		}
	})
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	o := &lifecycleObserver{
		ctx: t.Context(), childExe: exe,
		tracked: map[int]bool{target.Process.Pid: true},
		events:  make(chan observedProcess, 8),
	}
	m.Observe(o)
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	if event := awaitEvent(t, o.events, target.Process.Pid, "started"); !event.existing {
		t.Fatal("startup process was not marked as preexisting")
	}

	live := exec.CommandContext(t.Context(), exe, "-test.run=^TestProcessMonitorChild$")
	live.Env = append(os.Environ(), "QTAP_PROCESS_MONITOR_CHILD=1")
	input, err := live.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if err := live.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = live.Process.Kill()
		if live.ProcessState == nil {
			_ = live.Wait()
		}
	})
	pid := live.Process.Pid
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	proc, err := m.Await(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if event := awaitEvent(t, o.events, pid, "started"); event.existing {
		t.Fatal("live process was marked as preexisting")
	}
	if proc.Pid != pid || m.Get(pid) != proc {
		t.Fatal("lookup and wait did not return the discovered process")
	}
	found := false
	m.SnapshotProcesses(func(snapshotPID int, _ *process.Process) bool {
		found = found || snapshotPID == pid
		return true
	})
	if !found {
		t.Fatal("live process missing from snapshot")
	}

	if _, err := input.Write([]byte("replace\n")); err != nil {
		t.Fatal(err)
	}
	replaced := awaitEvent(t, o.events, pid, "replaced")
	if replaced.exe != "/usr/bin/cat" && replaced.exe != "/bin/cat" {
		t.Fatalf("unexpected replacement executable: %s", replaced.exe)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	if err := live.Wait(); err != nil {
		t.Fatal(err)
	}
	if event := awaitEvent(t, o.events, pid, "stopped"); event.exitCode != 0 {
		t.Fatalf("unexpected exit code: %d", event.exitCode)
	}
	if m.Get(pid) != nil {
		t.Fatal("stopped process remains in registry")
	}
	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
}

// This child blocks until the parent has observed its start, then replaces its
// executable. The parent closes stdin only after receiving the replaced callback.
func TestProcessMonitorChild(t *testing.T) {
	if os.Getenv("QTAP_PROCESS_MONITOR_CHILD") != "1" {
		t.Skip("child process helper")
	}
	if !bufio.NewScanner(os.Stdin).Scan() {
		t.Fatal("parent did not request replacement")
	}
	if err := syscall.Exec("/bin/cat", []string{"cat"}, os.Environ()); err != nil {
		t.Fatal(err)
	}
}
