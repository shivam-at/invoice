// Package metrics exposes Prometheus counters/histograms for the invoice
// and print pipelines. Each binary (api/worker/printer) calls Serve on its
// own port so they can be scraped independently.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	InvoiceJobsProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "invoice_jobs_processed_total",
		Help: "Invoice-generation jobs completed successfully.",
	})
	InvoiceJobsFailed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "invoice_jobs_failed_total",
		Help: "Invoice-generation jobs that exhausted retries and went to the dead-letter queue.",
	})
	InvoiceJobDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "invoice_job_duration_seconds",
		Help:    "Time to generate one invoice PDF and persist it.",
		Buckets: prometheus.DefBuckets,
	})

	PrintJobsProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "print_jobs_processed_total",
		Help: "Print jobs completed successfully.",
	})
	PrintJobsFailed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "print_jobs_failed_total",
		Help: "Print jobs that exhausted retries and went to the dead-letter queue.",
	})
	PrintJobDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "print_job_duration_seconds",
		Help:    "Time to print one invoice, including retries.",
		Buckets: prometheus.DefBuckets,
	})

	QueueDepth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "queue_depth",
		Help: "Current backlog size per queue.",
	}, []string{"queue"})
)

// Serve starts a background HTTP server exposing /metrics. It never returns
// an error to the caller — a metrics endpoint failing to bind shouldn't take
// down the actual job-processing loop.
func Serve(addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	go func() {
		_ = http.ListenAndServe(addr, mux)
	}()
}
