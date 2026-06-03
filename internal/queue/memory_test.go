package queue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestMemoryQueueGlobalRateLimit verifies the rate limit is global across the
// pool, not per worker. With rate=10/sec and 5 workers, 16 tasks must take
// >~1s. The previous per-worker design gave 5*10=50/sec and would finish in
// ~0.3s.
func TestMemoryQueueGlobalRateLimit(t *testing.T) {
	const rate = 10
	const workers = 5
	const tasks = 16

	q := NewMemoryQueue[int](100, workers, rate)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var done int64
	q.StartWorkers(ctx, func(context.Context, int) error {
		atomic.AddInt64(&done, 1)
		return nil
	})

	start := time.Now()
	for i := 0; i < tasks; i++ {
		if err := q.Push(ctx, i); err != nil {
			t.Fatal(err)
		}
	}

	deadline := time.After(5 * time.Second)
	for atomic.LoadInt64(&done) < tasks {
		select {
		case <-deadline:
			t.Fatalf("only %d/%d tasks processed", atomic.LoadInt64(&done), tasks)
		case <-time.After(10 * time.Millisecond):
		}
	}

	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("processed %d tasks in %v — global rate limit not enforced", tasks, elapsed)
	}
}

// TestMemoryQueueNoRateLimit confirms rate=0 disables throttling entirely.
func TestMemoryQueueNoRateLimit(t *testing.T) {
	q := NewMemoryQueue[int](100, 4, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var done int64
	q.StartWorkers(ctx, func(context.Context, int) error {
		atomic.AddInt64(&done, 1)
		return nil
	})

	for i := 0; i < 50; i++ {
		if err := q.Push(ctx, i); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.After(2 * time.Second)
	for atomic.LoadInt64(&done) < 50 {
		select {
		case <-deadline:
			t.Fatalf("only %d/50 processed with no rate limit", atomic.LoadInt64(&done))
		case <-time.After(5 * time.Millisecond):
		}
	}
}
