// Order import from a Google Sheet export, ported from the real Unicommerce
// "Sale Order Item" report: one row PER LINE ITEM, not per order, so rows
// sharing the same Display Order Code are grouped into a single order —
// matching this whole pipeline's one-invoice-per-order model. Each group's
// combo(s) are resolved from its Bundle SKU Code Number(s) — an order
// referencing 2+ different combo codes gets one primary combo plus one
// models.OrderCombo per additional one; any row whose SKU isn't part of
// any resolved combo's own definition becomes a standalone extra item
// (e.g. a free gift bundled onto that specific order).
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
	prepaidAmount, invoiceCode                                          int
	itemTypeName, hsnCode, gstTaxTypeCode                               int
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
		invoiceCode:      get("Invoice Code"),
		itemTypeName:     get("Item Type Name"),
		hsnCode:          get("HSN Code"),
		gstTaxTypeCode:   get("GST Tax Type Code"),
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

	// Collect distinct combo codes in the order they first appear in the
	// sheet — map iteration order isn't stable, and a multi-combo order
	// needs a deterministic "first" combo to designate as primary.
	var comboCodesInOrder []string
	seenCode := map[string]bool{}
	for _, row := range rows {
		if bc := orderCell(row, col.bundleSku); bc != "" && !seenCode[bc] {
			seenCode[bc] = true
			comboCodesInOrder = append(comboCodesInOrder, bc)
		}
	}

	// Every order gets invoiced now, not just combo orders — a plain,
	// single/multi-product order with no Bundle SKU Code Number (or whose
	// combo code isn't in the catalog yet) becomes an order with
	// combo_id = nil, and every one of its line items becomes a standalone
	// extra item instead of a combo component. Nobody's invoice gets
	// skipped just because a combo hasn't been resolved yet. An order that
	// references 2+ *different* combo codes gets its first resolved combo
	// as the primary (order.ComboID) and every other one as an additional
	// models.OrderCombo — each renders as its own group on the invoice.
	var comboIDPtr *int64
	var extraCombos []models.OrderCombo
	comboSKUs := map[string]bool{}
	for _, c := range comboCodesInOrder {
		comboID, found, err := catalogRepo.FindComboByCode(ctx, c)
		if err != nil {
			return fmt.Errorf("lookup combo: %w", err)
		}
		if !found {
			continue
		}
		comboItems, _, err := r.GetComboItems(ctx, comboID)
		if err != nil {
			return fmt.Errorf("load combo items: %w", err)
		}
		for _, it := range comboItems {
			comboSKUs[it.Product.SKU] = true
		}
		if comboIDPtr == nil {
			comboIDPtr = &comboID
		} else {
			extraCombos = append(extraCombos, models.OrderCombo{ComboID: comboID, ComboQuantity: 1})
		}
	}

	first := rows[0]
	stateName := orderCell(first, col.shipState)
	stateCode, ok := lookupStateCode(stateName)
	if !ok {
		return fmt.Errorf("could not map shipping state %q to a GST state code", stateName)
	}

	// The reference invoice puts "City-Pincode" (no spaces around the
	// hyphen) on its own line, separate from the street address lines —
	// not folded into one comma-joined string.
	addressParts := []string{orderCell(first, col.shipLine1), orderCell(first, col.shipLine2)}
	var nonEmpty []string
	for _, p := range addressParts {
		if p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	address := strings.Join(nonEmpty, ", ")

	cityPin := orderCell(first, col.shipCity)
	if pin := orderCell(first, col.shipPincode); pin != "" {
		cityPin += "-" + pin
	}
	if cityPin != "" {
		if address != "" {
			address += "\n"
		}
		address += cityPin
	}

	paymentCode, paymentLabel := "P1", "PREPAID"
	if orderCell(first, col.cod) == "1" {
		paymentCode, paymentLabel = "COD", "COD"
	}
	// "Dispatch Through" on the reference invoice is the logistics provider
	// name (e.g. "KWIKSHIP", "Delhivery"), not the courier/service-tier
	// string (e.g. "ShadowfaxSurface0.25kg-Direct") — Shipping provider,
	// not Shipping Courier.
	dispatchThrough := orderCell(first, col.shippingProvider)
	if dispatchThrough == "" {
		dispatchThrough = orderCell(first, col.shippingCourier)
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
		PrepaidAmount: prepaidAmount, ExternalInvoiceCode: orderCell(first, col.invoiceCode),
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
		price, _ := strconv.ParseFloat(orderCell(row, col.sellingPrice), 64)
		product, found, err := catalogRepo.FindProductBySKU(ctx, sku)
		if err != nil {
			return fmt.Errorf("lookup product %s: %w", sku, err)
		}
		if !found {
			// Nobody's invoice gets skipped just because a SKU was never
			// added to the Products catalog — create a minimal product
			// from this line item's own data (name, HSN, tax rate) so the
			// order (and every future one reusing this SKU) can be
			// invoiced normally.
			name := orderCell(row, col.itemTypeName)
			if name == "" {
				name = sku
			}
			newID, err := catalogRepo.CreateProduct(ctx, models.Product{
				Name: name, SKU: sku, HSNCode: orderCell(row, col.hsnCode),
				Price: price, TaxRate: gstRateFromTaxTypeCode(orderCell(row, col.gstTaxTypeCode)),
			})
			if err != nil {
				return fmt.Errorf("auto-create product %s: %w", sku, err)
			}
			product.ID = newID
		}
		extraItems = append(extraItems, models.OrderExtraItem{ProductID: product.ID, Quantity: 1, UnitPrice: price})
	}

	id, err := r.CreateOrder(ctx, order, extraItems, extraCombos)
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
	"dadra and nagar haveli and daman and diu": "26", // post-2020 merged UT
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

// gstRateFromTaxTypeCode extracts the percentage from a code like "GST_3"
// or "GST_18" -> 3, 18. Returns 0 for anything else (e.g. blank, "DEFAULT").
func gstRateFromTaxTypeCode(code string) float64 {
	_, numPart, found := strings.Cut(code, "_")
	if !found {
		return 0
	}
	rate, _ := strconv.ParseFloat(numPart, 64)
	return rate
}
