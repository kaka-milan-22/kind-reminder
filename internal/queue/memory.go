package queue

import (
	"context"
	"time"
)

type MemoryQueue[T any] struct {
	ch       chan T
	workers  int
	rate     int
	queueTyp string
}

func NewMemoryQueue[T any](size, workers, rateLimitPerSec int) *MemoryQueue[T] {
	if size <= 0 {
		size = 100
	}
	if workers <= 0 {
		workers = 10
	}
	return &MemoryQueue[T]{
		ch:       make(chan T, size),
		workers:  workers,
		rate:     rateLimitPerSec,
		queueTyp: "memory",
	}
}

func (q *MemoryQueue[T]) Push(ctx context.Context, p T) error {
	select {
	case q.ch <- p:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *MemoryQueue[T]) StartWorkers(ctx context.Context, handler WorkerHandler[T]) {
	for i := 0; i < q.workers; i++ {
		go func() {
			var ticker *time.Ticker
			if q.rate > 0 {
				ticker = time.NewTicker(time.Second / time.Duration(q.rate))
				defer ticker.Stop()
			}
			for {
				select {
				case p := <-q.ch:
					if ticker != nil {
						select {
						case <-ticker.C:
						case <-ctx.Done():
							return
						}
					}
					_ = handler(ctx, p)
				case <-ctx.Done():
					return
				}
			}
		}()
	}
}

func (q *MemoryQueue[T]) Len() int { return len(q.ch) }

func (q *MemoryQueue[T]) Workers() int { return q.workers }

func (q *MemoryQueue[T]) Type() string { return q.queueTyp }

func (q *MemoryQueue[T]) RateLimitPerSec() int { return q.rate }
