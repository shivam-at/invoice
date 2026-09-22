// The API server: order intake plus the "identify CMD orders" trigger that
// kicks off the invoice/print pipeline. Deliberately thin — all real work
// happens in the worker and printer binaries via Redis queues.
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/redis/go-redis/v9"

	"invoice-system/internal/catalog"
	"invoice-system/internal/config"
	"invoice-system/internal/db"
	"invoice-system/internal/metrics"
	"invoice-system/internal/models"
	"invoice-system/internal/orders"
	"invoice-system/internal/queue"
)

type api struct {
	svc           *orders.Service
	repo          *orders.Repository
	catalogRepo   *catalog.Repository
	googleKeyFile string
}

func main() {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	if err := db.RunMigrations(ctx, pool, "migrations"); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, DB: cfg.RedisDB})
	defer rdb.Close()

	repo := orders.NewRepository(pool)
	invoiceQ := queue.New(rdb, cfg.InvoiceJobsQueue)
	svc := orders.NewService(repo, rdb, invoiceQ)
	catalogRepo := catalog.NewRepository(pool)

	a := &api{svc: svc, repo: repo, catalogRepo: catalogRepo, googleKeyFile: cfg.GoogleKeyFile}

	metrics.Serve(cfg.MetricsPort)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.healthz)
	mux.HandleFunc("GET /api/orders", a.listOrders)
	mux.HandleFunc("POST /api/orders", a.createOrder)
	mux.HandleFunc("GET /api/orders/{id}", a.getOrder)
	mux.HandleFunc("GET /api/orders/{id}/pdf", a.getOrderPDF)
	mux.HandleFunc("POST /api/orders/identify-cmd", a.identifyCMD)
	mux.HandleFunc("POST /api/orders/import-google-sheet", a.importOrdersFromSheet)
	mux.HandleFunc("POST /api/orders/delete-recent", a.deleteRecentOrders)
	mux.HandleFunc("GET /api/stats", a.stats)

	mux.HandleFunc("GET /api/products", a.listProducts)
	mux.HandleFunc("POST /api/products", a.createProduct)
	mux.HandleFunc("DELETE /api/products/{id}", a.deleteProduct)
	mux.HandleFunc("POST /api/products/import-google-sheet", a.importProductsFromSheet)

	mux.HandleFunc("GET /api/combos", a.listCombos)
	mux.HandleFunc("POST /api/combos", a.createCombo)
	mux.HandleFunc("DELETE /api/combos/{id}", a.deleteCombo)
	mux.HandleFunc("POST /api/combos/import-google-sheet", a.importCombosFromSheet)
	mux.HandleFunc("POST /api/combos/unresolved-from-sheet", a.unresolvedCombosFromSheet)
	mux.HandleFunc("POST /api/combos/auto-guess-from-sheet", a.autoGuessCombosFromSheet)
	mux.HandleFunc("POST /api/combos/resolve", a.resolveCombo)

	addr := ":" + cfg.APIPort
	log.Printf("api listening on %s (metrics on %s)", addr, cfg.MetricsPort)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func (a *api) healthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

type createOrderReq struct {
	OrderNo           string  `json:"order_no"`
	ComboID           int64   `json:"combo_id"`
	ComboQuantity     float64 `json:"combo_quantity"`
	CustomerName      string  `json:"customer_name"`
	CustomerAddress   string  `json:"customer_address"`
	CustomerStateCode string  `json:"customer_state_code"`
	ShopifyOrderNo    string  `json:"shopify_order_no"`
	Portal            string  `json:"portal"`
	PaymentModeCode   string  `json:"payment_mode_code"`
	PaymentModeLabel  string  `json:"payment_mode_label"`
	DispatchThrough   string  `json:"dispatch_through"`
	AWBNo             string  `json:"awb_no"`
	ShippingName      string  `json:"shipping_name"`
	ShippingAddress   string  `json:"shipping_address"`
	PrepaidAmount     float64 `json:"prepaid_amount"`
	ExtraItems        []struct {
		ProductID int64   `json:"product_id"`
		Quantity  float64 `json:"quantity"`
		UnitPrice float64 `json:"unit_price"`
	} `json:"extra_items"`
}

func (a *api) createOrder(w http.ResponseWriter, r *http.Request) {
	var req createOrderReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.CustomerName == "" || req.OrderNo == "" {
		httpError(w, http.StatusBadRequest, "order_no and customer_name are required")
		return
	}
	if req.ComboID == 0 && len(req.ExtraItems) == 0 {
		httpError(w, http.StatusBadRequest, "either combo_id or at least one extra item is required")
		return
	}
	if req.CustomerStateCode == "" {
		// GST classification (CGST+SGST vs IGST) must never be a silent
		// guess — require the caller to state the customer's state rather
		// than defaulting to "same as company" behind their back.
		httpError(w, http.StatusBadRequest, "customer_state_code is required (GST classification cannot be guessed)")
		return
	}
	if req.ComboQuantity <= 0 {
		req.ComboQuantity = 1
	}

	var extraItems []models.OrderExtraItem
	for _, it := range req.ExtraItems {
		if it.ProductID == 0 {
			httpError(w, http.StatusBadRequest, "each extra item requires product_id")
			return
		}
		extraItems = append(extraItems, models.OrderExtraItem{ProductID: it.ProductID, Quantity: it.Quantity, UnitPrice: it.UnitPrice})
	}

	var comboID *int64
	if req.ComboID != 0 {
		comboID = &req.ComboID
	}

	id, err := a.svc.CreateOrder(r.Context(), models.Order{
		OrderNo: req.OrderNo, ComboID: comboID, ComboQuantity: req.ComboQuantity,
		CustomerName: req.CustomerName, CustomerAddress: req.CustomerAddress, CustomerStateCode: req.CustomerStateCode,
		ShopifyOrderNo: req.ShopifyOrderNo, Portal: req.Portal, PaymentModeCode: req.PaymentModeCode,
		PaymentModeLabel: req.PaymentModeLabel, DispatchThrough: req.DispatchThrough, AWBNo: req.AWBNo,
		ShippingName: req.ShippingName, ShippingAddress: req.ShippingAddress, PrepaidAmount: req.PrepaidAmount,
	}, extraItems)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *api) listOrders(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePageParams(r, 50)
	search := r.URL.Query().Get("search")
	result, err := a.repo.ListOrders(r.Context(), search, page, pageSize)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result.Items, "total": result.Total, "page": page, "pageSize": pageSize})
}

func (a *api) getOrderPDF(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	inv, err := a.repo.GetInvoice(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusNotFound, "invoice not found for this order")
		return
	}
	http.ServeFile(w, r, inv.PDFPath)
}

func (a *api) getOrder(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	order, err := a.repo.GetOrder(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusNotFound, "order not found")
		return
	}
	resp := map[string]any{"order": order}
	if inv, err := a.repo.GetInvoice(r.Context(), id); err == nil {
		resp["invoice"] = inv
	}
	if extraItems, err := a.repo.GetOrderExtraItems(r.Context(), id); err == nil {
		resp["extra_items"] = extraItems
	}
	writeJSON(w, http.StatusOK, resp)
}

type identifyReq struct {
	Limit int `json:"limit"`
}

func (a *api) identifyCMD(w http.ResponseWriter, r *http.Request) {
	var req identifyReq
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Limit <= 0 {
		req.Limit = 100
	}
	n, err := a.svc.IdentifyAndEnqueueCMDOrders(r.Context(), req.Limit)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enqueued": n})
}

// deleteRecentOrders permanently deletes the N most recently created
// orders (and everything tied to them: invoice, print job, extra items) —
// e.g. to clean up demo/test orders. Requires an explicit confirm:true so
// it's never triggered by accident.
func (a *api) deleteRecentOrders(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Count   int  `json:"count"`
		Confirm bool `json:"confirm"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if !req.Confirm {
		httpError(w, http.StatusBadRequest, "confirm: true is required to delete orders")
		return
	}
	if req.Count <= 0 {
		httpError(w, http.StatusBadRequest, "count must be a positive number of orders to delete")
		return
	}
	paths, count, err := a.svc.DeleteRecentOrders(r.Context(), req.Count)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, p := range paths {
		_ = os.Remove(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": count})
}

func (a *api) stats(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{}
	for _, s := range []string{models.OrderPending, models.OrderQueued, models.OrderGenerating,
		models.OrderGenerated, models.OrderPrinting, models.OrderPrinted, models.OrderFailed} {
		n, _ := a.svc.CountByStatus(r.Context(), s)
		out[s] = n
	}
	writeJSON(w, http.StatusOK, out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
