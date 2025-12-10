package tls

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestKeyedCoordinator_StartAndIsValid(t *testing.T) {
	c := NewKeyedCoordinator[string]()

	token := c.Start("key1")
	require.True(t, token.IsValid(), "fresh token should be valid")

	// starting new op for same key invalidates the old token
	token2 := c.Start("key1")
	require.False(t, token.IsValid(), "old token should be invalid after new start")
	require.True(t, token2.IsValid(), "new token should be valid")
}

func TestKeyedCoordinator_DifferentKeysIndependent(t *testing.T) {
	c := NewKeyedCoordinator[string]()

	tokenA := c.Start("keyA")
	tokenB := c.Start("keyB")

	require.True(t, tokenA.IsValid(), "tokenA should be valid")
	require.True(t, tokenB.IsValid(), "tokenB should be valid")

	// starting new op for keyA shouldn't affect keyB
	c.Start("keyA")
	require.False(t, tokenA.IsValid(), "old tokenA should be invalid")
	require.True(t, tokenB.IsValid(), "tokenB should still be valid")
}

func TestKeyedCoordinator_ExecuteRunsFunction(t *testing.T) {
	c := NewKeyedCoordinator[int]()
	ctx := t.Context()

	var executed bool
	token := c.Start(42)
	err := token.Execute(ctx, func(ctx context.Context) error {
		executed = true
		return nil
	})

	require.NoError(t, err)
	require.True(t, executed)
}

func TestKeyedCoordinator_ExecuteReturnsError(t *testing.T) {
	c := NewKeyedCoordinator[int]()
	ctx := t.Context()

	expectedErr := errors.New("boom")
	token := c.Start(1)
	err := token.Execute(ctx, func(ctx context.Context) error {
		return expectedErr
	})

	require.ErrorIs(t, err, expectedErr)
}

func TestKeyedCoordinator_StaleTokenDoesNotExecute(t *testing.T) {
	c := NewKeyedCoordinator[string]()
	ctx := t.Context()

	token1 := c.Start("key")
	c.Start("key") // invalidate token1

	var executed bool
	err := token1.Execute(ctx, func(ctx context.Context) error {
		executed = true
		return nil
	})

	require.NoError(t, err)
	require.False(t, executed, "stale token should not execute")
}

func TestKeyedCoordinator_SupersedesInflightOperation(t *testing.T) {
	c := NewKeyedCoordinator[string]()
	ctx := t.Context()

	var (
		op1Started   = make(chan struct{})
		op1Cancelled atomic.Bool
		op1Done      = make(chan struct{})
	)

	token1 := c.Start("key")

	// start op1 in background - it will block until cancelled
	go func() {
		defer close(op1Done)
		_ = token1.Execute(ctx, func(ctx context.Context) error {
			close(op1Started)
			<-ctx.Done()
			op1Cancelled.Store(true)
			return ctx.Err()
		})
	}()

	// wait for op1 to be in-flight
	<-op1Started

	// start op2 which should cancel op1
	token2 := c.Start("key")
	err := token2.Execute(ctx, func(ctx context.Context) error {
		return nil
	})
	require.NoError(t, err)

	// wait for op1 goroutine to finish
	<-op1Done
	require.True(t, op1Cancelled.Load(), "op1 should have been cancelled")
}

func TestKeyedCoordinator_CleansUpAfterExecution(t *testing.T) {
	c := NewKeyedCoordinator[string]()
	ctx := t.Context()

	token := c.Start("key")
	err := token.Execute(ctx, func(ctx context.Context) error {
		return nil
	})
	require.NoError(t, err)

	// after successful execution, maps should be cleaned up
	c.mu.Lock()
	_, hasVersion := c.versions["key"]
	_, hasInflight := c.inflight["key"]
	c.mu.Unlock()

	require.False(t, hasVersion, "version should be cleaned up")
	require.False(t, hasInflight, "inflight should be cleaned up")
}

func TestKeyedCoordinator_TokenSupersededWhileWaiting(t *testing.T) {
	// Tests that when token2 is waiting for token1, and token3 starts,
	// token2 becomes stale and won't execute
	c := NewKeyedCoordinator[string]()
	ctx := t.Context()

	var (
		op1Started  = make(chan struct{})
		op1Release  = make(chan struct{})
		op2Executed atomic.Bool
	)

	token1 := c.Start("key")

	// start op1 - blocks until released
	go func() {
		_ = token1.Execute(ctx, func(ctx context.Context) error {
			close(op1Started)
			<-op1Release
			return nil
		})
	}()
	<-op1Started

	token2 := c.Start("key")

	// start op2 in background - it will wait for op1 to finish
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = token2.Execute(ctx, func(ctx context.Context) error {
			op2Executed.Store(true)
			return nil
		})
	}()

	// give op2 time to start waiting
	time.Sleep(10 * time.Millisecond)

	// op3 supersedes op2 while op2 is waiting for op1
	c.Start("key") // just invalidate token2, don't need to execute

	// release op1
	close(op1Release)
	wg.Wait()

	// token2 should not have executed since token3 superseded it while waiting
	require.False(t, op2Executed.Load(), "op2 should not execute (superseded while waiting)")
}

func TestKeyedCoordinator_ConcurrentDifferentKeys(t *testing.T) {
	c := NewKeyedCoordinator[int]()
	ctx := t.Context()

	const numKeys = 10
	var wg sync.WaitGroup
	executed := make([]atomic.Bool, numKeys)

	for i := range numKeys {
		wg.Add(1)
		go func(key int) {
			defer wg.Done()
			token := c.Start(key)
			_ = token.Execute(ctx, func(ctx context.Context) error {
				executed[key].Store(true)
				return nil
			})
		}(i)
	}

	wg.Wait()

	for i := range numKeys {
		require.True(t, executed[i].Load(), "key %d should have executed", i)
	}
}

func TestKeyedCoordinator_ParentContextCancellation(t *testing.T) {
	c := NewKeyedCoordinator[string]()
	ctx, cancel := context.WithCancel(t.Context())

	var (
		started     = make(chan struct{})
		returnedErr error
	)

	token := c.Start("key")
	done := make(chan struct{})
	go func() {
		defer close(done)
		returnedErr = token.Execute(ctx, func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
	}()

	<-started
	cancel()
	<-done

	// parent context cancellation should propagate the error
	require.ErrorIs(t, returnedErr, context.Canceled)
}
