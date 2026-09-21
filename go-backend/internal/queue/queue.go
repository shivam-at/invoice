package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Job is the payload carried on every queue. OrderID doubles as the
// invoice/print-job id throughout the pipeline, since there's exactly one
// invoice per order.
type Job struct {
	OrderID  int64 `json:"order_id"`
	Attempts int   `json:"attempts"`
}

// Queue implements the reliable-queue pattern: a consumer moves a job from
// the main list to a processing list atomically (BRPOPLPUSH), so a worker
// that crashes mid-job leaves the job visible in the processing list rather
// than losing it outright. Ack removes it from processing; Requeue/Dead move
// it back to the main list or to a dead-letter list.
type Queue struct {
	rdb        *redis.Client
	name       string
	processing string
	dead       string
}

func New(rdb *redis.Client, name string) *Queue {
	return &Queue{
		rdb:        rdb,
		name:       name,
		processing: name + ":processing",
		dead:       name + ":dead",
	}
}

func (q *Queue) Push(ctx context.Context, job Job) error {
	b, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return q.rdb.LPush(ctx, q.name, b).Err()
}

// Pop blocks up to timeout for a job, atomically moving it into the
// processing list. Returns (nil, nil) on timeout — not an error, just "no
// job right now."
func (q *Queue) Pop(ctx context.Context, timeout time.Duration) (*Job, error) {
	raw, err := q.rdb.BRPopLPush(ctx, q.name, q.processing, timeout).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var job Job
	if err := json.Unmarshal([]byte(raw), &job); err != nil {
		return nil, fmt.Errorf("decode job: %w", err)
	}
	return &job, nil
}

func (q *Queue) Ack(ctx context.Context, job Job) error {
	b, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return q.rdb.LRem(ctx, q.processing, 1, b).Err()
}

// Requeue removes the job from the processing list and pushes an
// attempts-incremented copy back onto the main queue. Callers are expected
// to sleep/backoff before calling this, since Redis lists have no native
// per-item delay.
func (q *Queue) Requeue(ctx context.Context, job Job) error {
	old, err := json.Marshal(job)
	if err != nil {
		return err
	}
	job.Attempts++
	next, err := json.Marshal(job)
	if err != nil {
		return err
	}
	pipe := q.rdb.TxPipeline()
	pipe.LRem(ctx, q.processing, 1, old)
	pipe.LPush(ctx, q.name, next)
	_, err = pipe.Exec(ctx)
	return err
}

// Dead moves an exhausted job from processing to the dead-letter list for
// manual inspection instead of retrying forever.
func (q *Queue) Dead(ctx context.Context, job Job) error {
	old, err := json.Marshal(job)
	if err != nil {
		return err
	}
	pipe := q.rdb.TxPipeline()
	pipe.LRem(ctx, q.processing, 1, old)
	pipe.LPush(ctx, q.dead, old)
	_, err = pipe.Exec(ctx)
	return err
}

// Depth reports the current backlog size — the main metric a load test
// watches to confirm the pool is keeping up.
func (q *Queue) Depth(ctx context.Context) (int64, error) {
	return q.rdb.LLen(ctx, q.name).Result()
}
