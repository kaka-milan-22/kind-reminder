package queue

import (
	"context"
	"errors"
)

var ErrRedisQueueNotImplemented = errors.New("redis queue is not implemented yet")

// RedisQueue is a placeholder for future MQ backend integration.
// It exists to keep extension points stable while memory queue remains default.
type RedisQueue[T any] struct{}

func NewRedisQueue[T any]() *RedisQueue[T] {
	return &RedisQueue[T]{}
}

func (q *RedisQueue[T]) Push(ctx context.Context, payload T) error {
	return ErrRedisQueueNotImplemented
}

func (q *RedisQueue[T]) StartWorkers(ctx context.Context, handler WorkerHandler[T]) {}

func (q *RedisQueue[T]) Len() int { return 0 }

func (q *RedisQueue[T]) Workers() int { return 0 }

func (q *RedisQueue[T]) Type() string { return "redis" }

func (q *RedisQueue[T]) RateLimitPerSec() int { return 0 }
