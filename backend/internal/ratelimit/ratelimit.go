// Package ratelimit provides Redis-backed distributed rate limiting and locks.
// All implementations fail open: a Redis outage never blocks delivery.
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Limiter interface {
	// Allow reports whether the caller may proceed under limit-per-second for key.
	Allow(ctx context.Context, key string, perSecond int) bool
	// TryLock acquires a best-effort distributed lock for ttl.
	TryLock(ctx context.Context, key string, ttl time.Duration) bool
	Close() error
}

// New returns a Redis limiter, or a no-op limiter when url is empty.
func New(url string) (Limiter, error) {
	if url == "" {
		return Noop{}, nil
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	return &Redis{c: redis.NewClient(opts)}, nil
}

type Redis struct{ c *redis.Client }

// Allow implements a fixed one-second window counter.
func (r *Redis) Allow(ctx context.Context, key string, perSecond int) bool {
	if perSecond <= 0 {
		return true
	}
	ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	k := fmt.Sprintf("ep:rl:%s:%d", key, time.Now().Unix())
	pipe := r.c.Pipeline()
	incr := pipe.Incr(ctx, k)
	pipe.Expire(ctx, k, 2*time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return true
	}
	return incr.Val() <= int64(perSecond)
}

func (r *Redis) TryLock(ctx context.Context, key string, ttl time.Duration) bool {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	ok, err := r.c.SetNX(ctx, "ep:lock:"+key, "1", ttl).Result()
	if err != nil {
		return true
	}
	return ok
}

func (r *Redis) Close() error { return r.c.Close() }

type Noop struct{}

func (Noop) Allow(context.Context, string, int) bool              { return true }
func (Noop) TryLock(context.Context, string, time.Duration) bool { return true }
func (Noop) Close() error                                         { return nil }
