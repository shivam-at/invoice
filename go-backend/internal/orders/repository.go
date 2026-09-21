package orders

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"invoice-system/internal/companyinfo"
	"invoice-system/internal/models"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// CreateOrder inserts a new order, deciding IsCMD from whether the combo's
// code starts with "CMB" (the existing combo-SKU convention this whole
// pipeline exists to fast-track).
func (r *Repository) CreateOrder(ctx context.Context, o models.Order) (int64, error) {
	var comboCode string
	if err := r.pool.QueryRow(ctx, `SELECT code FROM combos WHERE id = $1`, o.ComboID).Scan(&comboCode); err != nil {
		return 0, fmt.Errorf("lookup combo: %w", err)
	}
	isCMD := len(comboCode) >= 3 && comboCode[:3] == "CMB"

	shopifyOrderNo := orDefault(o.ShopifyOrderNo, companyinfo.DefaultOrderNo)
	portal := orDefault(o.Portal, companyinfo.DefaultPortal)
	paymentModeCode := orDefault(o.PaymentModeCode, companyinfo.DefaultPaymentModeCode)
	paymentModeLabel := orDefault(o.PaymentModeLabel, companyinfo.DefaultPaymentMode)
	dispatchThrough := orDefault(o.DispatchThrough, companyinfo.DefaultDispatchThrough)
	awbNo := orDefault(o.AWBNo, companyinfo.DefaultAWBNo)
	shippingName := orDefault(o.ShippingName, o.CustomerName)
	shippingAddress := orDefault(o.ShippingAddress, o.CustomerAddress)

	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO orders (
			order_no, combo_id, combo_quantity, customer_name, customer_address, customer_state_code, is_cmd, status,
			shopify_order_no, portal, payment_mode_code, payment_mode_label, dispatch_through, awb_no,
			shipping_name, shipping_address
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		RETURNING id
	`, o.OrderNo, o.ComboID, o.ComboQuantity, o.CustomerName, o.CustomerAddress, o.CustomerStateCode, isCMD, models.OrderPending,
		shopifyOrderNo, portal, paymentModeCode, paymentModeLabel, dispatchThrough, awbNo, shippingName, shippingAddress).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert order: %w", err)
	}
	return id, nil
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

const orderColumns = `
	id, order_no, combo_id, combo_quantity, customer_name, customer_address,
	COALESCE(customer_state_code, ''), COALESCE(shopify_order_no, ''), COALESCE(portal, ''),
	COALESCE(payment_mode_code, ''), COALESCE(payment_mode_label, ''), COALESCE(dispatch_through, ''),
	COALESCE(awb_no, ''), COALESCE(shipping_name, ''), COALESCE(shipping_address, ''),
	is_cmd, status, created_at, updated_at
`

func scanOrder(row pgx.Row) (models.Order, error) {
	var o models.Order
	err := row.Scan(&o.ID, &o.OrderNo, &o.ComboID, &o.ComboQuantity, &o.CustomerName, &o.CustomerAddress,
		&o.CustomerStateCode, &o.ShopifyOrderNo, &o.Portal, &o.PaymentModeCode, &o.PaymentModeLabel,
		&o.DispatchThrough, &o.AWBNo, &o.ShippingName, &o.ShippingAddress, &o.IsCMD, &o.Status, &o.CreatedAt, &o.UpdatedAt)
	return o, err
}

func (r *Repository) GetOrder(ctx context.Context, id int64) (models.Order, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+orderColumns+` FROM orders WHERE id = $1`, id)
	return scanOrder(row)
}

func (r *Repository) ListOrders(ctx context.Context, limit int) ([]models.Order, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+orderColumns+` FROM orders ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// []models.Order{}, not nil — a nil slice marshals to JSON "null", which
	// crashes frontend code doing orders.map(...) on an empty list.
	out := []models.Order{}
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}

// ClaimCMDOrders atomically selects up to `limit` PENDING CMD orders and
// flips them to QUEUED in the same transaction (FOR UPDATE SKIP LOCKED), so
// two concurrent "identify" calls never both grab the same order.
func (r *Repository) ClaimCMDOrders(ctx context.Context, limit int) ([]int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT id FROM orders
		WHERE is_cmd = true AND status = $1
		ORDER BY created_at
		LIMIT $2
		FOR UPDATE SKIP LOCKED
	`, models.OrderPending, limit)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()

	if len(ids) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE orders SET status = $1, updated_at = now() WHERE id = ANY($2)`,
			models.OrderQueued, ids); err != nil {
			return nil, err
		}
	}
	return ids, tx.Commit(ctx)
}

func (r *Repository) UpdateOrderStatus(ctx context.Context, id int64, status string) error {
	_, err := r.pool.Exec(ctx, `UPDATE orders SET status = $1, updated_at = now() WHERE id = $2`, status, id)
	return err
}

func (r *Repository) GetComboItems(ctx context.Context, comboID int64) ([]models.ComboItem, models.Combo, error) {
	var combo models.Combo
	err := r.pool.QueryRow(ctx, `SELECT id, name, code, discount_type, discount_value FROM combos WHERE id = $1`, comboID).
		Scan(&combo.ID, &combo.Name, &combo.Code, &combo.DiscountType, &combo.DiscountValue)
	if err != nil {
		return nil, combo, fmt.Errorf("lookup combo: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT ci.combo_id, ci.product_id, ci.quantity, p.sku, p.name, p.hsn_code, p.price, p.tax_rate
		FROM combo_items ci JOIN products p ON p.id = ci.product_id
		WHERE ci.combo_id = $1
	`, comboID)
	if err != nil {
		return nil, combo, err
	}
	defer rows.Close()

	var items []models.ComboItem
	for rows.Next() {
		var it models.ComboItem
		if err := rows.Scan(&it.ComboID, &it.ProductID, &it.Quantity, &it.Product.SKU, &it.Product.Name,
			&it.Product.HSNCode, &it.Product.Price, &it.Product.TaxRate); err != nil {
			return nil, combo, err
		}
		items = append(items, it)
	}
	return items, combo, nil
}

// CreateInvoiceIfAbsent is the DB-level idempotency backstop: ON CONFLICT DO
// NOTHING means a duplicate job (e.g. two workers racing on the same order
// after a Redis hiccup) can insert twice without erroring, but only one row
// ever survives. inserted=false tells the caller "someone already did this."
func (r *Repository) CreateInvoiceIfAbsent(ctx context.Context, inv models.Invoice) (inserted bool, err error) {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO invoices (order_id, invoice_number, pdf_path, total_amount)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (order_id) DO NOTHING
	`, inv.OrderID, inv.InvoiceNumber, inv.PDFPath, inv.TotalAmount)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *Repository) CreatePrintJobIfAbsent(ctx context.Context, invoiceID int64) (inserted bool, err error) {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO print_jobs (invoice_id) VALUES ($1)
		ON CONFLICT (invoice_id) DO NOTHING
	`, invoiceID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *Repository) UpdatePrintJob(ctx context.Context, invoiceID int64, status string, attempts int, lastErr string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE print_jobs
		SET status = $1, attempts = $2, last_error = $3, updated_at = now(),
		    printed_at = CASE WHEN $1 = 'PRINTED' THEN now() ELSE printed_at END
		WHERE invoice_id = $4
	`, status, attempts, lastErr, invoiceID)
	return err
}

func (r *Repository) GetInvoice(ctx context.Context, orderID int64) (models.Invoice, error) {
	var inv models.Invoice
	err := r.pool.QueryRow(ctx, `
		SELECT order_id, invoice_number, pdf_path, total_amount, generated_at
		FROM invoices WHERE order_id = $1
	`, orderID).Scan(&inv.OrderID, &inv.InvoiceNumber, &inv.PDFPath, &inv.TotalAmount, &inv.GeneratedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return inv, fmt.Errorf("no invoice for order %d", orderID)
	}
	return inv, err
}

// CountByStatus powers /metrics and the load-test's "how many PRINTED so
// far" poll.
func (r *Repository) CountByStatus(ctx context.Context, status string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM orders WHERE status = $1`, status).Scan(&n)
	return n, err
}
