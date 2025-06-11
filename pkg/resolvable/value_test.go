package resolvable

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSimple(t *testing.T) {
	ctx := context.Background()
	var called bool
	v := New(func(ctx context.Context) (int, error) {
		if called {
			return 0, errors.New("called twice")
		}
		called = true
		return 1, nil
	})
	value, err := v(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, value)

	// by default, the value never expires
	value, err = v(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, value)
}

func TestRetry(t *testing.T) {
	ctx := context.Background()

	var count int
	v := New(func(ctx context.Context) (int, error) {
		count++
		if count < 3 { // return an error twice and then succeed
			return 0, errors.New("try again")
		}
		return count, nil
	}, WithRetry())

	value, err := v(ctx)
	require.EqualError(t, err, "try again")
	require.Equal(t, 0, value)

	value, err = v(ctx)
	require.EqualError(t, err, "try again")
	require.Equal(t, 0, value)

	value, err = v(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, value)

	// fn should not be called again
	value, err = v(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, value)
}

func TestGraceful(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	var (
		count    int
		retryErr error
	)
	v := New(
		func(ctx context.Context) (int, error) {
			count++
			return count, retryErr
		},
		WithExpiry(time.Second),
		WithGraceful(),
		WithNow(func() time.Time { return now }),
	)

	// first call, no error
	value, err := v(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, value)

	// expire it and return an error
	now = now.Add(2 * time.Second)
	retryErr = errors.New("try again")
	value, err = v(ctx)
	require.EqualError(t, err, "try again")
	require.Equal(t, 1, value) // last known good value

	// expire it and return success
	now = now.Add(2 * time.Second)
	retryErr = nil
	value, err = v(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, value)
}

func TestExpiry(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	var (
		count      int
		resolveErr error
	)
	v := New(
		func(ctx context.Context) (int, error) {
			count++
			return count, resolveErr
		},
		WithExpiry(2*time.Second),
		WithNow(func() time.Time { return now }),
	).WithContext(ctx)

	// first call, no error
	value, err := v()
	require.NoError(t, err)
	require.Equal(t, 1, value)

	// still not expired
	now = now.Add(time.Second)
	value, err = v()
	require.NoError(t, err)
	require.Equal(t, 1, value)

	// expired, return an error
	now = now.Add(2 * time.Second)
	resolveErr = errors.New("try again")
	value, err = v()
	require.EqualError(t, err, "try again")
	require.Equal(t, 2, value)

	// errors are cached unless retry is enabled
	value, err = v()
	require.EqualError(t, err, "try again")
	require.Equal(t, 2, value)
}

func TestExpiry_Retry(t *testing.T) {
	now := time.Now()
	var (
		count      int
		resolveErr error
	)
	v := New(
		func(ctx context.Context) (int, error) {
			count++
			return count, resolveErr
		},
		WithRetry(),
		WithExpiry(2*time.Second),
		WithNow(func() time.Time { return now }),
	).WithBgContext()

	// first call, no error
	value, err := v()
	require.NoError(t, err)
	require.Equal(t, 1, value)

	// still not expired
	now = now.Add(time.Second)
	value, err = v()
	require.NoError(t, err)
	require.Equal(t, 1, value)

	// expired, return an error
	now = now.Add(2 * time.Second)
	resolveErr = errors.New("try again")
	value, err = v()
	require.EqualError(t, err, "try again")
	require.Equal(t, 2, value)

	// errors are not cached because we set WithRetry()
	// it is resolved again even though the clock did not advance
	value, err = v()
	require.EqualError(t, err, "try again")
	require.Equal(t, 3, value)
}
