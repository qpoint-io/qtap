package tls

import (
	"context"
	"errors"
	"sync"
)

// KeyedCoordinator manages per-key operation coordination with version tracking.
// It ensures that only the latest operation for a given key proceeds, cancelling
// any in-flight operations when a newer one starts.
type KeyedCoordinator[K comparable] struct {
	mu       sync.Mutex
	versions map[K]int
	inflight map[K]*inflightOp
}

func NewKeyedCoordinator[K comparable]() *KeyedCoordinator[K] {
	return &KeyedCoordinator[K]{
		versions: make(map[K]int),
		inflight: make(map[K]*inflightOp),
	}
}

// Start begins a new operation for the given key and returns a token.
// The token can be used to check validity and execute the operation.
func (c *KeyedCoordinator[K]) Start(key K) *opToken[K] {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.versions[key]++
	return &opToken[K]{c: c, key: key, version: c.versions[key]}
}

// opToken is an operation token provided by KeyedCoordinator.
type opToken[K comparable] struct {
	c       *KeyedCoordinator[K]
	key     K
	version int
}

// IsValid checks if this token is still the latest for its key (non-consuming).
func (t *opToken[K]) IsValid() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	return t.c.versions[t.key] == t.version
}

// Execute runs fn if the token is still valid, coordinating with other operations.
// If a newer operation starts, this one will be cancelled via context.
func (t *opToken[K]) Execute(ctx context.Context, fn func(context.Context) error) error {
	t.c.mu.Lock()
	if t.c.versions[t.key] != t.version {
		t.c.mu.Unlock()
		return nil // stale token
	}

	if old := t.c.inflight[t.key]; old != nil {
		t.c.mu.Unlock()
		// cancel in-flight op and wait for it to complete
		old.cancel(errOpSuperseded)
		<-old.done
		t.c.mu.Lock()

		// are we still valid?
		if t.c.versions[t.key] != t.version {
			t.c.mu.Unlock()
			return nil // stale token
		}
	}

	ctx, cancel := context.WithCancelCause(ctx)
	op := &inflightOp{done: make(chan struct{}), cancel: cancel}
	t.c.inflight[t.key] = op
	t.c.mu.Unlock()

	err := fn(ctx)
	close(op.done)

	t.c.mu.Lock()
	if t.c.inflight[t.key] == op {
		// clean up
		delete(t.c.inflight, t.key)
		delete(t.c.versions, t.key)
	}
	t.c.mu.Unlock()

	if errors.Is(context.Cause(ctx), errOpSuperseded) {
		return nil
	}
	return err
}

var errOpSuperseded = errors.New("operation superseded")

// inflightOp is an in-flight operation tracked by KeyedCoordinator.
type inflightOp struct {
	// done is a channel that is closed when the operation completes.
	done   chan struct{}
	cancel context.CancelCauseFunc
}
