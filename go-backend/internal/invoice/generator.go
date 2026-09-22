// Package invoice computes line-item tax and renders the PDF, reproducing
// the original Node app's Maskyeti-format tax invoice: bordered header grid
// with barcodes, combo-grouped line-item tables, CGST+SGST/IGST breakdown,
// amount in words, declaration, and signatory box.
package invoice

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"invoice-system/internal/companyinfo"
	"invoice-system/internal/gststates"
	"invoice-system/internal/models"
	"invoice-system/internal/orders"
)

type Generator struct {
	repo        *orders.Repository
	invoicesDir string
}

func NewGenerator(repo *orders.Repository, invoicesDir string) *Generator {
	_ = os.MkdirAll(invoicesDir, 0o755)
	return &Generator{repo: repo, invoicesDir: invoicesDir}
}

type lineItem struct {
	name        string
	sku         string
	hsn         string
	qty         float64
	rate        float64
	gross       float64
	discount    float64
	taxable     float64
	taxRate     float64
	taxAmount   float64
	cgst        float64
	sgst        float64
	igst        float64
	totalAmount float64
}

// invoiceNumber is deterministic from the order id, so retrying a failed job
// for the same order always reproduces the same invoice number instead of
// burning a new one — important since invoice numbers are meant to be
// gap-free for GST filing.
func invoiceNumber(orderID int64) string {
	return fmt.Sprintf("CMD%08d", orderID)
}

var unsafeFilenameChars = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// sanitizeFilename lets an external invoice code (e.g. "MHR-26-27-1059053")
// through unchanged in the common case, while still guaranteeing a safe
// filesystem path if a sheet ever contains something stranger (a slash,
// space, etc.).
func sanitizeFilename(s string) string {
	return unsafeFilenameChars.ReplaceAllString(s, "_")
}

// drawCurvedText places each character of text along an elliptical arc
// from startDeg to endDeg (0° = top, clockwise), each one rotated to stay
// tangent to the curve — approximating a rubber-stamp seal's curved
// lettering, which fpdf has no built-in support for.
func drawCurvedText(pdf *fpdf.Fpdf, cx, cy, rx, ry float64, text string, startDeg, endDeg float64) {
	runes := []rune(text)
	n := len(runes)
	if n == 0 {
		return
	}
	for i, r := range runes {
		frac := 0.5
		if n > 1 {
			frac = float64(i) / float64(n-1)
		}
		deg := startDeg + frac*(endDeg-startDeg)
		rad := deg * math.Pi / 180
		x := cx + rx*math.Sin(rad)
		y := cy - ry*math.Cos(rad)
		pdf.TransformBegin()
		pdf.TransformRotate(-deg, x, y)
		pdf.SetXY(x-1.5, y-1.5)
		pdf.CellFormat(3, 3, string(r), "", 0, "C", false, 0, "")
		pdf.TransformEnd()
	}
}

// Generate computes the invoice for order id, writes its PDF to disk, and
// persists the invoice row (idempotently — see CreateInvoiceIfAbsent).
func (g *Generator) Generate(ctx context.Context, orderID int64) (models.Invoice, error) {
	order, err := g.repo.GetOrder(ctx, orderID)
	if err != nil {
		return models.Invoice{}, fmt.Errorf("get order: %w", err)
	}

	var items []models.ComboItem
	var combo models.Combo
	if order.ComboID != nil {
		items, combo, err = g.repo.GetComboItems(ctx, *order.ComboID)
		if err != nil {
			return models.Invoice{}, fmt.Errorf("get combo items: %w", err)
		}
		if len(items) == 0 {
			return models.Invoice{}, fmt.Errorf("combo %d has no items", *order.ComboID)
		}
	}

	extraItems, err := g.repo.GetOrderExtraItems(ctx, orderID)
	if err != nil {
		return models.Invoice{}, fmt.Errorf("get extra items: %w", err)
	}
	if order.ComboID == nil && len(extraItems) == 0 {
		return models.Invoice{}, fmt.Errorf("order %d has no combo and no items", orderID)
	}

	isInterstate := order.CustomerStateCode != "" && order.CustomerStateCode != companyinfo.CompanyStateCode

	lines, sumGross := buildLines(items, order.ComboQuantity)
	discountTotal := comboDiscount(combo, sumGross)
	applyDiscount(lines, sumGross, discountTotal, isInterstate)

	// Extra (standalone) items aren't part of the combo, so no combo
	// discount applies to them — sumGross/discountTotal of 0 makes
	// applyDiscount's proportional-share branch a no-op, leaving discount=0.
	extraLines := buildExtraLines(extraItems)
	applyDiscount(extraLines, 0, 0, isInterstate)

	// A real Unicommerce-issued order already has its own invoice number
	// (Invoice Code) — print that instead of minting our own, since the
	// number on a real GST invoice can't be silently swapped for a
	// different one. Only orders created fresh through this system (no
	// external code) get our own generated CMD######## number.
	invNo := order.ExternalInvoiceCode
	if invNo == "" {
		invNo = invoiceNumber(orderID)
	}
	pdfName := sanitizeFilename(invNo) + ".pdf"
	pdfPath := filepath.Join(g.invoicesDir, pdfName)

	total := 0.0
	for _, l := range lines {
		total += l.totalAmount
	}
	for _, l := range extraLines {
		total += l.totalAmount
	}

	if err := renderPDF(pdfPath, order, combo, invNo, lines, extraLines, total, isInterstate); err != nil {
		return models.Invoice{}, fmt.Errorf("render pdf: %w", err)
	}

	inv := models.Invoice{
		OrderID:       orderID,
		InvoiceNumber: invNo,
		PDFPath:       pdfPath,
		TotalAmount:   total,
	}
	if _, err := g.repo.CreateInvoiceIfAbsent(ctx, inv); err != nil {
		return models.Invoice{}, fmt.Errorf("persist invoice: %w", err)
	}
	return inv, nil
}

func buildLines(items []models.ComboItem, comboQty float64) ([]*lineItem, float64) {
	var lines []*lineItem
	sumGross := 0.0
	for _, it := range items {
		qty := it.Quantity * comboQty
		gross := it.Product.Price * qty
		sumGross += gross
		lines = append(lines, &lineItem{
			name: it.Product.Name, sku: it.Product.SKU, hsn: it.Product.HSNCode,
			qty: qty, rate: it.Product.Price, gross: gross, taxRate: it.Product.TaxRate,
		})
	}
	return lines, sumGross
}

// buildExtraLines builds lineItems for an order's standalone items, using
// each line's own stored unit_price (often far below the product's normal
// catalog price for something like a free gift) rather than product.Price.
func buildExtraLines(items []models.OrderExtraItem) []*lineItem {
	var lines []*lineItem
	for _, it := range items {
		gross := it.UnitPrice * it.Quantity
		lines = append(lines, &lineItem{
			name: it.Product.Name, sku: it.Product.SKU, hsn: it.Product.HSNCode,
			qty: it.Quantity, rate: it.UnitPrice, gross: gross, taxRate: it.Product.TaxRate,
		})
	}
	return lines
}

// appendPrepaidRow adds the reference invoice's spacer row right above the
// Total row, in both the amount table and the GST breakdown table. The
// reference always reserves this row — for a partial-COD order (e.g.
// GoKwik PPCOD) it holds "Prepaid Amount:" and how much was already
// collected online at checkout; for a normal order it's simply blank.
func appendPrepaidRow(rows [][]string, bold []bool, numCols int, prepaidAmount float64) ([][]string, []bool) {
	row := make([]string, numCols)
	if prepaidAmount > 0 {
		row[1] = "Prepaid Amount:"
		row[numCols-1] = money2(prepaidAmount)
	}
	return append(rows, row), append(bold, true)
}

func comboDiscount(combo models.Combo, sumGross float64) float64 {
	switch combo.DiscountType {
	case "percent":
		return sumGross * (combo.DiscountValue / 100)
	case "fixed":
		if combo.DiscountValue < sumGross {
			return combo.DiscountValue
		}
		return sumGross
	default:
		return 0
	}
}

// applyDiscount splits the combo discount across lines proportional to each
// line's own gross amount, derives taxable value/tax back out of the
// GST-inclusive price, then splits that tax into CGST+SGST (intra-state) or
// IGST (inter-state) — same model the original Node invoiceService used.
func applyDiscount(lines []*lineItem, sumGross, discountTotal float64, isInterstate bool) {
	for _, l := range lines {
		if sumGross > 0 {
			l.discount = round2(discountTotal * (l.gross / sumGross))
		}
		l.totalAmount = round2(l.gross - l.discount)
		l.taxable = round2(l.totalAmount / (1 + l.taxRate/100))
		l.taxAmount = round2(l.totalAmount - l.taxable)

		if isInterstate {
			l.igst = l.taxAmount
		} else {
			l.cgst = round2(l.taxAmount / 2)
			l.sgst = round2(l.taxAmount - l.cgst)
		}
	}
}

func round2(f float64) float64 {
	return float64(int64(f*100+0.5)) / 100
}

func formatDate(t time.Time) string {
	return t.Format("02-Jan-2006")
}

// fullAddressBlock appends the state name+code and country, then a blank
// phone line, matching the reference invoice's Bill To/Ship To format —
// e.g. "...JHANSI-284001 Uttar Pradesh (09)\n,India\nT :".
func fullAddressBlock(address, stateCode string) string {
	if name := gststates.Name(stateCode); name != "" {
		address += " " + name + " (" + stateCode + ")"
	}
	return address + "\n,India\nT :"
}

const (
	pageMargin = 10.0 // mm
	pageWidth  = 210.0
	pageRight  = pageWidth - pageMargin
)

func renderPDF(path string, order models.Order, combo models.Combo, invNo string, lines, extraLines []*lineItem, total float64, isInterstate bool) error {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetAutoPageBreak(false, 0)
	pdf.AddPage()

	left := pageMargin
	right := pageRight
	contentWidth := right - left

	pdf.SetFont("Helvetica", "B", 13)
	pdf.SetXY(left, 8)
	pdf.CellFormat(contentWidth, 6, "Tax Invoice", "", 0, "C", false, 0, "")

	// ---- Header block: a 2-row x 3-column grid, like the reference invoice ----
	row1Top := 16.0
	row2Top := 74.0
	row2Bottom := 105.0
	colA := left + contentWidth*0.29
	colB := left + contentWidth*0.635

	orderBarcode := registerBarcode(pdf, "order", order.ShopifyOrderNo)
	awbBarcode := registerBarcode(pdf, "awb", order.AWBNo)

	y := row1Top + 3
	pdf.SetFont("Helvetica", "B", 8)
	pdf.SetXY(left+1.5, y)
	pdf.CellFormat(colA-left-4, 4, "BILL FROM:", "", 0, "L", false, 0, "")
	pdf.SetXY(left+1.5, y+4)
	pdf.CellFormat(colA-left-4, 4, companyinfo.CompanyName, "", 0, "L", false, 0, "")

	pdf.SetFont("Helvetica", "", 7)
	addrH := multiCellHeight(pdf, colA-left-4, 3, companyinfo.CompanyAddress)
	pdf.SetXY(left+1.5, y+8)
	pdf.MultiCell(colA-left-4, 3, companyinfo.CompanyAddress, "", "L", false)
	gstinY := y + 8 + addrH + 0.5
	pdf.SetXY(left+1.5, gstinY)
	pdf.CellFormat(colA-left-4, 3.5, "GSTIN: "+companyinfo.CompanyGSTIN, "", 0, "L", false, 0, "")

	dividerY := gstinY + 5
	pdf.Line(left+1.5, dividerY, colA-1.5, dividerY)

	pdf.SetFont("Helvetica", "B", 8)
	pdf.SetXY(left+1.5, dividerY+2)
	pdf.CellFormat(colA-left-4, 4, "Shipped From:", "", 0, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 7)
	pdf.SetXY(left+1.5, dividerY+6)
	pdf.MultiCell(colA-left-4, 3, companyinfo.ShippedFromAddress, "", "L", false)

	// Invoice No (left half) and Invoice Date (right half) sit side by side
	// at the top of the middle column, matching the reference layout —
	// Order No/Order Date + barcode are centered below, spanning the full
	// column width.
	colBHalf := (colB - colA) / 2
	pdf.SetFont("Helvetica", "", 7)
	pdf.SetXY(colA+1.5, y)
	pdf.CellFormat(colBHalf-3, 3.5, "Invoice No:", "", 0, "L", false, 0, "")
	pdf.SetFont("Helvetica", "B", 7)
	pdf.SetXY(colA+1.5, y+4)
	pdf.CellFormat(colBHalf-3, 3.5, invNo, "", 0, "L", false, 0, "")

	pdf.SetFont("Helvetica", "", 7)
	pdf.SetXY(colA+colBHalf+1.5, y)
	pdf.CellFormat(colBHalf-3, 3.5, "Invoice Date", "", 0, "L", false, 0, "")
	pdf.SetFont("Helvetica", "B", 7)
	pdf.SetXY(colA+colBHalf+1.5, y+4)
	pdf.CellFormat(colBHalf-3, 3.5, formatDate(order.CreatedAt), "", 0, "L", false, 0, "")

	pdf.SetFont("Helvetica", "B", 8)
	pdf.SetXY(colA+1.5, y+10)
	pdf.CellFormat(colB-colA-3, 4, "Order No: "+order.ShopifyOrderNo, "", 0, "C", false, 0, "")
	pdf.SetFont("Helvetica", "", 7)
	pdf.SetXY(colA+1.5, y+14.5)
	pdf.CellFormat(colB-colA-3, 3.5, "Order Date: "+formatDate(order.CreatedAt), "", 0, "C", false, 0, "")
	if orderBarcode != "" {
		barcodeWidth := 40.0
		pdf.ImageOptions(orderBarcode, colA+(colB-colA-barcodeWidth)/2, y+19, barcodeWidth, 8, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
		pdf.SetXY(colA+1.5, y+27.5)
		pdf.CellFormat(colB-colA-3, 3.5, order.ShopifyOrderNo, "", 0, "C", false, 0, "")
	}

	// This block sits vertically centered in its tall column, not pinned to
	// the top like the other two — and "Portal:" is a regular-weight label
	// followed by a bold value, so it's laid out as two adjacent cells
	// (measured and centered as one line) rather than one CellFormat call.
	col3Width := right - colB - 3
	col3CenterY := row1Top + 20
	portalLabel := "Portal: "
	pdf.SetFont("Helvetica", "", 7)
	labelWidth := pdf.GetStringWidth(portalLabel)
	pdf.SetFont("Helvetica", "B", 7)
	valueWidth := pdf.GetStringWidth(order.Portal)
	portalStartX := colB + 1.5 + (col3Width-(labelWidth+valueWidth))/2

	pdf.SetFont("Helvetica", "", 7)
	pdf.SetXY(portalStartX, col3CenterY)
	pdf.CellFormat(labelWidth, 3.5, portalLabel, "", 0, "L", false, 0, "")
	pdf.SetFont("Helvetica", "B", 7)
	pdf.SetXY(portalStartX+labelWidth, col3CenterY)
	pdf.CellFormat(valueWidth, 3.5, order.Portal, "", 0, "L", false, 0, "")

	pdf.SetFont("Helvetica", "", 7)
	pdf.SetXY(colB+1.5, col3CenterY+5)
	pdf.CellFormat(col3Width, 3.5, "Payment Mode: "+order.PaymentModeCode, "", 0, "C", false, 0, "")
	pdf.SetFont("Helvetica", "B", 7)
	pdf.SetXY(colB+1.5, col3CenterY+8.5)
	pdf.CellFormat(col3Width, 3.5, order.PaymentModeLabel, "", 0, "C", false, 0, "")

	// ---- Bill To / Ship To / Dispatch (same 3 columns as the row above) ----
	y = row2Top + 3
	pdf.SetFont("Helvetica", "B", 8)
	pdf.SetXY(left+1.5, y)
	pdf.CellFormat(colA-left-4, 3.5, "Bill To:", "", 0, "L", false, 0, "")
	pdf.SetXY(left+1.5, y+4)
	pdf.CellFormat(colA-left-4, 3.5, order.CustomerName, "", 0, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 7)
	pdf.SetXY(left+1.5, y+8)
	pdf.MultiCell(colA-left-4, 3, fullAddressBlock(order.CustomerAddress, order.CustomerStateCode), "", "L", false)

	pdf.SetFont("Helvetica", "B", 8)
	pdf.SetXY(colA+1.5, y)
	pdf.CellFormat(colB-colA-3, 3.5, "Ship To:", "", 0, "L", false, 0, "")
	shipName := order.ShippingName
	if shipName == "" {
		shipName = order.CustomerName
	}
	pdf.SetXY(colA+1.5, y+4)
	pdf.CellFormat(colB-colA-3, 3.5, shipName, "", 0, "L", false, 0, "")
	shipAddr := order.ShippingAddress
	if shipAddr == "" {
		shipAddr = order.CustomerAddress
	}
	pdf.SetFont("Helvetica", "", 7)
	pdf.SetXY(colA+1.5, y+8)
	pdf.MultiCell(colB-colA-3, 3, fullAddressBlock(shipAddr, order.CustomerStateCode), "", "L", false)

	pdf.SetFont("Helvetica", "", 7)
	pdf.SetXY(colB+1.5, y)
	pdf.CellFormat(col3Width, 3.5, "Dispatch Through", "", 0, "C", false, 0, "")
	pdf.SetFont("Helvetica", "B", 7)
	pdf.SetXY(colB+1.5, y+4)
	pdf.CellFormat(col3Width, 3.5, order.DispatchThrough, "", 0, "C", false, 0, "")
	pdf.SetFont("Helvetica", "", 7)
	pdf.SetXY(colB+1.5, y+8)
	pdf.CellFormat(col3Width, 3.5, "AWB No", "", 0, "C", false, 0, "")
	pdf.SetXY(colB+1.5, y+11.5)
	pdf.CellFormat(col3Width, 3.5, order.AWBNo, "", 0, "C", false, 0, "")
	if awbBarcode != "" {
		pdf.ImageOptions(awbBarcode, colB+1.5, y+15.5, col3Width-2, 8, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
		pdf.SetXY(colB+1.5, y+24)
		pdf.CellFormat(col3Width, 3.5, order.AWBNo, "", 0, "C", false, 0, "")
	}

	// ---- Grid lines for the whole header block ----
	pdf.Rect(left, row1Top, contentWidth, row2Bottom-row1Top, "D")
	pdf.Line(left, row2Top, right, row2Top)
	pdf.Line(colA, row1Top, colA, row2Bottom)
	pdf.Line(colB, row1Top, colB, row2Bottom)
	// A divider under Invoice No/Invoice Date, separating them from the
	// Order No/barcode + Portal content below — spans columns B and C only
	// (column A has its own separate BILL FROM/Shipped From divider).
	pdf.Line(colA, row1Top+11, right, row1Top+11)

	y = row2Bottom + 4

	// ---- Table 1: gross amount / amount, grouped by combo ----
	// No "Discount" column here — the reference invoice folds any combo
	// discount straight into Amount (INR) rather than showing it separately.
	t1Cols := []tableColumn{
		{"Sr No.", 8, "L"}, {"Product Name", 42, "L"}, {"Product Code.", 22, "L"}, {"HSN Code", 18, "L"},
		{"Qty", 9, "R"}, {"Rate", 21, "R"}, {"Gross Amount- Incl GST (INR)", 25, "R"},
		{"Store Credit", 18, "R"}, {"Amount (INR)", 27, "R"},
	}

	// The reference invoice's Total row counts combo SETS ordered (i.e.
	// order.ComboQuantity), not the sum of each component's own per-set
	// quantity — a 1-set combo of 2 different components totals Qty 1, not
	// 2. Standalone extra items are real separate order lines, so their
	// quantities do sum normally.
	totalQty := 0.0
	if order.ComboID != nil {
		totalQty += order.ComboQuantity
	}
	for _, l := range extraLines {
		totalQty += l.qty
	}

	t1Rows, t1Bold := buildGroupedRows(combo, order, lines, extraLines, func(l *lineItem, code string) []string {
		return []string{"", l.name, code, l.hsn, fmt.Sprintf("%.0f", l.qty), money2(l.rate), money2(l.gross), "0.00", money2(l.totalAmount)}
	})
	t1Rows, t1Bold = appendPrepaidRow(t1Rows, t1Bold, len(t1Cols), order.PrepaidAmount)
	y = drawTable(pdf, left, y, t1Cols, t1Rows, t1Bold,
		[]string{"", "", "", "", fmt.Sprintf("%.0f", totalQty), "", "", "", money2(total)}, 1, "Total:")

	y += 6

	// ---- Table 2: taxable value / GST breakdown, grouped by combo ----
	pdf.SetFont("Helvetica", "", 8)
	pdf.SetXY(left, y)
	pdf.CellFormat(contentWidth, 4, "*Breakdown of Invoice Value is as follows", "", 1, "L", false, 0, "")
	y = pdf.GetY() + 2

	var t2Cols []tableColumn
	if isInterstate {
		t2Cols = []tableColumn{
			{"Sr No.", 8, "L"}, {"Product Name", 42, "L"}, {"Product Code.", 22, "L"}, {"HSN Code", 18, "L"},
			{"Qty", 9, "R"}, {"Taxable Value (INR)", 30, "R"}, {"IGST (INR)", 31, "R"}, {"Amount (INR)", 30, "R"},
		}
	} else {
		t2Cols = []tableColumn{
			{"Sr No.", 8, "L"}, {"Product Name", 42, "L"}, {"Product Code.", 22, "L"}, {"HSN Code", 18, "L"},
			{"Qty", 9, "R"}, {"Taxable Value (INR)", 25, "R"}, {"CGST (INR)", 22, "R"}, {"SGST (INR)", 22, "R"}, {"Amount (INR)", 22, "R"},
		}
	}

	totalTaxable, totalCgst, totalSgst, totalIgst := 0.0, 0.0, 0.0, 0.0
	for _, l := range append(append([]*lineItem{}, lines...), extraLines...) {
		totalTaxable += l.taxable
		totalCgst += l.cgst
		totalSgst += l.sgst
		totalIgst += l.igst
	}

	t2Rows, t2Bold := buildGroupedRows(combo, order, lines, extraLines, func(l *lineItem, code string) []string {
		if isInterstate {
			return []string{"", l.name, code, l.hsn, fmt.Sprintf("%.0f", l.qty), money2(l.taxable),
				fmt.Sprintf("%s (%.3f%%)", money2(l.igst), l.taxRate), money2(l.totalAmount)}
		}
		return []string{"", l.name, code, l.hsn, fmt.Sprintf("%.0f", l.qty), money2(l.taxable),
			fmt.Sprintf("%s (%.3f%%)", money2(l.cgst), l.taxRate/2), fmt.Sprintf("%s (%.3f%%)", money2(l.sgst), l.taxRate/2), money2(l.totalAmount)}
	})
	t2Rows, t2Bold = appendPrepaidRow(t2Rows, t2Bold, len(t2Cols), order.PrepaidAmount)

	var totalRow2 []string
	if isInterstate {
		totalRow2 = []string{"", "", "", "", fmt.Sprintf("%.0f", totalQty), money2(totalTaxable), money2(totalIgst), money2(total)}
	} else {
		totalRow2 = []string{"", "", "", "", fmt.Sprintf("%.0f", totalQty), money2(totalTaxable), money2(totalCgst), money2(totalSgst), money2(total)}
	}
	y = drawTable(pdf, left, y, t2Cols, t2Rows, t2Bold, totalRow2, 1, "Total:")

	y += 6
	pdf.SetFont("Helvetica", "B", 8)
	pdf.SetXY(left, y)
	pdf.CellFormat(contentWidth-15, 4, "Amount Chargeable (in words)", "", 0, "L", false, 0, "")
	pdf.CellFormat(15, 4, "E. & O.E", "", 1, "R", false, 0, "")
	pdf.SetXY(left, pdf.GetY()+0.5)
	pdf.MultiCell(contentWidth, 4, "INR "+amountInWordsINR(total), "", "L", false)
	y = pdf.GetY() + 2

	pdf.SetFont("Helvetica", "B", 8)
	pdf.SetXY(left, y)
	pdf.CellFormat(contentWidth, 4, "Tax is payable on reverse charge basis: No", "", 1, "L", false, 0, "")
	y = pdf.GetY() + 1
	pdf.Line(left, y, right, y)
	y += 3

	declarationWidth := contentWidth * 0.6
	pdf.SetFont("Helvetica", "BU", 8)
	pdf.SetXY(left, y)
	pdf.CellFormat(declarationWidth, 4, "Declaration", "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 7)
	pdf.SetXY(left, y+5)
	pdf.MultiCell(declarationWidth, 3.2,
		"1.This is a computer generated Invoice.Doesnt require signature or stamp. "+
			"2. All figures are showing in INR "+
			"3. Ship/Handling Charges inclusive of GST "+
			"4. All Disputes are subject to "+companyinfo.Jurisdiction+" jurisdiction only.",
		"", "L", false)

	boxX := left + declarationWidth + 8
	boxWidth := right - boxX
	pdf.Rect(boxX, y, boxWidth, 28, "D")
	pdf.SetFont("Helvetica", "B", 8)
	pdf.SetXY(boxX, y+3)
	pdf.CellFormat(boxWidth, 4, "For "+companyinfo.CompanyName, "", 0, "C", false, 0, "")

	// A faint oval company-seal placeholder, with the company name curved
	// around its rim like a real rubber stamp, matching the reference
	// invoice's layout.
	sealCX, sealCY := boxX+boxWidth/2, y+13.0
	pdf.SetDrawColor(160, 160, 160)
	pdf.SetTextColor(160, 160, 160)
	pdf.Ellipse(sealCX, sealCY, 15, 7, 0, "D")
	pdf.SetFont("Helvetica", "", 4.5)
	drawCurvedText(pdf, sealCX, sealCY, 13, 5.5, strings.ToUpper(companyinfo.CompanyName), -100, 100)
	pdf.SetFont("Helvetica", "", 4.5)
	drawCurvedText(pdf, sealCX, sealCY, 13, 5.5, "PVT LTD SEAL", 130, 230)
	pdf.SetTextColor(0, 0, 0)
	pdf.SetDrawColor(0, 0, 0)

	pdf.SetFont("Helvetica", "", 7)
	pdf.SetXY(boxX, y+22)
	pdf.CellFormat(boxWidth, 4, "Authorised Signatory", "", 0, "C", false, 0, "")

	if companyinfo.FulfillmentPlatform != "" {
		footerY := 285.0
		pdf.Rect(left, footerY, contentWidth, 10, "D")
		pdf.SetFont("Helvetica", "B", 8)
		pdf.SetXY(left+3, footerY+1.5)
		pdf.CellFormat(30, 4, "Bill By:", "", 0, "L", false, 0, "")

		// "Powered By", then a pair of overlapping gray ellipses standing
		// in for the platform's actual logo mark (we have no image asset
		// for it), then its lowercase wordmark name — all on one line,
		// matching the reference.
		textX := left + 3
		pdf.SetFont("Helvetica", "", 7)
		pdf.SetXY(textX, footerY+5.5)
		pdf.CellFormat(18, 4, "Powered By", "", 0, "L", false, 0, "")

		iconX, iconY := textX+15, footerY+6.5
		pdf.SetDrawColor(130, 130, 130)
		pdf.SetLineWidth(0.3)
		pdf.Ellipse(iconX+1.2, iconY, 1.2, 1.9, 0, "D")
		pdf.Ellipse(iconX+2.5, iconY, 1.2, 1.9, 0, "D")
		pdf.SetLineWidth(0.2)
		pdf.SetDrawColor(0, 0, 0)

		pdf.SetTextColor(110, 110, 110)
		pdf.SetXY(iconX+4.5, footerY+5.5)
		pdf.CellFormat(30, 4, strings.ToLower(companyinfo.FulfillmentPlatform), "", 0, "L", false, 0, "")
		pdf.SetTextColor(0, 0, 0)

		pdf.SetXY(left, footerY+5.5)
		pdf.CellFormat(contentWidth, 4, "This is a computer generated Invoice", "", 0, "C", false, 0, "")
	}

	return pdf.OutputFileAndClose(path)
}

func money2(f float64) string {
	return fmt.Sprintf("%.2f", f)
}

// multiCellHeight measures how tall text would render at the given width
// with the pdf's currently-set font, without actually drawing it — used to
// stack the next block right below variable-height wrapped text.
func multiCellHeight(pdf *fpdf.Fpdf, width, lineHeight float64, text string) float64 {
	lines := pdf.SplitLines([]byte(text), width)
	return float64(len(lines)) * lineHeight
}

// buildGroupedRows mirrors the reference invoice's bundle layout: a bold
// combo-header row (name/code/number of sets, no money columns) followed by
// each real combo product indented below it with its code in parentheses,
// then any standalone extra items (e.g. a "FREE GIFT" bundled onto this
// specific order, or — for an order with no combo at all — its only items)
// as their own full top-level Sr rows — not indented, since they aren't
// part of the combo.
func buildGroupedRows(combo models.Combo, order models.Order, lines []*lineItem, extraLines []*lineItem, rowFor func(*lineItem, string) []string) ([][]string, []bool) {
	var rows [][]string
	var bold []bool

	sr := 1
	if len(lines) > 0 {
		header := rowFor(lines[0], "")
		blankHeader := make([]string, len(header))
		blankHeader[0] = "1"
		blankHeader[1] = combo.Name
		code := combo.Code
		if code == "" {
			code = "-"
		}
		blankHeader[2] = code
		blankHeader[3] = ""
		blankHeader[4] = fmt.Sprintf("%.0f", order.ComboQuantity)
		for i := 5; i < len(blankHeader); i++ {
			blankHeader[i] = ""
		}
		rows = append(rows, blankHeader)
		bold = append(bold, true)

		for _, l := range lines {
			row := rowFor(l, fmt.Sprintf("(%s)", l.sku))
			rows = append(rows, row)
			bold = append(bold, false)
		}
		sr = 2
	}

	for _, l := range extraLines {
		row := rowFor(l, l.sku)
		row[0] = fmt.Sprintf("%d", sr)
		rows = append(rows, row)
		bold = append(bold, false)
		sr++
	}
	return rows, bold
}
