package queue

import "context"

type WorkerHandler[T any] func(context.Context, T) error

type DispatchQueue[T any] interface {
	Push(ctx context.Context, payload T) error
	StartWorkers(ctx context.Context, handler WorkerHandler[T])
	Len() int
	Workers() int
	Type() string
	RateLimitPerSec() int
}
