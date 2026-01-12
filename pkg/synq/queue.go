package synq

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrQueueClosed   = errors.New("queue closed")
	ErrQueueDraining = errors.New("queue draining")
)

// Op represents a queue operation type for notifications
type Op int

const (
	Pushed Op = iota // Item was pushed to queue
	Popped           // Item was popped from queue
)

// Prioritized is an interface for items that can be prioritized in the queue
type Prioritized interface {
	QueuePriority() int
}

type node struct {
	value any
	next  *node
}

type Queue struct {
	ctx    context.Context
	cancel context.CancelFunc

	head     *node
	tail     *node
	mu       sync.Mutex
	closed   atomic.Bool
	draining atomic.Bool
	count    int64

	notify chan Op // signals queue operations (Pushed/Popped)
}

func NewQueue(ctx context.Context) *Queue {
	qCtx, cancel := context.WithCancel(ctx)
	return &Queue{
		ctx:    qCtx,
		cancel: cancel,
		notify: make(chan Op, 1),
	}
}

// TODO(Jon): Should we use generics here?
func (q *Queue) Push(values ...any) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.draining.Load() {
		return ErrQueueDraining
	}

	if q.closed.Load() {
		return ErrQueueClosed
	}

	for _, v := range values {
		q.push(v)
	}

	// Signal waiting consumers
	select {
	case q.notify <- Pushed:
	default:
	}

	return nil
}

func (q *Queue) push(value any) {
	if p, ok := value.(Prioritized); ok {
		q.insertWithPriority(p)
	} else {
		q.append(value)
	}
}

func (q *Queue) insertWithPriority(p Prioritized) {
	newNode := &node{value: p}
	priority := p.QueuePriority()

	switch {
	case q.head == nil:
		// Case 1: Empty queue
		q.head = newNode
		q.tail = newNode
	case priority < getPriority(q.head.value):
		// Case 2: New node has highest priority
		newNode.next = q.head
		q.head = newNode
	default:
		// Case 3: Insert node in the middle or at the end
		current := q.head
		for current.next != nil && priority >= getPriority(current.next.value) {
			current = current.next
		}
		newNode.next = current.next
		current.next = newNode
		if newNode.next == nil {
			q.tail = newNode
		}
	}

	atomic.AddInt64(&q.count, 1)
}

func (q *Queue) append(value any) {
	newNode := &node{value: value}
	if q.tail == nil {
		q.head = newNode
		q.tail = newNode
	} else {
		q.tail.next = newNode
		q.tail = newNode
	}
	atomic.AddInt64(&q.count, 1)
}

func getPriority(value any) int {
	if p, ok := value.(Prioritized); ok {
		return p.QueuePriority()
	}
	return math.MaxInt // Non-prioritized items have the lowest priority
}

func (q *Queue) pop() any {
	if q.head == nil {
		return nil
	}

	value := q.head.value
	q.head = q.head.next
	if q.head == nil {
		q.tail = nil
	}
	atomic.AddInt64(&q.count, -1)

	// Signal drain waiters that an item was popped
	if q.draining.Load() {
		select {
		case q.notify <- Popped:
		default:
		}
	}

	return value
}

func (q *Queue) Pop() any {
	q.mu.Lock()
	defer q.mu.Unlock()

	return q.pop()
}

// PopN pops n items from the queue.
func (q *Queue) PopN(n int) []any {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.Len() < n {
		n = q.Len()
	}

	values := make([]any, n)
	for i := range values {
		values[i] = q.pop()
	}
	return values
}

// Next will block while the queue is empty and not closed.
// TODO: return nil value
func (q *Queue) Next() (any, bool) {
	for {
		q.mu.Lock()
		if q.head != nil {
			value := q.head.value
			q.head = q.head.next
			if q.head == nil {
				q.tail = nil
			}
			atomic.AddInt64(&q.count, -1)

			// Signal drain waiters that an item was popped
			if q.draining.Load() {
				select {
				case q.notify <- Popped:
				default:
				}
			}

			q.mu.Unlock()
			return value, true
		}
		if q.closed.Load() {
			q.mu.Unlock()
			return nil, false
		}
		q.mu.Unlock()

		// Wait for new items or context cancellation
		select {
		case <-q.notify:
			// Any operation (Pushed/Popped) means we should check the queue again
			continue
		case <-q.ctx.Done():
			return nil, false
		}
	}
}

func (q *Queue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	remainingCount := q.Len()

	q.closed.Store(true)
	q.head = nil
	q.tail = nil
	atomic.StoreInt64(&q.count, 0)
	q.cancel() // Cancel the context

	if remainingCount > 0 {
		return fmt.Errorf("queue was not empty when closed: %d", remainingCount)
	}

	return nil
}

// Drain drains the queue and returns when the queue is empty or
// the context is canceled. Drain is permanent.
func (q *Queue) Drain(d time.Duration) error {
	q.draining.Store(true)

	ctx, cancel := context.WithTimeout(q.ctx, d)
	defer cancel()

	for {
		q.mu.Lock()
		if q.head == nil || q.closed.Load() {
			q.mu.Unlock()
			return nil
		}
		q.mu.Unlock()

		// Wait for any operation or timeout
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-q.notify:
			// Any operation means we should check if queue is empty
			continue
		}
	}
}

func (q *Queue) IsClosed() bool {
	return q.closed.Load()
}

func (q *Queue) IsDraining() bool {
	return q.draining.Load()
}

func (q *Queue) Len() int {
	return int(atomic.LoadInt64(&q.count))
}

func (q *Queue) IsEmpty() bool {
	return q.Len() == 0
}
