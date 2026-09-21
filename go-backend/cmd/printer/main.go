// The print-job worker pool: pulls order ids off queue:print_jobs and sends
// each invoice's PDF to the Printer implementation, retrying failures with
// backoff before dead-lettering.
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
	"invoice-system/internal/metrics"
	"invoice-system/internal/models"
	"invoice-system/internal/orders"
	"invoice-system/internal/printservice"
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
	printQ := queue.New(rdb, cfg.PrintJobsQueue)
	printer := printservice.NewStubPrinter()

	metrics.Serve(cfg.PrinterMetricsPort)
	log.Printf("printer pool starting: %d workers, metrics on %s", cfg.PrinterPoolSize, cfg.PrinterMetricsPort)

	var wg sync.WaitGroup
	for i := 0; i < cfg.PrinterPoolSize; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			runPrinter(ctx, id, repo, printer, printQ, cfg.MaxJobAttempts)
		}(i)
	}
	wg.Wait()
	log.Println("printer pool shut down")
}

func runPrinter(ctx context.Context, id int, repo *orders.Repository, printer printservice.Printer,
	printQ *queue.Queue, maxAttempts int) {
	for {
		if ctx.Err() != nil {
			return
		}
		job, err := printQ.Pop(ctx, 2*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("printer %d: pop error: %v", id, err)
			time.Sleep(time.Second)
			continue
		}
		if job == nil {
			continue
		}
		processPrintJob(ctx, id, *job, repo, printer, printQ, maxAttempts)
	}
}

func processPrintJob(ctx context.Context, printerID int, job queue.Job, repo *orders.Repository,
	printer printservice.Printer, printQ *queue.Queue, maxAttempts int) {
	start := time.Now()
	defer func() { metrics.PrintJobDuration.Observe(time.Since(start).Seconds()) }()

	_ = repo.UpdateOrderStatus(ctx, job.OrderID, models.OrderPrinting)
	_ = repo.UpdatePrintJob(ctx, job.OrderID, models.PrintPrinting, job.Attempts, "")

	inv, err := repo.GetInvoice(ctx, job.OrderID)
	if err != nil {
		log.Printf("printer %d: order %d has no invoice yet: %v", printerID, job.OrderID, err)
		backoff(job.Attempts)
		_ = printQ.Requeue(ctx, job)
		return
	}

	if err := printer.Print(inv.PDFPath); err != nil {
		log.Printf("printer %d: order %d print failed (attempt %d): %v", printerID, job.OrderID, job.Attempts+1, err)
		if job.Attempts+1 >= maxAttempts {
			_ = repo.UpdateOrderStatus(ctx, job.OrderID, models.OrderFailed)
			_ = repo.UpdatePrintJob(ctx, job.OrderID, models.PrintFailed, job.Attempts+1, err.Error())
			_ = printQ.Dead(ctx, job)
			metrics.PrintJobsFailed.Inc()
			return
		}
		backoff(job.Attempts)
		_ = printQ.Requeue(ctx, job)
		return
	}

	_ = repo.UpdateOrderStatus(ctx, job.OrderID, models.OrderPrinted)
	_ = repo.UpdatePrintJob(ctx, job.OrderID, models.PrintPrinted, job.Attempts+1, "")
	_ = printQ.Ack(ctx, job)
	metrics.PrintJobsProcessed.Inc()
	log.Printf("printer %d: order %d printed (invoice %s)", printerID, job.OrderID, inv.InvoiceNumber)
}

func backoff(attempts int) {
	ms := 200 * math.Pow(2, float64(attempts))
	if ms > 5000 {
		ms = 5000
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
}
