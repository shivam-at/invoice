package models

import "time"

type Product struct {
	ID           int64   `json:"id"`
	SKU          string  `json:"sku"`
	Name         string  `json:"name"`
	HSNCode      string  `json:"hsn_code"`
	Price        float64 `json:"price"`
	TaxRate      float64 `json:"tax_rate"`
	CategoryCode string  `json:"category_code"`
}

type Combo struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	Code          string  `json:"code"`
	Description   string  `json:"description"`
	DiscountType  string  `json:"discount_type"`
	DiscountValue float64 `json:"discount_value"`
}

type ComboItem struct {
	ComboID   int64   `json:"combo_id"`
	ProductID int64   `json:"product_id"`
	Quantity  float64 `json:"quantity"`
	Product   Product `json:"product"`
}

// OrderExtraItem is a standalone product line on an order that isn't part
// of its combo (e.g. a "FREE GIFT" bundled onto that specific order) —
// rendered as its own top-level Sr row on the invoice, not indented under
// the combo. UnitPrice is stored per-line rather than looked up from
// products.price, since these are often priced far below catalog price.
type OrderExtraItem struct {
	ID        int64   `json:"id"`
	OrderID   int64   `json:"order_id"`
	ProductID int64   `json:"product_id"`
	Quantity  float64 `json:"quantity"`
	UnitPrice float64 `json:"unit_price"`
	Product   Product `json:"product"`
}

// Order statuses form a strict pipeline:
//
//	PENDING -> QUEUED -> GENERATING -> GENERATED -> PRINTING -> PRINTED
//	                                                        \-> FAILED
const (
	OrderPending    = "PENDING"
	OrderQueued     = "QUEUED"
	OrderGenerating = "GENERATING"
	OrderGenerated  = "GENERATED"
	OrderPrinting   = "PRINTING"
	OrderPrinted    = "PRINTED"
	OrderFailed     = "FAILED"
)

type Order struct {
	ID                int64     `json:"id"`
	OrderNo           string    `json:"order_no"`
	ComboID           *int64    `json:"combo_id"`
	ComboQuantity     float64   `json:"combo_quantity"`
	CustomerName      string    `json:"customer_name"`
	CustomerAddress   string    `json:"customer_address"`
	CustomerStateCode string    `json:"customer_state_code"`
	ShopifyOrderNo    string    `json:"shopify_order_no"`
	Portal            string    `json:"portal"`
	PaymentModeCode   string    `json:"payment_mode_code"`
	PaymentModeLabel  string    `json:"payment_mode_label"`
	DispatchThrough   string    `json:"dispatch_through"`
	AWBNo             string    `json:"awb_no"`
	ShippingName      string    `json:"shipping_name"`
	ShippingAddress   string    `json:"shipping_address"`
	IsCMD             bool      `json:"is_cmd"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type Invoice struct {
	OrderID       int64     `json:"order_id"`
	InvoiceNumber string    `json:"invoice_number"`
	PDFPath       string    `json:"pdf_path"`
	TotalAmount   float64   `json:"total_amount"`
	GeneratedAt   time.Time `json:"generated_at"`
}

const (
	PrintQueued   = "QUEUED"
	PrintPrinting = "PRINTING"
	PrintPrinted  = "PRINTED"
	PrintFailed   = "FAILED"
)

type PrintJob struct {
	ID        int64  `json:"id"`
	InvoiceID int64  `json:"invoice_id"` // == order_id
	Status    string `json:"status"`
	Attempts  int    `json:"attempts"`
	LastError string `json:"last_error"`
}
