package e2e

import (
	"context"
	"strings"
	"sync"

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
	resultCh   chan ContainerResult
	processPID chan int
	// Request  *HTTPRequest
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
	logData *containerResultLogData

	ExitCode int
	Error    error
}

type containerResultLogData struct {
	mu       sync.RWMutex
	stdout   strings.Builder
	stderr   strings.Builder
	combined strings.Builder
}

func newContainerResult() ContainerResult {
	return ContainerResult{logData: &containerResultLogData{}}
}

var containerResultLogDataMu sync.Mutex

func (c *ContainerResult) logs() *containerResultLogData {
	containerResultLogDataMu.Lock()
	defer containerResultLogDataMu.Unlock()
	if c.logData == nil {
		c.logData = &containerResultLogData{}
	}
	return c.logData
}

// Combined is both the stdout and stderr in order of arrival from the container.
func (c *ContainerResult) Combined() string {
	logs := c.logs()
	logs.mu.RLock()
	defer logs.mu.RUnlock()
	return logs.combined.String()
}

func (c *ContainerResult) Stdout() string {
	logs := c.logs()
	logs.mu.RLock()
	defer logs.mu.RUnlock()
	return logs.stdout.String()
}

func (c *ContainerResult) Stderr() string {
	logs := c.logs()
	logs.mu.RLock()
	defer logs.mu.RUnlock()
	return logs.stderr.String()
}

// Accept is required to satisfy the testcontainers Log aggregator interface
func (c *ContainerResult) Accept(log testcontainers.Log) {
	logs := c.logs()
	logs.mu.Lock()
	defer logs.mu.Unlock()

	_, _ = logs.combined.Write(log.Content)
	_ = logs.combined.WriteByte('\n')
	switch log.LogType {
	case "STDOUT":
		_, _ = logs.stdout.Write(log.Content)
		_ = logs.stdout.WriteByte('\n')
	case "STDERR":
		_, _ = logs.stderr.Write(log.Content)
		_ = logs.stderr.WriteByte('\n')
	}
}
