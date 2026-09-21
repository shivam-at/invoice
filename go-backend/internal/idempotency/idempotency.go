// Package idempotency guards against the same order being enqueued twice
// (e.g. two "identify CMD orders" scans overlapping). This is a fast-path
// guard only — the real, unconditional guarantee is the UNIQUE constraint on
// invoices.order_id and print_jobs.invoice_id in migrations/0001_init.sql,
// which holds even if Redis is flushed or this check races.
package idempotency

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// TryAcquire returns true if this is the first caller to claim key within
// ttl. A duplicate scan/job for the same order simply gets false back and
// should skip silently rather than re-enqueue.
func TryAcquire(ctx context.Context, rdb *redis.Client, key string, ttl time.Duration) (bool, error) {
	ok, err := rdb.SetNX(ctx, key, "1", ttl).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}

// Release clears a claim, e.g. after a job fails so a future retry attempt
// is allowed to re-acquire it immediately instead of waiting out the TTL.
func Release(ctx context.Context, rdb *redis.Client, key string) error {
	return rdb.Del(ctx, key).Err()
}
