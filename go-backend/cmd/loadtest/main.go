// loadtest seeds N synthetic CMD orders, triggers identification, then
// polls /api/stats until `target` orders reach PRINTED (or times out),
// reporting whether the pipeline met the 50-invoices-in-2-minutes SLA.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"invoice-system/internal/config"
	"invoice-system/internal/db"
)

func main() {
	n := flag.Int("n", 60, "number of synthetic orders to create")
	target := flag.Int("target", 50, "PRINTED count that must be reached to pass")
	timeout := flag.Duration("timeout", 2*time.Minute, "how long to wait for the target")
	apiBase := flag.String("api", "http://127.0.0.1:8080", "API base URL")
	flag.Parse()

	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	var comboID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM combos WHERE code = 'CMB_0004_0428'`).Scan(&comboID); err != nil {
		log.Fatalf("demo combo not found — did migrations run? %v", err)
	}

	log.Printf("seeding %d synthetic CMD orders...", *n)
	for i := 0; i < *n; i++ {
		orderNo := fmt.Sprintf("LOADTEST-%d-%d", time.Now().UnixNano(), i)
		if _, err := pool.Exec(ctx, `
			INSERT INTO orders (order_no, combo_id, combo_quantity, customer_name, customer_address, is_cmd, status)
			VALUES ($1, $2, 1, 'Load Test Customer', 'Test Address', true, 'PENDING')
		`, orderNo, comboID); err != nil {
			log.Fatalf("insert order %d: %v", i, err)
		}
	}

	log.Println("triggering identify-cmd...")
	body, _ := json.Marshal(map[string]int{"limit": *n})
	resp, err := http.Post(*apiBase+"/api/orders/identify-cmd", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Fatalf("identify-cmd request failed: %v", err)
	}
	resp.Body.Close()

	start := time.Now()
	deadline := start.Add(*timeout)
	for time.Now().Before(deadline) {
		printed, err := getPrintedCount(*apiBase)
		if err != nil {
			log.Printf("stats poll error: %v", err)
		} else {
			elapsed := time.Since(start)
			log.Printf("elapsed %s: %d/%d printed", elapsed.Round(time.Second), printed, *target)
			if printed >= *target {
				fmt.Printf("\nPASS: %d invoices printed in %s (target: %d in %s)\n", printed, elapsed.Round(time.Second), *target, *timeout)
				return
			}
		}
		time.Sleep(1 * time.Second)
	}

	printed, _ := getPrintedCount(*apiBase)
	fmt.Printf("\nFAIL: only %d/%d invoices printed within %s\n", printed, *target, *timeout)
}

func getPrintedCount(apiBase string) (int, error) {
	resp, err := http.Get(apiBase + "/api/stats")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var stats map[string]int
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return 0, err
	}
	return stats["PRINTED"], nil
}
