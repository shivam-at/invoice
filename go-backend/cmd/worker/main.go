// The invoice-generation worker pool: pulls order ids off queue:invoice_jobs,
// generates and persists the invoice PDF, then hands off to the print
// queue. Run several of these (or raise WORKER_POOL_SIZE) to scale
// throughput toward the "50 invoices in 2 minutes" target.
package main

import (
	"context"
	"log"
	"math"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"invoice-system/internal/config"
	"invoice-system/internal/db"
	"invoice-system/internal/invoice"
	"invoice-system/internal/metrics"
	"invoice-system/internal/models"
	"invoice-system/internal/orders"
	"invoice-system/internal/queue"
)

func main() {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, DB: cfg.RedisDB})
	defer rdb.Close()

	repo := orders.NewRepository(pool)
	gen := invoice.NewGenerator(repo, cfg.InvoicesDir)
	invoiceQ := queue.New(rdb, cfg.InvoiceJobsQueue)
	printQ := queue.New(rdb, cfg.PrintJobsQueue)

	metrics.Serve(cfg.WorkerMetricsPort)
	log.Printf("worker pool starting: %d workers, metrics on %s", cfg.WorkerPoolSize, cfg.WorkerMetricsPort)

	var wg sync.WaitGroup
	for i := 0; i < cfg.WorkerPoolSize; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			runWorker(ctx, id, repo, gen, invoiceQ, printQ, cfg.MaxJobAttempts)
		}(i)
	}
	wg.Wait()
	log.Println("worker pool shut down")
}

func runWorker(ctx context.Context, id int, repo *orders.Repository, gen *invoice.Generator,
	invoiceQ, printQ *queue.Queue, maxAttempts int) {
	for {
		if ctx.Err() != nil {
			return
		}
		job, err := invoiceQ.Pop(ctx, 2*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("worker %d: pop error: %v", id, err)
			time.Sleep(time.Second)
			continue
		}
		if job == nil {
			continue // timed out waiting, loop and check ctx again
		}
		processInvoiceJob(ctx, id, *job, repo, gen, invoiceQ, printQ, maxAttempts)
	}
}

func processInvoiceJob(ctx context.Context, workerID int, job queue.Job, repo *orders.Repository,
	gen *invoice.Generator, invoiceQ, printQ *queue.Queue, maxAttempts int) {
	start := time.Now()
	defer func() { metrics.InvoiceJobDuration.Observe(time.Since(start).Seconds()) }()

	_ = repo.UpdateOrderStatus(ctx, job.OrderID, models.OrderGenerating)

	inv, err := gen.Generate(ctx, job.OrderID)
	if err != nil {
		log.Printf("worker %d: order %d generate failed (attempt %d): %v", workerID, job.OrderID, job.Attempts+1, err)
		if job.Attempts+1 >= maxAttempts {
			_ = repo.UpdateOrderStatus(ctx, job.OrderID, models.OrderFailed)
			_ = invoiceQ.Dead(ctx, job)
			metrics.InvoiceJobsFailed.Inc()
			return
		}
		backoff(job.Attempts)
		_ = invoiceQ.Requeue(ctx, job)
		return
	}

	if _, err := repo.CreatePrintJobIfAbsent(ctx, inv.OrderID); err != nil {
		log.Printf("worker %d: order %d create print job failed: %v", workerID, job.OrderID, err)
		backoff(job.Attempts)
		_ = invoiceQ.Requeue(ctx, job)
		return
	}
	if err := printQ.Push(ctx, queue.Job{OrderID: inv.OrderID}); err != nil {
		log.Printf("worker %d: order %d enqueue print job failed: %v", workerID, job.OrderID, err)
		backoff(job.Attempts)
		_ = invoiceQ.Requeue(ctx, job)
		return
	}

	_ = repo.UpdateOrderStatus(ctx, job.OrderID, models.OrderGenerated)
	_ = invoiceQ.Ack(ctx, job)
	metrics.InvoiceJobsProcessed.Inc()
	log.Printf("worker %d: order %d invoice %s generated", workerID, job.OrderID, inv.InvoiceNumber)
}

// backoff is a simple capped exponential wait: 200ms, 400ms, 800ms... up to 5s.
func backoff(attempts int) {
	ms := 200 * math.Pow(2, float64(attempts))
	if ms > 5000 {
		ms = 5000
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
}
