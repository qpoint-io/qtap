package synq

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestQueue_PushPop(t *testing.T) {
	tests := []struct {
		name     string
		pushOps  [][]any
		popOps   int
		expected []any
	}{
		{
			name:     "Push and pop single item",
			pushOps:  [][]any{{1}},
			popOps:   1,
			expected: []any{1},
		},
		{
			name:     "Push multiple items, pop all",
			pushOps:  [][]any{{1, 2, 3}},
			popOps:   3,
			expected: []any{1, 2, 3},
		},
		{
			name:     "Push in multiple operations, pop all",
			pushOps:  [][]any{{1}, {2}, {3}},
			popOps:   3,
			expected: []any{1, 2, 3},
		},
		{
			name:     "Push multiple, pop some",
			pushOps:  [][]any{{1, 2, 3, 4, 5}},
			popOps:   3,
			expected: []any{1, 2, 3},
		},
		{
			name:     "Pop more than pushed",
			pushOps:  [][]any{{1, 2}},
			popOps:   3,
			expected: []any{1, 2, nil},
		},
		{
			name:     "Push different types",
			pushOps:  [][]any{{1, "two", 3.0, true}},
			popOps:   4,
			expected: []any{1, "two", 3.0, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			q := NewQueue(ctx)

			// Perform push operations
			for _, pushOp := range tt.pushOps {
				err := q.Push(pushOp...)
				if err != nil {
					t.Fatalf("Unexpected error during Push: %v", err)
				}
			}

			// Perform pop operations and collect results
			results := make([]any, 0, tt.popOps)
			for range tt.popOps {
				results = append(results, q.Pop())
			}

			// Check if results match expected
			if !reflect.DeepEqual(results, tt.expected) {
				t.Errorf("Push/Pop operations resulted in %v, expected %v", results, tt.expected)
			}

			// Check if Len() is correct after operations
			expectedLen := 0
			for _, pushOp := range tt.pushOps {
				expectedLen += len(pushOp)
			}
			expectedLen -= tt.popOps
			if expectedLen < 0 {
				expectedLen = 0
			}
			if q.Len() != expectedLen {
				t.Errorf("Queue length is %d, expected %d", q.Len(), expectedLen)
			}

			// Check IsEmpty() is correct after operations
			isEmpty := q.IsEmpty()
			expectedIsEmpty := expectedLen == 0
			if isEmpty != expectedIsEmpty {
				t.Errorf("Queue IsEmpty() is %v, expected %v", isEmpty, expectedIsEmpty)
			}
		})
	}
}

func TestQueue_Next(t *testing.T) {
	tests := []struct {
		name string
		ops  []func(*Queue, *sync.WaitGroup, chan any)
		want []any
	}{
		{
			name: "Next on empty queue",
			ops: []func(*Queue, *sync.WaitGroup, chan any){
				func(q *Queue, wg *sync.WaitGroup, ch chan any) {
					wg.Go(func() {
						v, ok := q.Next()
						ch <- v
						ch <- ok
					})
				},
				func(q *Queue, wg *sync.WaitGroup, ch chan any) {
					time.Sleep(100 * time.Millisecond)
					err := q.Push(1)
					if err != nil {
						ch <- fmt.Sprintf("Push error: %v", err)
					}
				},
			},
			want: []any{1, true},
		},
		{
			name: "Next on non-empty queue",
			ops: []func(*Queue, *sync.WaitGroup, chan any){
				func(q *Queue, wg *sync.WaitGroup, ch chan any) {
					err := q.Push(1, 2, 3)
					if err != nil {
						ch <- fmt.Sprintf("Push error: %v", err)
					}
				},
				func(q *Queue, wg *sync.WaitGroup, ch chan any) {
					wg.Go(func() {
						for range 3 {
							v, ok := q.Next()
							ch <- v
							ch <- ok
						}
					})
				},
			},
			want: []any{1, true, 2, true, 3, true},
		},
		{
			name: "Next after Destroy",
			ops: []func(*Queue, *sync.WaitGroup, chan any){
				func(q *Queue, wg *sync.WaitGroup, ch chan any) {
					wg.Go(func() {
						v, ok := q.Next()
						ch <- v
						ch <- ok
					})
				},
				func(q *Queue, wg *sync.WaitGroup, ch chan any) {
					time.Sleep(100 * time.Millisecond)
					q.Close()
				},
			},
			want: []any{nil, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			q := NewQueue(ctx)
			var wg sync.WaitGroup
			ch := make(chan any, len(tt.want))

			for _, op := range tt.ops {
				op(q, &wg, ch)
			}

			wg.Wait()
			close(ch)

			got := make([]any, 0, len(tt.want))
			for v := range ch {
				got = append(got, v)
			}

			if len(got) != len(tt.want) {
				t.Errorf("Got %d results, want %d", len(got), len(tt.want))
			}

			for i := 0; i < len(got) && i < len(tt.want); i++ {
				if got[i] != tt.want[i] {
					t.Errorf("Result %d: got %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// prioritizedItem is a simple struct that implements the Prioritized interface
type prioritizedItem struct {
	value    string
	priority int
}

func (p prioritizedItem) QueuePriority() int {
	return p.priority
}

func TestPrioritizedQueue(t *testing.T) {
	tests := []struct {
		name     string
		pushOps  [][]any
		popCount int
		expected []any
	}{
		{
			name: "Push prioritized items in reverse order",
			pushOps: [][]any{{
				prioritizedItem{"low", 3},
				prioritizedItem{"medium", 2},
				prioritizedItem{"high", 1},
			}},
			popCount: 3,
			expected: []any{
				prioritizedItem{"high", 1},
				prioritizedItem{"medium", 2},
				prioritizedItem{"low", 3},
			},
		},
		{
			name: "Mix of prioritized and non-prioritized items",
			pushOps: [][]any{{
				"non-prioritized1",
				prioritizedItem{"medium", 2},
				"non-prioritized2",
				prioritizedItem{"high", 1},
				prioritizedItem{"low", 3},
			}},
			popCount: 5,
			expected: []any{
				prioritizedItem{"high", 1},
				prioritizedItem{"medium", 2},
				prioritizedItem{"low", 3},
				"non-prioritized1",
				"non-prioritized2",
			},
		},
		{
			name: "Same priority items maintain FIFO order",
			pushOps: [][]any{{
				prioritizedItem{"first", 1},
				prioritizedItem{"second", 1},
				prioritizedItem{"third", 1},
			}},
			popCount: 3,
			expected: []any{
				prioritizedItem{"first", 1},
				prioritizedItem{"second", 1},
				prioritizedItem{"third", 1},
			},
		},
		{
			name: "Complex priority scenario with multiple pushes",
			pushOps: [][]any{
				{prioritizedItem{"medium1", 2}, "non-prioritized1"},
				{prioritizedItem{"highest", 1}, prioritizedItem{"low", 3}},
				{"non-prioritized2", prioritizedItem{"high", 1}, prioritizedItem{"medium2", 2}},
			},
			popCount: 7,
			expected: []any{
				prioritizedItem{"highest", 1},
				prioritizedItem{"high", 1},
				prioritizedItem{"medium1", 2},
				prioritizedItem{"medium2", 2},
				prioritizedItem{"low", 3},
				"non-prioritized1",
				"non-prioritized2",
			},
		},
		{
			name: "Non-prioritized items between prioritized items",
			pushOps: [][]any{{
				prioritizedItem{"low", 3},
				"non-prioritized1",
				prioritizedItem{"medium", 2},
				"non-prioritized2",
				prioritizedItem{"high", 1},
			}},
			popCount: 5,
			expected: []any{
				prioritizedItem{"high", 1},
				prioritizedItem{"medium", 2},
				prioritizedItem{"low", 3},
				"non-prioritized1",
				"non-prioritized2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			q := NewQueue(ctx)

			// Push all items
			for _, pushOp := range tt.pushOps {
				err := q.Push(pushOp...)
				if err != nil {
					t.Fatalf("Unexpected error during Push: %v", err)
				}
			}

			// Pop items and collect results
			results := make([]any, 0, tt.popCount)
			for range tt.popCount {
				results = append(results, q.Pop())
			}

			// Check if results match expected
			if !reflect.DeepEqual(results, tt.expected) {
				t.Errorf("Push/Pop operations resulted in %v, expected %v", results, tt.expected)
			}

			// Check if Len() is correct after operations
			expectedLen := 0
			for _, pushOp := range tt.pushOps {
				expectedLen += len(pushOp)
			}
			expectedLen -= tt.popCount
			if expectedLen < 0 {
				expectedLen = 0
			}
			if q.Len() != expectedLen {
				t.Errorf("Queue length is %d, expected %d", q.Len(), expectedLen)
			}

			// Check IsEmpty() is correct after operations
			isEmpty := q.IsEmpty()
			expectedIsEmpty := expectedLen == 0
			if isEmpty != expectedIsEmpty {
				t.Errorf("Queue IsEmpty() is %v, expected %v", isEmpty, expectedIsEmpty)
			}
		})
	}
}

func TestQueue_Drain(t *testing.T) {
	t.Run("Drain empty queue", func(t *testing.T) {
		ctx := t.Context()
		q := NewQueue(ctx)

		err := q.Drain(100 * time.Millisecond)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
	})

	t.Run("Drain non-empty queue", func(t *testing.T) {
		ctx := t.Context()
		q := NewQueue(ctx)
		err := q.Push(1, 2, 3)
		if err != nil {
			t.Fatalf("Unexpected error during Push: %v", err)
		}

		var wg sync.WaitGroup
		wg.Go(func() {
			for range 3 {
				time.Sleep(50 * time.Millisecond)
				q.Pop()
			}
		})

		err = q.Drain(500 * time.Millisecond)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		wg.Wait()

		// Add new checks for permanent drain state
		if !q.IsDraining() {
			t.Error("Queue should be in draining state")
		}

		// Verify that new pushes are rejected
		err = q.Push(4)
		if !errors.Is(err, ErrQueueDraining) {
			t.Errorf("Expected ErrQueueDraining, got: %v", err)
		}

		if !q.IsEmpty() {
			t.Error("Queue should be empty after draining")
		}
	})

	t.Run("Drain with timeout", func(t *testing.T) {
		ctx := t.Context()
		q := NewQueue(ctx)
		err := q.Push(1, 2, 3)
		if err != nil {
			t.Fatalf("Unexpected error during Push: %v", err)
		}

		err = q.Drain(100 * time.Millisecond)
		if err == nil {
			t.Errorf("Expected timeout error, got nil")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Expected DeadlineExceeded error, got: %v", err)
		}

		// Add new checks for permanent drain state
		if !q.IsDraining() {
			t.Error("Queue should be in draining state even after timeout")
		}

		// Verify that new pushes are rejected
		err = q.Push(4)
		if !errors.Is(err, ErrQueueDraining) {
			t.Errorf("Expected ErrQueueDraining, got: %v", err)
		}
	})

	t.Run("Drain with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		q := NewQueue(ctx)
		err := q.Push(1, 2, 3)
		if err != nil {
			t.Fatalf("Unexpected error during Push: %v", err)
		}

		// Cancel the context before draining
		cancel()

		err = q.Drain(1 * time.Second)
		if err == nil {
			t.Errorf("Expected context cancellation error, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected Canceled error, got: %v", err)
		}

		if !q.IsDraining() {
			t.Error("Queue should be in draining state even after context cancellation")
		}
	})
}

func TestQueue_PopN(t *testing.T) {
	tests := []struct {
		name    string
		items   []any // items to push into queue
		n       int   // number of items to pop
		want    []any // expected items
		wantLen int   // expected queue length after pop
	}{
		{
			name:    "empty queue",
			items:   []any{},
			n:       5,
			want:    []any{},
			wantLen: 0,
		},
		{
			name:    "pop all items",
			items:   []any{1, 2, 3},
			n:       3,
			want:    []any{1, 2, 3},
			wantLen: 0,
		},
		{
			name:    "pop partial items",
			items:   []any{1, 2, 3, 4, 5},
			n:       3,
			want:    []any{1, 2, 3},
			wantLen: 2,
		},
		{
			name:    "pop more than available",
			items:   []any{1, 2},
			n:       5,
			want:    []any{1, 2},
			wantLen: 0,
		},
		{
			name:    "pop mixed types",
			items:   []any{1, "two", 3.0, true},
			n:       2,
			want:    []any{1, "two"},
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := NewQueue(t.Context())

			// Push initial items
			err := q.Push(tt.items...)
			if err != nil {
				t.Fatalf("failed to push items: %v", err)
			}

			// Execute PopN
			got := q.PopN(tt.n)

			// Check length
			if gotLen := q.Len(); gotLen != tt.wantLen {
				t.Errorf("Queue.Len() after PopN = %v, want %v", gotLen, tt.wantLen)
			}

			// Check popped items
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Queue.PopN() = %v, want %v", got, tt.want)
			}
		})
	}
}
