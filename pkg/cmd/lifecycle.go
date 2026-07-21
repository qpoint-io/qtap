package cmd

import (
	"context"
	"errors"
	"sync"
	"time"
)

const shutdownTimeout = 30 * time.Second

type cleanupFunc func(context.Context) error

type shutdownBudget struct {
	once    sync.Once
	timeout time.Duration
	ctx     context.Context
	cancel  context.CancelFunc
}

func newShutdownBudget(timeout time.Duration) *shutdownBudget {
	return &shutdownBudget{timeout: timeout}
}

func (b *shutdownBudget) Context() context.Context {
	b.once.Do(func() {
		b.ctx, b.cancel = context.WithTimeout(context.Background(), b.timeout)
	})
	return b.ctx
}

func (b *shutdownBudget) Close() {
	if b.cancel != nil {
		b.cancel()
	}
}

func runCleanup(ctx context.Context, cleanup func() error) error {
	done := make(chan error, 1)
	go func() { done <- cleanup() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return errors.Join(errors.New("shutdown deadline exceeded"), ctx.Err())
	}
}

func runCleanups(ctx context.Context, cleanups []cleanupFunc) error {
	var cleanupErr error
	for i := len(cleanups) - 1; i >= 0; i-- {
		cleanupErr = errors.Join(cleanupErr, cleanups[i](ctx))
	}
	return cleanupErr
}
