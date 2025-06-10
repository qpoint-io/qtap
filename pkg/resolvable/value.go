package resolvable

import (
	"context"
	"sync"
	"time"
)

type V[T any] func(ctx context.Context) (T, error)

type Value[T any] struct {
	options  *options
	value    T
	err      error
	resolved time.Time
	mutex    sync.Mutex
	fn       V[T]
}

type options struct {
	retryable bool
	expiry    time.Duration
	now       func() time.Time
}

type Option func(*options)

func WithRetryable() Option {
	return func(o *options) {
		o.retryable = true
	}
}

func WithExpiry(expiry time.Duration) Option {
	return func(o *options) {
		o.expiry = expiry
	}
}

func WithNow(now func() time.Time) Option {
	return func(o *options) {
		o.now = now
	}
}

func New[T any](fn V[T], opts ...Option) V[T] {
	o := options{
		now: time.Now,
	}
	for _, opt := range opts {
		opt(&o)
	}
	v := &Value[T]{fn: fn, options: &o}
	return v.Resolve
}

func (v *Value[T]) Resolve(ctx context.Context) (T, error) {
	v.mutex.Lock()
	defer v.mutex.Unlock()

	if !v.resolved.IsZero() && !v.expired() {
		return v.value, v.err
	}

	v.value, v.err = v.fn(ctx)
	if !v.options.retryable || v.err == nil {
		// if not retryable or no error, this result is final
		v.resolved = v.options.now()
	}
	return v.value, v.err
}

func (v *Value[T]) expired() bool {
	if v.options.expiry == 0 || v.resolved.IsZero() {
		return false
	}
	return v.options.now().Sub(v.resolved) > v.options.expiry
}
