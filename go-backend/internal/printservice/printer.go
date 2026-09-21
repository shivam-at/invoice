package printservice

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"time"
)

// Printer abstracts the physical printer so the worker pool around it never
// changes when the real hardware integration (network printer, CUPS/lp,
// a vendor SDK) replaces the stub.
type Printer interface {
	Print(pdfPath string) error
}

// StubPrinter simulates print latency and an occasional failure so the
// retry path is exercised in dev without any real hardware attached.
// PRINTER_FAILURE_RATE (0..1, default 0) controls how often it fails.
type StubPrinter struct {
	FailureRate float64
}

func NewStubPrinter() StubPrinter {
	rate := 0.0
	if v := os.Getenv("PRINTER_FAILURE_RATE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			rate = f
		}
	}
	return StubPrinter{FailureRate: rate}
}

func (p StubPrinter) Print(pdfPath string) error {
	if _, err := os.Stat(pdfPath); err != nil {
		return fmt.Errorf("pdf missing: %w", err)
	}
	// Simulated realistic print latency (spooling + physical print time).
	time.Sleep(150 * time.Millisecond)
	if p.FailureRate > 0 && rand.Float64() < p.FailureRate {
		return fmt.Errorf("simulated printer jam")
	}
	return nil
}
