//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/qpoint-io/qtap/pkg/process"
)

type observer struct {
	process.DefaultObserver
	manager *process.Manager
}

func (o *observer) ProcessStarted(_ context.Context, p *process.Process) error {
	fmt.Printf("started pid=%d existing=%t tracked=%t\n", p.Pid, p.PredatesQpoint, o.manager.Get(p.Pid) != nil)
	return nil
}

func (*observer) ProcessReplaced(_ context.Context, p *process.Process) error {
	fmt.Printf("replaced pid=%d\n", p.Pid)
	return nil
}

func (*observer) ProcessStopped(_ context.Context, p *process.Process) error {
	fmt.Printf("stopped pid=%d exit_code=%d\n", p.Pid, p.ExitCode)
	return nil
}

func run(ctx context.Context) error {
	m, err := process.NewMonitor(nil)
	if err != nil {
		return err
	}
	defer m.Stop()
	m.Observe(&observer{manager: m.Manager})
	if err := m.Start(); err != nil {
		return err
	}
	count := 0
	m.SnapshotProcesses(func(_ int, _ *process.Process) bool {
		count++
		return true
	})
	fmt.Printf("tracking %d processes\n", count)
	<-ctx.Done()
	return m.Stop()
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
