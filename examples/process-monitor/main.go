//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/process/monitor"
)

type observer struct {
	process.DefaultObserver
}

func (*observer) ProcessStarted(_ context.Context, p *process.Process) error {
	fmt.Printf("started pid=%d executable=%s existing=%t\n", p.Pid, p.Exe, p.PredatesQpoint)
	return nil
}

func run(ctx context.Context) error {
	m, err := monitor.New(nil)
	if err != nil {
		return err
	}
	defer m.Stop()
	m.Observe(&observer{})
	if err := m.Start(); err != nil {
		return err
	}
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
