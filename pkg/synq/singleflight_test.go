package synq

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSingleFlight_Do_Deduplicates(t *testing.T) {
	var (
		g     SingleFlight[string, string]
		calls int32
		wg    sync.WaitGroup
	)

	// We launch 10 concurrent calls.
	// The function takes 50ms to complete.
	// We expect 'calls' to be 1, and all 10 goroutines to get the same result.
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			val, err := g.Do("key", func() (string, error) {
				atomic.AddInt32(&calls, 1)
				time.Sleep(50 * time.Millisecond)
				return "success", nil
			})
			assert.NoError(t, err)
			assert.Equal(t, "success", val)
		}()
	}

	wg.Wait()
	assert.Equal(t, int32(1), calls, "expected exactly 1 actual call")
	assert.Equal(t, 0, g.Len(), "expected map to be empty after completion")
}

func TestSingleFlight_Do_RetriesOnError(t *testing.T) {
	var (
		g     SingleFlight[string, string]
		calls int32
	)

	// We want to verify that we RETRY on error.
	// If we have 3 concurrent callers, and the first 2 attempts fail,
	// we expect 3 actual calls to the function (the 3rd one succeeding).
	// This proves that waiters don't just return the leader's error,
	// but pick up the torch and try again.
	fn := func() (string, error) {
		c := atomic.AddInt32(&calls, 1)

		// Hold the lock for a bit to ensure the other goroutines
		// have time to enter Do() and wait on us.
		time.Sleep(50 * time.Millisecond)

		if c <= 2 {
			return "", fmt.Errorf("fail %d", c)
		}
		return "success", nil
	}

	var wg sync.WaitGroup
	wg.Add(3)

	for range 3 {
		go func() {
			defer wg.Done()
			val, err := g.Do("key", fn)

			if err != nil {
				// If it failed, it means this goroutine was a leader that failed.
				// This is expected behavior for 2 of the 3 calls.
				assert.Contains(t, err.Error(), "fail")
			} else {
				// If it succeeded, it must be the success result
				assert.Equal(t, "success", val)
			}
		}()
	}

	wg.Wait()
	assert.Equal(t, int32(3), calls, "expected 3 calls (2 failures + 1 success)")
	assert.Equal(t, 0, g.Len(), "expected map to be empty after completion")
}

func TestSingleFlight_Do_PanicSafety(t *testing.T) {
	var g SingleFlight[string, string]

	// This function panics
	fn := func() (string, error) {
		panic("oops")
	}

	val, err := g.Do("key", fn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "panic: oops")
	assert.Empty(t, val)
	assert.Equal(t, 0, g.Len(), "expected map to be empty after panic")

	// Ensure the key was cleaned up so we can use it again
	val, err = g.Do("key", func() (string, error) {
		return "recovered", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "recovered", val)
}
