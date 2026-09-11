//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/qpoint-io/qtap/pkg/container"
	"go.uber.org/zap"
)

func quoteOr(s string) string {
	if s == "" {
		return "unavailable"
	}
	return strconv.Quote(s)
}

// describe quotes runtime values so embedded newlines cannot break event blocks.
// Pod identity comes from labels; calling Pod would lazily mutate the record.
func describe(kind, runtime string, c *container.Container) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s runtime=%q id=%s\n", kind, runtime, quoteOr(c.ID))
	fmt.Fprintf(&b, "  name:      %s\n  image:     %s\n  digest:    %s\n",
		quoteOr(c.TidyName()), quoteOr(c.Image), quoteOr(c.ImageDigest))
	pid := "unavailable"
	if c.RootPID != 0 {
		pid = strconv.Itoa(c.RootPID)
	}
	fmt.Fprintf(&b, "  root_pid:  %s\n  rootfs:    %s\n", pid, quoteOr(c.RootFS))
	fmt.Fprintf(&b, "  pod:       %s\n  namespace: %s\n  pod_uid:   %s\n",
		quoteOr(c.Labels[container.ContainerLabelKeyPodName]),
		quoteOr(c.Labels[container.ContainerLabelKeyPodNamespace]),
		quoteOr(c.Labels[container.ContainerLabelKeyPodUID]))
	if len(c.Labels) == 0 {
		b.WriteString("  labels:    unavailable\n")
	} else {
		fmt.Fprintf(&b, "  labels:    %q\n", c.Labels)
	}
	return b.String()
}

func run(ctx context.Context, dockerEndpoint, containerdEndpoint, criEndpoint string) error {
	logger, err := zap.NewProduction()
	if err != nil {
		return err
	}

	printEvent := func(kind string) func(*container.Container, string) {
		return func(c *container.Container, runtime string) {
			// One write per block keeps concurrent runtime callbacks readable.
			fmt.Print(describe(kind, runtime, c))
		}
	}
	manager := container.NewManager(logger, dockerEndpoint, containerdEndpoint, criEndpoint, container.Callbacks{
		Started:   printEvent("started"),
		Stopped:   printEvent("stopped"),
		Restarted: printEvent("restarted"),
	})
	if err := manager.Start(ctx); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "watching container events; press Ctrl-C to stop")
	<-ctx.Done()
	return nil
}

func main() {
	dockerEndpoint := flag.String("docker-endpoint", "", "Docker endpoint (empty uses Docker defaults and environment)")
	containerdEndpoint := flag.String("containerd-endpoint", "", "containerd socket path (empty uses the default)")
	criEndpoint := flag.String("cri-endpoint", "", "CRI endpoint (empty probes known sockets)")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "container-monitor accepts flags only")
		flag.Usage()
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, *dockerEndpoint, *containerdEndpoint, *criEndpoint)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
