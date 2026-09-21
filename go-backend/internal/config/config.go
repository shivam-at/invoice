package config

import (
	"os"

	"github.com/joho/godotenv"
)

// Config holds every setting the api/worker/printer binaries need. All of it
// comes from the environment (with .env loaded on top for local dev) so the
// same binaries run unmodified across dev/staging/prod.
type Config struct {
	DatabaseURL string
	RedisAddr   string
	RedisDB     int

	APIPort            string
	MetricsPort        string
	WorkerMetricsPort  string
	PrinterMetricsPort string
	InvoicesDir        string
	GoogleKeyFile      string

	WorkerPoolSize   int
	PrinterPoolSize  int
	MaxJobAttempts   int
	InvoiceJobsQueue string
	PrintJobsQueue   string
}

func Load() Config {
	// Best-effort: a missing .env is normal in prod where real env vars are set.
	_ = godotenv.Load()

	return Config{
		DatabaseURL: getenv("DATABASE_URL", "postgres://postgres:postgres@127.0.0.1:5432/invoice_system?sslmode=disable"),
		RedisAddr:   getenv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisDB:     0,

		APIPort:            getenv("API_PORT", "8080"),
		MetricsPort:        getenv("METRICS_PORT", ":9090"),
		WorkerMetricsPort:  getenv("WORKER_METRICS_PORT", ":9091"),
		PrinterMetricsPort: getenv("PRINTER_METRICS_PORT", ":9092"),
		InvoicesDir:        getenv("INVOICES_DIR", "./storage/invoices"),
		GoogleKeyFile:      getenv("GOOGLE_SERVICE_ACCOUNT_FILE", "./config/google-service-account.json"),

		WorkerPoolSize:   getenvInt("WORKER_POOL_SIZE", 8),
		PrinterPoolSize:  getenvInt("PRINTER_POOL_SIZE", 4),
		MaxJobAttempts:   getenvInt("MAX_JOB_ATTEMPTS", 5),
		InvoiceJobsQueue: "queue:invoice_jobs",
		PrintJobsQueue:   "queue:print_jobs",
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n := 0
	for _, c := range v {
		if c < '0' || c > '9' {
			return fallback
		}
		n = n*10 + int(c-'0')
	}
	return n
}
