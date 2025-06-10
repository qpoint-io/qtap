package resolvable

import (
	"context"
	"sync"
	"time"
)

type V[T any] func(ctx context.Context) (T, error)

// Static creates a resolvable that returns the same value every time
func Static[T any](value T) V[T] {
	return func(ctx context.Context) (T, error) {
		return value, nil
	}
}

type value[T any] struct {
	options  *options
	value    T
	err      error
	resolved time.Time
	mutex    sync.Mutex
	fn       V[T]
}

func New[T any](fn V[T], opts ...Option) V[T] {
	o := options{
		now: time.Now,
	}
	for _, opt := range opts {
		opt(&o)
	}
	v := &value[T]{fn: fn, options: &o}
	return v.Resolve
}

type options struct {
	retry    bool
	expiry   time.Duration
	now      func() time.Time
	graceful bool
}

type Option func(*options)

func WithRetry() Option {
	return func(o *options) {
		o.retry = true
	}
}

func WithGraceful() Option {
	return func(o *options) {
		o.graceful = true
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

func (v *value[T]) Resolve(ctx context.Context) (T, error) {
	v.mutex.Lock()
	defer v.mutex.Unlock()

	if !v.expired() {
		return v.value, v.err
	}

	value, err := v.fn(ctx)
	v.err = err // always cache the error itself
	if err == nil {
		// success, cache the value and reset the expiry
		v.value = value
		v.resolved = v.options.now()
		return value, nil
	}

	if !v.options.retry {
		// if not retrying on errors, reset the expiry
		// otherwise keep it expired so we can retry
		v.resolved = v.options.now()
	}

	if v.options.graceful {
		// restore the last known good value
		value = v.value
	} else {
		// persist the errored value
		v.value = value
	}

	// if not graceful, return the resolved result as is
	return value, err
}

func (v *value[T]) expired() bool {
	if v.resolved.IsZero() {
		// if never resolved, pretend it's expired
		return true
	}

	if v.options.expiry == 0 {
		// no expiry
		return false
	}

	// check if the value is expired
	return v.options.now().Sub(v.resolved) > v.options.expiry
}
