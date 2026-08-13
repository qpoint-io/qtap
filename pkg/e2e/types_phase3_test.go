package e2e

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

func TestContainerResultLogsSupportConcurrentReadersAndWriters(t *testing.T) {
	result := newContainerResult()
	var workers sync.WaitGroup
	for range 10 {
		workers.Add(2)
		go func() {
			defer workers.Done()
			result.Accept(testcontainers.Log{LogType: "STDOUT", Content: []byte("line")})
		}()
		go func() {
			defer workers.Done()
			_ = result.Stdout()
			_ = result.Stderr()
			_ = result.Combined()
		}()
	}
	workers.Wait()

	require.Contains(t, result.Stdout(), "line")
	require.Contains(t, result.Combined(), "line")
}
