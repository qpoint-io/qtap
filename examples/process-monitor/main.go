//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"

	"github.com/qpoint-io/qtap/pkg/process"
)

type observer struct {
	process.DefaultObserver
	manager *process.Manager
}

// details are the process fields shown for started and replaced events. They
// are copied under the process mutex because a later exec can replace the
// executable and arguments while a callback is still running.
type details struct {
	pid      int
	exe      string
	binary   string
	args     []string
	existing bool
	tracked  bool
}

func capture(p *process.Process) details {
	p.Lock()
	defer p.Unlock()
	return details{
		pid:      p.Pid,
		exe:      p.Exe,
		binary:   p.Binary,
		args:     slices.Clone(p.Args),
		existing: p.PredatesQpoint,
	}
}

// describe renders one event as a single block. Values are quoted so spaces
// and embedded newlines stay inside their field instead of starting new lines.
func describe(kind string, d details) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s pid=%d existing=%t", kind, d.pid, d.existing)
	if kind == "started" {
		fmt.Fprintf(&b, " tracked=%t", d.tracked)
	}
	fmt.Fprintf(&b, "\n  exe:    %q\n  binary: %q\n", d.exe, d.binary)
	if len(d.args) == 0 {
		// Processes found at startup have no argv event, so an empty list means
		// the arguments were not captured, not that there were none.
		b.WriteString("  args:   not captured\n")
	} else {
		fmt.Fprintf(&b, "  args:   %q\n", d.args)
	}
	return b.String()
}

func (o *observer) ProcessStarted(_ context.Context, p *process.Process) error {
	d := capture(p)
	// The registry lock and the write happen after the process mutex is released.
	d.tracked = o.manager.Get(p.Pid) != nil
	fmt.Print(describe("started", d))
	return nil
}

func (*observer) ProcessReplaced(_ context.Context, p *process.Process) error {
	fmt.Print(describe("replaced", capture(p)))
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
