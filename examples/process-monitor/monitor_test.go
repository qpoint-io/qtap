//go:build integration && linux

package main

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/process/monitor"
)

type startupObserver struct {
	process.DefaultObserver
	pid      int
	existing chan bool
}

func (o *startupObserver) ProcessStarted(_ context.Context, p *process.Process) error {
	if p.Pid == o.pid {
		o.existing <- p.PredatesQpoint
	}
	return nil
}

func TestStartupDiscovery(t *testing.T) {
	target := exec.CommandContext(t.Context(), "sleep", "30")
	if err := target.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = target.Process.Kill()
		_ = target.Wait()
	})

	m, err := monitor.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Stop(); err != nil {
			t.Error(err)
		}
	})
	o := &startupObserver{pid: target.Process.Pid, existing: make(chan bool, 1)}
	m.Observe(o)
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case existing := <-o.existing:
		if !existing {
			t.Fatal("startup process was not marked as preexisting")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no started callback for startup process")
	}
	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
}
