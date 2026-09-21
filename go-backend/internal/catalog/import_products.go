package catalog

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"invoice-system/internal/models"
)

// productColumnAliases mirrors COLUMN_ALIASES in the Node app's products.js
// — header matching is case-insensitive and tries every known alias per
// field, since the source spreadsheet's exact column names aren't
// guaranteed to be stable.
var productColumnAliases = map[string][]string{
	"name":          {"name", "product name", "product"},
	"sku":           {"sku", "product code", "code"},
	"hsn_code":      {"hsn", "hsn code", "hsn_code"},
	"price":         {"price", "rate", "mrp", "selling price"},
	"tax_rate":      {"tax_rate", "tax rate", "gst", "gst%", "gst rate", "gst tax type code"},
	"category_code": {"category code", "category_code"},
}

const comboCategoryCode = "CMB"

func buildColumnIndex(header []string) map[string]int {
	normalized := make([]string, len(header))
	for i, h := range header {
		normalized[i] = strings.ToLower(strings.TrimSpace(h))
	}
	index := map[string]int{}
	for field, aliases := range productColumnAliases {
		for pos, h := range normalized {
			if containsStr(aliases, h) {
				index[field] = pos
				break
			}
		}
	}
	return index
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

var numericCleanup = regexp.MustCompile(`[^0-9.\-]`)

func parseNumber(raw string) (float64, bool) {
	cleaned := numericCleanup.ReplaceAllString(strings.TrimSpace(raw), "")
	if cleaned == "" {
		return 0, false
	}
	n, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

type SkipReason struct {
	Row    int    `json:"row"`
	Reason string `json:"reason"`
}

type ProductImportResult struct {
	Created   int          `json:"created"`
	Updated   int          `json:"updated"`
	Skipped   []SkipReason `json:"skipped"`
	TotalRows int          `json:"totalRows"`
}

// ImportProductsFromRows upserts by SKU (import again updates instead of
// duplicating) and skips CMB-category rows outright — those are combos,
// imported separately via ImportCombosFromSheet/AutoGuessFromSheet.
func (r *Repository) ImportProductsFromRows(ctx context.Context, rows [][]string) (ProductImportResult, error) {
	if len(rows) < 2 {
		return ProductImportResult{}, fmt.Errorf("sheet has no data rows below the header")
	}
	col := buildColumnIndex(rows[0])
	nameIdx, hasName := col["name"]
	priceIdx, hasPrice := col["price"]
	if !hasName || !hasPrice {
		return ProductImportResult{}, fmt.Errorf("could not find 'name' and 'price' columns in the sheet header")
	}

	result := ProductImportResult{TotalRows: len(rows) - 1, Skipped: []SkipReason{}}

	for i := 1; i < len(rows); i++ {
		row := rows[i]
		rowNum := i + 1

		categoryCode := ""
		if idx, ok := col["category_code"]; ok && idx < len(row) {
			categoryCode = strings.ToUpper(strings.TrimSpace(row[idx]))
		}
		if categoryCode == comboCategoryCode {
			result.Skipped = append(result.Skipped, SkipReason{Row: rowNum, Reason: "combo item — import combos separately from Combo Deals page"})
			continue
		}

		name := cellAt(row, nameIdx)
		priceRaw := cellAt(row, priceIdx)
		if name == "" || priceRaw == "" {
			result.Skipped = append(result.Skipped, SkipReason{Row: rowNum, Reason: "missing name or price"})
			continue
		}
		price, ok := parseNumber(priceRaw)
		if !ok {
			result.Skipped = append(result.Skipped, SkipReason{Row: rowNum, Reason: "price is not a number"})
			continue
		}

		sku := ""
		if idx, ok := col["sku"]; ok {
			sku = cellAt(row, idx)
		}
		hsn := ""
		if idx, ok := col["hsn_code"]; ok {
			hsn = cellAt(row, idx)
		}
		taxRate := 0.0
		if idx, ok := col["tax_rate"]; ok {
			taxRate, _ = parseNumber(cellAt(row, idx))
		}

		product := models.Product{Name: name, SKU: sku, HSNCode: hsn, Price: price, TaxRate: taxRate, CategoryCode: categoryCode}

		if sku != "" {
			existing, found, err := r.FindProductBySKU(ctx, sku)
			if err != nil {
				return result, err
			}
			if found {
				if err := r.UpdateProduct(ctx, existing.ID, product); err != nil {
					return result, err
				}
				result.Updated++
				continue
			}
		}
		if _, err := r.CreateProduct(ctx, product); err != nil {
			return result, err
		}
		result.Created++
	}

	return result, nil
}

func cellAt(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}
