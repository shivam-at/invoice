package invoice

import (
	"github.com/go-pdf/fpdf"
)

type tableColumn struct {
	Label string
	Width float64
	Align string // "L", "C", "R"
}

const (
	tableHeaderHeight = 8.0
	tableMinRowHeight = 5.0
	tableLineHeight   = 2.6
	tableCellPad      = 1.0
	// pageBreakLimit is how far down the page a table may draw before
	// wrapping onto a fresh page. A multi-combo order can have far more
	// line items than a single-combo one, easily growing a table taller
	// than one page's remaining space — without this, later content
	// (the second table, declaration, footer) drew right on top of
	// whatever rows the first table hadn't finished yet.
	pageBreakLimit = 275.0
)

// drawTable renders a bordered table with a wrapping header row, wrapping
// data rows (any row can be rendered bold via boldRows, used for the
// combo-header rows), and an optional total row. Row height grows to fit
// whatever text a cell holds, so a long combo name never overlaps the row
// below it. If a row (or the total row) would run past pageBreakLimit, a
// new page starts first, with the column headers repeated at its top.
// Returns the y position immediately below the table (on whichever page it
// ended up finishing on).
func drawTable(pdf *fpdf.Fpdf, x, y float64, columns []tableColumn, rows [][]string, boldRows []bool,
	totalRow []string, totalLabelCol int, totalLabel string) float64 {

	drawHeader := func(atY float64) float64 {
		pdf.SetFont("Helvetica", "B", 6.5)
		cx := x
		for _, col := range columns {
			pdf.Rect(cx, atY, col.Width, tableHeaderHeight, "D")
			pdf.SetXY(cx+tableCellPad, atY+0.5)
			pdf.MultiCell(col.Width-2*tableCellPad, tableLineHeight, col.Label, "", alignCode(col.Align), false)
			cx += col.Width
		}
		return atY + tableHeaderHeight
	}

	rowY := drawHeader(y)

	for r, row := range rows {
		style := ""
		if boldRows != nil && r < len(boldRows) && boldRows[r] {
			style = "B"
		}
		pdf.SetFont("Helvetica", style, 6.5)

		rowHeight := tableMinRowHeight
		for i, col := range columns {
			h := multiCellHeight(pdf, col.Width-2*tableCellPad, tableLineHeight, row[i]) + 1
			if h > rowHeight {
				rowHeight = h
			}
		}

		if rowY+rowHeight > pageBreakLimit {
			pdf.AddPage()
			rowY = drawHeader(pageMargin)
			pdf.SetFont("Helvetica", style, 6.5)
		}

		cx := x
		for i, col := range columns {
			pdf.Rect(cx, rowY, col.Width, rowHeight, "D")
			pdf.SetXY(cx+tableCellPad, rowY+0.5)
			pdf.MultiCell(col.Width-2*tableCellPad, tableLineHeight, row[i], "", alignCode(col.Align), false)
			cx += col.Width
		}
		rowY += rowHeight
	}

	if totalRow != nil {
		if rowY+tableMinRowHeight > pageBreakLimit {
			pdf.AddPage()
			rowY = drawHeader(pageMargin)
		}
		pdf.SetFont("Helvetica", "B", 6.5)
		cx := x
		for i, col := range columns {
			text := totalRow[i]
			if i == totalLabelCol && totalLabel != "" {
				text = totalLabel
			}
			pdf.Rect(cx, rowY, col.Width, tableMinRowHeight, "D")
			pdf.SetXY(cx+tableCellPad, rowY+0.5)
			pdf.MultiCell(col.Width-2*tableCellPad, tableLineHeight, text, "", alignCode(col.Align), false)
			cx += col.Width
		}
		rowY += tableMinRowHeight
	}

	return rowY
}

func alignCode(a string) string {
	switch a {
	case "R":
		return "R"
	case "C":
		return "C"
	default:
		return "L"
	}
}
