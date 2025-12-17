package synq

import (
	"fmt"
	"sync"
)

// SingleFlight implements a generic singleflight with retry-on-failure behavior.
// It ensures that only one execution of a function for a given key is in-flight at a time.
//
// adapted from golang.org/x/sync/singleflight
type SingleFlight[K comparable, V any] struct {
	mu sync.Mutex
	m  map[K]*call[V]
}

// Len returns the number of in-flight requests.
func (g *SingleFlight[K, V]) Len() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.m)
}

type call[V any] struct {
	wg  sync.WaitGroup
	val V
	err error
}

// Do executes the function for the given key.
// If a call is already in progress, it waits for it to complete.
// If the leader succeeds (err == nil), the result is returned to all waiters.
// If the leader fails (err != nil), the leader returns the error, but waiters
// will automatically retry (becoming the new leader) to ensure independent attempts.
func (g *SingleFlight[K, V]) Do(key K, fn func() (V, error)) (V, error) {
	for {
		g.mu.Lock()
		if g.m == nil {
			g.m = make(map[K]*call[V])
		}

		if c, ok := g.m[key]; ok {
			g.mu.Unlock()
			c.wg.Wait()

			// If success, share the result
			if c.err == nil {
				return c.val, nil
			}

			// If failure, retry (loop back to start)
			// This ensures that waiting calls get a chance to execute
			// if the previous leader failed.
			continue
		}

		c := new(call[V])
		c.wg.Add(1)
		g.m[key] = c
		g.mu.Unlock()

		c.val, c.err = panicSafe(fn)

		g.mu.Lock()
		delete(g.m, key)
		g.mu.Unlock()
		c.wg.Done()

		return c.val, c.err
	}
}

// panicSafe executes a function and converts panics into errors
func panicSafe[T any](fn func() (T, error)) (retVal T, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("panic: %v", r)
		}
	}()
	retVal, retErr = fn()
	return
}
