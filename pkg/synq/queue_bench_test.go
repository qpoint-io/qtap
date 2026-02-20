package synq

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// Helper functions for benchmark setup

// populateQueue populates the queue with n integer items
func populateQueue(q *Queue, n int) {
	for i := range n {
		_ = q.Push(i)
	}
}

// populateQueuePriority populates the queue with n prioritized items
func populateQueuePriority(q *Queue, n int) {
	for i := range n {
		_ = q.Push(prioritizedItem{
			value:    fmt.Sprintf("item-%d", i),
			priority: i,
		})
	}
}

// Group A: Basic Operations Benchmarks

func BenchmarkQueue_Push(b *testing.B) {
	ctx := b.Context()
	q := NewQueue(ctx)

	b.ResetTimer()
	for range b.N {
		_ = q.Push(1)
	}
}

func BenchmarkQueue_PushBatch(b *testing.B) {
	ctx := b.Context()
	q := NewQueue(ctx)

	b.ResetTimer()
	for range b.N {
		_ = q.Push(1, 2, 3, 4, 5, 6, 7, 8, 9, 10)
	}
}

func BenchmarkQueue_Pop(b *testing.B) {
	ctx := b.Context()
	q := NewQueue(ctx)
	populateQueue(q, 1000)

	b.ResetTimer()
	for range b.N {
		_ = q.Pop()
	}
}

func BenchmarkQueue_PopN(b *testing.B) {
	ctx := b.Context()
	q := NewQueue(ctx)
	populateQueue(q, 10000)

	b.ResetTimer()
	for range b.N {
		_ = q.PopN(10)
	}
}

func BenchmarkQueue_Next(b *testing.B) {
	ctx := b.Context()
	q := NewQueue(ctx)
	populateQueue(q, b.N)

	b.ResetTimer()
	for range b.N {
		_, _ = q.Next()
	}
}

func BenchmarkQueue_Len(b *testing.B) {
	ctx := b.Context()
	q := NewQueue(ctx)
	populateQueue(q, 1000)

	b.ResetTimer()
	for range b.N {
		_ = q.Len()
	}
}

// Group B: Priority Operations Benchmarks

func BenchmarkQueue_PushPriorityEmpty(b *testing.B) {
	ctx := b.Context()

	b.ResetTimer()
	for range b.N {
		q := NewQueue(ctx)
		_ = q.Push(prioritizedItem{value: "test", priority: 50})
	}
}

func BenchmarkQueue_PushPriorityHead(b *testing.B) {
	ctx := b.Context()
	q := NewQueue(ctx)
	populateQueuePriority(q, 100)

	b.ResetTimer()
	for range b.N {
		_ = q.Push(prioritizedItem{value: "highest", priority: -1})
	}
}

func BenchmarkQueue_PushPriorityMiddle(b *testing.B) {
	ctx := b.Context()
	q := NewQueue(ctx)
	populateQueuePriority(q, 100)

	b.ResetTimer()
	for range b.N {
		_ = q.Push(prioritizedItem{value: "middle", priority: 50})
	}
}

func BenchmarkQueue_PushPriorityTail(b *testing.B) {
	ctx := b.Context()
	q := NewQueue(ctx)
	populateQueuePriority(q, 100)

	b.ResetTimer()
	for range b.N {
		_ = q.Push(prioritizedItem{value: "lowest", priority: 1000})
	}
}

func BenchmarkQueue_PushMixed(b *testing.B) {
	ctx := b.Context()
	q := NewQueue(ctx)

	b.ResetTimer()
	for range b.N {
		_ = q.Push(
			prioritizedItem{value: "p1", priority: 1},
			prioritizedItem{value: "p2", priority: 2},
			prioritizedItem{value: "p3", priority: 3},
			prioritizedItem{value: "p4", priority: 4},
			prioritizedItem{value: "p5", priority: 5},
			1, 2, 3, 4, 5, // non-priority items
		)
	}
}

// Group C: Concurrent Operations Benchmarks

func BenchmarkQueue_Concurrent1Writer(b *testing.B) {
	ctx := b.Context()
	q := NewQueue(ctx)

	b.ResetTimer()

	var wg sync.WaitGroup

	wg.Go(func() {
		for range b.N {
			_ = q.Push(1)
		}
	})

	wg.Wait()
}

func BenchmarkQueue_Concurrent10Writers(b *testing.B) {
	ctx := b.Context()
	q := NewQueue(ctx)

	b.ResetTimer()

	var wg sync.WaitGroup
	concurrency := 10
	itemsPerWorker := b.N / concurrency

	for range concurrency {
		wg.Go(func() {
			for range itemsPerWorker {
				_ = q.Push(1)
			}
		})
	}

	wg.Wait()
}

func BenchmarkQueue_Concurrent10Readers(b *testing.B) {
	ctx := b.Context()
	q := NewQueue(ctx)
	populateQueue(q, b.N)

	b.ResetTimer()

	var wg sync.WaitGroup
	concurrency := 10
	itemsPerWorker := b.N / concurrency

	for range concurrency {
		wg.Go(func() {
			for range itemsPerWorker {
				_ = q.Pop()
			}
		})
	}

	wg.Wait()
}

func BenchmarkQueue_Concurrent1W1R(b *testing.B) {
	ctx := b.Context()
	q := NewQueue(ctx)

	b.ResetTimer()

	var wg sync.WaitGroup
	wg.Add(2)

	// Writer
	go func() {
		defer wg.Done()
		for range b.N {
			_ = q.Push(1)
		}
	}()

	// Reader
	go func() {
		defer wg.Done()
		for range b.N {
			_, _ = q.Next()
		}
	}()

	wg.Wait()
}

func BenchmarkQueue_Concurrent10W10R(b *testing.B) {
	ctx := b.Context()
	q := NewQueue(ctx)

	b.ResetTimer()

	var wg sync.WaitGroup
	concurrency := 10
	itemsPerWorker := b.N / concurrency

	// Writers
	for range concurrency {
		wg.Go(func() {
			for range itemsPerWorker {
				_ = q.Push(1)
			}
		})
	}

	// Readers
	for range concurrency {
		wg.Go(func() {
			for range itemsPerWorker {
				_ = q.Pop()
			}
		})
	}

	wg.Wait()
}

// Group D: Special Scenarios Benchmarks

func BenchmarkQueue_AlternatingPushPop(b *testing.B) {
	ctx := b.Context()
	q := NewQueue(ctx)

	b.ResetTimer()
	for range b.N {
		_ = q.Push(1)
		_ = q.Pop()
	}
}

func BenchmarkQueue_LargeQueue(b *testing.B) {
	ctx := b.Context()
	q := NewQueue(ctx)
	populateQueue(q, 10000)

	b.ResetTimer()
	for range b.N {
		_ = q.Push(1)
	}
}

func BenchmarkQueue_EmptyPop(b *testing.B) {
	ctx := b.Context()
	q := NewQueue(ctx)

	b.ResetTimer()
	for range b.N {
		_ = q.Pop()
	}
}

func BenchmarkQueue_Drain(b *testing.B) {
	ctx := b.Context()

	b.ResetTimer()
	for range b.N {
		q := NewQueue(ctx)
		populateQueue(q, 100)

		// Start a goroutine to pop items
		go func() {
			for range 100 {
				q.Pop()
			}
		}()

		_ = q.Drain(time.Second)
	}
}
