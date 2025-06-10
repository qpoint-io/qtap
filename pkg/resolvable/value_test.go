package resolvable

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValue_Resolve(t *testing.T) {
	ctx := context.Background()
	t.Run("simple", func(t *testing.T) {
		v := New(func(ctx context.Context) (int, error) {
			return 1, nil
		})
		value, err := v(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, value)
	})

	t.Run("retryable", func(t *testing.T) {
		var count int
		v := New(func(ctx context.Context) (int, error) {
			count++
			if count < 3 {
				return 0, errors.New("try again")
			}
			return count, nil
		}, WithRetryable())

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
	})

	t.Run("expiry", func(t *testing.T) {
		now := time.Now()
		var count int
		v := New(func(ctx context.Context) (int, error) {
			count++
			return count, nil
		}, WithExpiry(2*time.Second), WithNow(func() time.Time { return now }))

		value, err := v(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, value)

		// still not expired
		now = now.Add(time.Second)
		value, err = v(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, value)

		// expired
		now = now.Add(2 * time.Second)
		value, err = v(ctx)
		require.NoError(t, err)
		require.Equal(t, 2, value)
	})
}
