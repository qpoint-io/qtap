package e2e

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/testcontainers/testcontainers-go"
)

type Language string

const (
	Python Language = "python"
	Ruby   Language = "ruby"
	PHP    Language = "php"
	Go     Language = "go"
	Java   Language = "java"
	NodeJS Language = "nodejs"
)

type Container struct {
	testcontainers.Container
	resultCh chan ContainerResult
}

func (c *Container) WaitForExit(ctx context.Context) (*ContainerResult, error) {
	select {
	case result := <-c.resultCh:
		return &result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ContainerResult captures the output from a container run
type ContainerResult struct {
	stdout   *strings.Builder
	stderr   *strings.Builder
	combined *strings.Builder

	ExitCode int
	Error    error
}

// Combined is both the stdout and stderr in order of arrival from the container.
func (c *ContainerResult) Combined() string {
	if c.combined == nil {
		return ""
	}

	return c.combined.String()
}

func (c *ContainerResult) Stdout() string {
	if c.stdout == nil {
		return ""
	}

	return c.stdout.String()
}

func (c *ContainerResult) Stderr() string {
	if c.stderr == nil {
		return ""
	}

	return c.stderr.String()
}

// Accept is required to satisfy the testcontainers Log aggregator interface
func (c *ContainerResult) Accept(log testcontainers.Log) {
	var w io.Writer
	switch log.LogType {
	case "STDOUT":
		if c.stdout == nil {
			c.stdout = &strings.Builder{}
		}
		w = c.stdout
	case "STDERR":
		if c.stderr == nil {
			c.stderr = &strings.Builder{}
		}
		w = c.stderr
	default:
		w = io.Discard
	}

	if c.combined == nil {
		c.combined = &strings.Builder{}
	}

	_, _ = c.combined.Write(log.Content)
	_, _ = c.combined.Write([]byte("\n")) // TODO(Jon): confirm if this is required
	_, _ = w.Write(log.Content)
	_, _ = w.Write([]byte("\n")) // TODO(Jon): confirm if this is required
}

// ReceivedRequest represents a request received by the test server
type ReceivedRequest struct {
	Method     string
	Path       string
	Headers    http.Header
	Body       string
	StatusCode int
	Latency    time.Duration
	Proto      string // "HTTP/1.0", "HTTP/1.1", "HTTP/2.0"
}
