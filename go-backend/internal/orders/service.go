package orders

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"invoice-system/internal/idempotency"
	"invoice-system/internal/models"
	"invoice-system/internal/queue"
)

type Service struct {
	repo     *Repository
	rdb      *redis.Client
	invoiceQ *queue.Queue
}

func NewService(repo *Repository, rdb *redis.Client, invoiceQ *queue.Queue) *Service {
	return &Service{repo: repo, rdb: rdb, invoiceQ: invoiceQ}
}

func (s *Service) CreateOrder(ctx context.Context, o models.Order, extraItems []models.OrderExtraItem) (int64, error) {
	return s.repo.CreateOrder(ctx, o, extraItems)
}

func (s *Service) GetOrder(ctx context.Context, id int64) (models.Order, error) {
	return s.repo.GetOrder(ctx, id)
}

// IdentifyAndEnqueueCMDOrders is the entry point of the whole pipeline: find
// PENDING orders whose combo code starts with CMB, claim them (flip to
// QUEUED so nobody else grabs them), and push one invoice-generation job
// per order onto Redis. Returns how many were enqueued.
func (s *Service) IdentifyAndEnqueueCMDOrders(ctx context.Context, limit int) (int, error) {
	ids, err := s.repo.ClaimCMDOrders(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("claim cmd orders: %w", err)
	}

	enqueued := 0
	for _, id := range ids {
		key := fmt.Sprintf("idemp:enqueue:%d", id)
		acquired, err := idempotency.TryAcquire(ctx, s.rdb, key, 10*time.Minute)
		if err != nil {
			return enqueued, fmt.Errorf("idempotency check for order %d: %w", id, err)
		}
		if !acquired {
			// Already enqueued by a previous call within the TTL window —
			// the order's status is already QUEUED so this is just a
			// safety no-op, not a bug.
			continue
		}
		if err := s.invoiceQ.Push(ctx, queue.Job{OrderID: id}); err != nil {
			return enqueued, fmt.Errorf("enqueue order %d: %w", id, err)
		}
		enqueued++
	}
	return enqueued, nil
}

func (s *Service) CountByStatus(ctx context.Context, status string) (int, error) {
	return s.repo.CountByStatus(ctx, status)
}
