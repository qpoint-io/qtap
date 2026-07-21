package cmd

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunCleanupReturnsFailure(t *testing.T) {
	want := errors.New("cleanup failed")
	require.ErrorIs(t, runCleanup(t.Context(), func() error { return want }), want)
}

func TestRunCleanupHonorsSharedDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	err := runCleanup(ctx, func() error {
		select {}
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestShutdownBudgetStartsOnFirstCleanup(t *testing.T) {
	budget := newShutdownBudget(20 * time.Millisecond)
	t.Cleanup(budget.Close)
	time.Sleep(25 * time.Millisecond)
	require.NoError(t, budget.Context().Err())
}

func TestRunCleanupsJoinsErrorsInReverseOrder(t *testing.T) {
	firstErr := errors.New("first cleanup failed")
	secondErr := errors.New("second cleanup failed")
	var order []string
	cleanups := []cleanupFunc{
		func(context.Context) error {
			order = append(order, "first")
			return firstErr
		},
		func(context.Context) error {
			order = append(order, "second")
			return secondErr
		},
	}

	err := runCleanups(t.Context(), cleanups)
	require.ErrorIs(t, err, firstErr)
	require.ErrorIs(t, err, secondErr)
	require.Equal(t, []string{"second", "first"}, order)
}

func TestRunCleanupsPassesContext(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(t.Context(), contextKey{}, "value")

	err := runCleanups(ctx, []cleanupFunc{func(got context.Context) error {
		require.Equal(t, "value", got.Value(contextKey{}))
		return nil
	}})
	require.NoError(t, err)
}
