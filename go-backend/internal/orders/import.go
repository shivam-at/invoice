// Order import from a Google Sheet export, ported from the real Unicommerce
// "Sale Order Item" report: one row PER LINE ITEM, not per order, so rows
// sharing the same Display Order Code are grouped into a single order —
// matching this whole pipeline's one-invoice-per-order model. Each group's
// combo is resolved from its Bundle SKU Code Number; any row whose SKU
// isn't part of that combo's own definition becomes a standalone extra
// item (e.g. a free gift bundled onto that specific order).
package orders

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"invoice-system/internal/catalog"
	"invoice-system/internal/models"
	"invoice-system/internal/queue"
)

type orderColumnIndex struct {
	displayOrderCode, itemSku, bundleSku, sellingPrice                  int
	shipName, shipLine1, shipLine2, shipCity, shipState, shipPincode    int
	channelName, shippingCourier, shippingProvider, trackingNumber, cod int
	prepaidAmount                                                       int
}

func findOrderColumns(header []string) (orderColumnIndex, error) {
	byName := map[string]int{}
	for i, h := range header {
		byName[strings.ToLower(strings.TrimSpace(h))] = i
	}
	get := func(name string) int {
		if v, ok := byName[strings.ToLower(name)]; ok {
			return v
		}
		return -1
	}
	col := orderColumnIndex{
		displayOrderCode: get("Display Order Code"),
		itemSku:          get("Item SKU Code"),
		bundleSku:        get("Bundle SKU Code Number"),
		sellingPrice:     get("Selling Price"),
		shipName:         get("Shipping Address Name"),
		shipLine1:        get("Shipping Address Line 1"),
		shipLine2:        get("Shipping Address Line 2"),
		shipCity:         get("Shipping Address City"),
		shipState:        get("Shipping Address State"),
		shipPincode:      get("Shipping Address Pincode"),
		channelName:      get("Channel Name"),
		shippingCourier:  get("Shipping Courier"),
		shippingProvider: get("Shipping provider"),
		trackingNumber:   get("Tracking Number"),
		cod:              get("COD"),
		prepaidAmount:    get("Prepaid Amount"),
	}
	if col.displayOrderCode == -1 || col.itemSku == -1 || col.bundleSku == -1 {
		return col, fmt.Errorf("sheet must have Display Order Code, Item SKU Code, and Bundle SKU Code Number columns")
	}
	return col, nil
}

func orderCell(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}

type OrderSkipReason struct {
	OrderCode string `json:"order_code"`
	Reason    string `json:"reason"`
}

type OrderImportResult struct {
	Created     []string          `json:"created"`
	Skipped     []OrderSkipReason `json:"skipped"`
	TotalGroups int               `json:"total_groups"`
}

// ImportOrdersFromRows groups rows by Display Order Code (in the order
// they first appear in the sheet) and processes the slice
// [offset, offset+limit) of those groups — a positional "range" over the
// sheet's distinct orders, not a raw row range, since one order's line
// items must never be split across a range boundary. Every order in that
// slice is attempted (success or skip both count), so a range like
// offset=200 limit=200 always means "the 201st-400th orders in the sheet",
// regardless of how many of them turn out to be skippable. Each created
// order is enqueued straight onto the invoice queue — the same path a
// UI-created order takes.
func (r *Repository) ImportOrdersFromRows(ctx context.Context, rows [][]string, catalogRepo *catalog.Repository, invoiceQ *queue.Queue, offset, limit int) (OrderImportResult, error) {
	if len(rows) < 2 {
		return OrderImportResult{}, fmt.Errorf("sheet has no data rows below the header")
	}
	col, err := findOrderColumns(rows[0])
	if err != nil {
		return OrderImportResult{}, err
	}

	groups := map[string][][]string{}
	var orderCodes []string
	for _, row := range rows[1:] {
		code := orderCell(row, col.displayOrderCode)
		if code == "" {
			continue
		}
		if _, seen := groups[code]; !seen {
			orderCodes = append(orderCodes, code)
		}
		groups[code] = append(groups[code], row)
	}

	result := OrderImportResult{Created: []string{}, Skipped: []OrderSkipReason{}, TotalGroups: len(orderCodes)}

	start := offset
	if start > len(orderCodes) {
		start = len(orderCodes)
	}
	end := start + limit
	if end > len(orderCodes) {
		end = len(orderCodes)
	}

	for _, code := range orderCodes[start:end] {
		if err := r.importOneOrder(ctx, code, groups[code], col, catalogRepo, invoiceQ); err != nil {
			result.Skipped = append(result.Skipped, OrderSkipReason{OrderCode: code, Reason: err.Error()})
			continue
		}
		result.Created = append(result.Created, code)
	}
	return result, nil
}

func (r *Repository) importOneOrder(ctx context.Context, orderCode string, rows [][]string, col orderColumnIndex,
	catalogRepo *catalog.Repository, invoiceQ *queue.Queue) error {
	var existingID int64
	if err := r.pool.QueryRow(ctx, `SELECT id FROM orders WHERE order_no = $1`, orderCode).Scan(&existingID); err == nil {
		return fmt.Errorf("already imported (order id %d)", existingID)
	}

	comboCodeCounts := map[string]int{}
	for _, row := range rows {
		if bc := orderCell(row, col.bundleSku); bc != "" {
			comboCodeCounts[bc]++
		}
	}
	if len(comboCodeCounts) > 1 {
		return fmt.Errorf("order references %d different combo codes — multi-combo orders aren't supported yet", len(comboCodeCounts))
	}

	// Every order gets invoiced now, not just combo orders — a plain,
	// single/multi-product order with no Bundle SKU Code Number becomes an
	// order with combo_id = nil, and every one of its line items becomes a
	// standalone extra item instead of a combo component.
	var comboIDPtr *int64
	comboSKUs := map[string]bool{}
	for c := range comboCodeCounts {
		comboID, found, err := catalogRepo.FindComboByCode(ctx, c)
		if err != nil {
			return fmt.Errorf("lookup combo: %w", err)
		}
		if !found {
			return fmt.Errorf("combo %s not found in catalog", c)
		}
		comboItems, _, err := r.GetComboItems(ctx, comboID)
		if err != nil {
			return fmt.Errorf("load combo items: %w", err)
		}
		for _, it := range comboItems {
			comboSKUs[it.Product.SKU] = true
		}
		comboIDPtr = &comboID
	}

	first := rows[0]
	stateName := orderCell(first, col.shipState)
	stateCode, ok := lookupStateCode(stateName)
	if !ok {
		return fmt.Errorf("could not map shipping state %q to a GST state code", stateName)
	}

	addressParts := []string{orderCell(first, col.shipLine1), orderCell(first, col.shipLine2), orderCell(first, col.shipCity)}
	var nonEmpty []string
	for _, p := range addressParts {
		if p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	address := strings.Join(nonEmpty, ", ")
	if pin := orderCell(first, col.shipPincode); pin != "" {
		address += " - " + pin
	}

	paymentCode, paymentLabel := "P1", "PREPAID"
	if orderCell(first, col.cod) == "1" {
		paymentCode, paymentLabel = "COD", "COD"
	}
	dispatchThrough := orderCell(first, col.shippingCourier)
	if dispatchThrough == "" {
		dispatchThrough = orderCell(first, col.shippingProvider)
	}

	// Partial-COD orders (e.g. GoKwik PPCOD) split "Prepaid Amount" per line
	// item; the invoice shows the order's total prepaid amount as one row.
	prepaidAmount := 0.0
	for _, row := range rows {
		v, _ := strconv.ParseFloat(orderCell(row, col.prepaidAmount), 64)
		prepaidAmount += v
	}

	order := models.Order{
		OrderNo: orderCode, ComboID: comboIDPtr, ComboQuantity: 1,
		CustomerName: orderCell(first, col.shipName), CustomerAddress: address, CustomerStateCode: stateCode,
		ShopifyOrderNo: orderCode, Portal: orderCell(first, col.channelName),
		PaymentModeCode: paymentCode, PaymentModeLabel: paymentLabel,
		DispatchThrough: dispatchThrough, AWBNo: orderCell(first, col.trackingNumber),
		PrepaidAmount: prepaidAmount,
	}
	if order.CustomerName == "" {
		return fmt.Errorf("missing Shipping Address Name")
	}

	var extraItems []models.OrderExtraItem
	for _, row := range rows {
		sku := orderCell(row, col.itemSku)
		if sku == "" || comboSKUs[sku] {
			continue // already accounted for by the combo's own definition
		}
		product, found, err := catalogRepo.FindProductBySKU(ctx, sku)
		if err != nil {
			return fmt.Errorf("lookup product %s: %w", sku, err)
		}
		if !found {
			return fmt.Errorf("extra-item product %s not found in catalog", sku)
		}
		price, _ := strconv.ParseFloat(orderCell(row, col.sellingPrice), 64)
		extraItems = append(extraItems, models.OrderExtraItem{ProductID: product.ID, Quantity: 1, UnitPrice: price})
	}

	id, err := r.CreateOrder(ctx, order, extraItems)
	if err != nil {
		return fmt.Errorf("create order: %w", err)
	}
	if invoiceQ != nil {
		if err := invoiceQ.Push(ctx, queue.Job{OrderID: id}); err != nil {
			return fmt.Errorf("enqueue order %d: %w", id, err)
		}
		_, _ = r.pool.Exec(ctx, `UPDATE orders SET status = $1 WHERE id = $2`, models.OrderQueued, id)
	}
	return nil
}

// lookupStateCode maps the free-text state names Unicommerce exports to our
// GST state codes, tolerating a few common spelling variants.
var stateNameToCode = map[string]string{
	"jammu and kashmir": "01", "jammu & kashmir": "01",
	"himachal pradesh": "02",
	"punjab":           "03",
	"chandigarh":       "04",
	"uttarakhand":      "05", "uttaranchal": "05",
	"haryana": "06",
	"delhi":   "07", "new delhi": "07", "nct of delhi": "07",
	"rajasthan":         "08",
	"uttar pradesh":     "09",
	"bihar":             "10",
	"sikkim":            "11",
	"arunachal pradesh": "12",
	"nagaland":          "13",
	"manipur":           "14",
	"mizoram":           "15",
	"tripura":           "16",
	"meghalaya":         "17",
	"assam":             "18",
	"west bengal":       "19",
	"jharkhand":         "20",
	"odisha":            "21", "orissa": "21",
	"chhattisgarh": "22", "chattisgarh": "22",
	"madhya pradesh": "23",
	"gujarat":        "24",
	"daman and diu":  "25", "daman & diu": "25",
	"dadra and nagar haveli": "26", "dadra & nagar haveli": "26",
	"maharashtra":    "27",
	"andhra pradesh": "37",
	"karnataka":      "29",
	"goa":            "30",
	"lakshadweep":    "31",
	"kerala":         "32",
	"tamil nadu":     "33", "tamilnadu": "33",
	"puducherry": "34", "pondicherry": "34",
	"andaman and nicobar islands": "35", "andaman & nicobar islands": "35",
	"telangana": "36",
	"ladakh":    "38",
}

func lookupStateCode(name string) (string, bool) {
	code, ok := stateNameToCode[strings.ToLower(strings.TrimSpace(name))]
	return code, ok
}
