// Combo auto-detection, ported from combos.js. Rows with Category Code
// "CMB" (soft combo) or "KIT" (hard combo — pre-packed as one physical
// unit, but coded and resolved identically) are combos; their Product Code
// encodes component SKUs as trailing numeric suffixes joined by underscore
// (e.g. CMB_0047_0196 bundles whichever two products already in the catalog
// have SKUs ending in _0047 and _0196). A combo's discount is reconstructed as
// (sum of component prices - combo MRP). Combos where a suffix doesn't
// resolve to exactly one product are left for manual/heuristic resolution
// rather than guessed at blindly, since this feeds real invoices.
package catalog

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"

	"invoice-system/internal/models"
)

func money(f float64) float64 {
	return math.Round(f*100) / 100
}

// dedupeWithQuantity collapses repeated products (e.g. a combo code that
// encodes 2x of the same component via a repeated suffix) into one
// combo_items row per product with quantity = occurrence count, since
// combo_items has a UNIQUE(combo_id, product_id) constraint — inserting the
// same product twice as two separate rows would violate it.
func dedupeWithQuantity(products []models.Product) (ids []int64, qty map[int64]float64) {
	qty = map[int64]float64{}
	seen := map[int64]bool{}
	for _, p := range products {
		qty[p.ID]++
		if !seen[p.ID] {
			seen[p.ID] = true
			ids = append(ids, p.ID)
		}
	}
	return ids, qty
}

type comboSheetRow struct {
	code string
	name string
	mrp  float64
}

type comboColumnIndex struct {
	category, code, name, mrp int
}

func findComboColumns(header []string) (comboColumnIndex, error) {
	normalized := make([]string, len(header))
	for i, h := range header {
		normalized[i] = strings.ToLower(strings.TrimSpace(h))
	}
	idx := comboColumnIndex{
		category: indexOf(normalized, "category code"),
		code:     indexOf(normalized, "product code"),
		name:     indexOf(normalized, "name"),
		mrp:      indexOf(normalized, "mrp"),
	}
	if idx.category == -1 || idx.code == -1 || idx.name == -1 || idx.mrp == -1 {
		return idx, fmt.Errorf("sheet must have Category Code, Product Code, Name, and MRP columns")
	}
	return idx, nil
}

func indexOf(list []string, target string) int {
	for i, v := range list {
		if v == target {
			return i
		}
	}
	return -1
}

// loadComboRowsFromSheet groups CMB rows by Product Code, first occurrence
// wins (de-duped), mirroring loadComboRowsFromSheet() in the Node app.
func loadComboRowsFromSheet(rows [][]string) (map[string]comboSheetRow, error) {
	if len(rows) < 2 {
		return nil, fmt.Errorf("sheet has no data rows below the header")
	}
	idx, err := findComboColumns(rows[0])
	if err != nil {
		return nil, err
	}

	byCode := map[string]comboSheetRow{}
	for _, row := range rows[1:] {
		category := strings.ToUpper(strings.TrimSpace(cellAt(row, idx.category)))
		code := cellAt(row, idx.code)
		if !isComboCategoryCode(category) || code == "" {
			continue
		}
		if _, exists := byCode[code]; exists {
			continue
		}
		mrp, _ := parseNumber(cellAt(row, idx.mrp))
		byCode[code] = comboSheetRow{code: code, name: cellAt(row, idx.name), mrp: mrp}
	}
	return byCode, nil
}

var leadingCMBPattern = regexp.MustCompile(`(?i)^CMB_`)

func parseSuffixes(code string) []string {
	stripped := leadingCMBPattern.ReplaceAllString(code, "")
	parts := strings.Split(stripped, "_")
	var suffixes []string
	for _, p := range parts {
		if isAllDigits(p) {
			suffixes = append(suffixes, p)
		}
	}
	return suffixes
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

type CreatedCombo struct {
	Code               string   `json:"code"`
	Name               string   `json:"name"`
	ComboMRP           float64  `json:"comboMrp"`
	SumComponentPrices float64  `json:"sumComponentPrices"`
	DiscountValue      float64  `json:"discountValue"`
	Components         []string `json:"components"`
	Confident          bool     `json:"confident,omitempty"`
}

type ComboImportResult struct {
	Created []CreatedCombo `json:"created"`
	Skipped []SkipReason   `json:"skipped"`
}

// ImportCombosFromSheet auto-creates every CMB combo whose suffixes each
// resolve to exactly one product. Anything ambiguous is skipped here and
// surfaced instead by UnresolvedFromSheet / AutoGuessFromSheet.
func (r *Repository) ImportCombosFromSheet(ctx context.Context, rows [][]string) (ComboImportResult, error) {
	byCode, err := loadComboRowsFromSheet(rows)
	if err != nil {
		return ComboImportResult{}, err
	}
	bySuffix, err := r.AllProductsBySuffix(ctx)
	if err != nil {
		return ComboImportResult{}, err
	}

	result := ComboImportResult{Created: []CreatedCombo{}, Skipped: []SkipReason{}}
	for code, row := range byCode {
		if _, found, _ := r.FindComboByCode(ctx, code); found {
			result.Skipped = append(result.Skipped, SkipReason{Reason: "combo already exists"})
			continue
		}
		suffixes := parseSuffixes(code)
		if len(suffixes) < 2 {
			result.Skipped = append(result.Skipped, SkipReason{Reason: fmt.Sprintf("%s: could not parse component codes from product code", code)})
			continue
		}

		var resolved []models.Product
		unresolvedReason := ""
		for _, suffix := range suffixes {
			matches := bySuffix[suffix]
			if len(matches) != 1 {
				unresolvedReason = fmt.Sprintf("%s: component suffix _%s matched %d products (need exactly 1)", code, suffix, len(matches))
				break
			}
			resolved = append(resolved, matches[0])
		}
		if unresolvedReason != "" {
			result.Skipped = append(result.Skipped, SkipReason{Reason: unresolvedReason})
			continue
		}

		sumPrices := 0.0
		skus := make([]string, 0, len(resolved))
		for _, p := range resolved {
			sumPrices += p.Price
			skus = append(skus, p.SKU)
		}
		productIDs, qtyByProduct := dedupeWithQuantity(resolved)
		discount := math.Max(0, money(sumPrices-row.mrp))

		name := row.name
		if name == "" {
			name = code
		}
		if _, err := r.CreateCombo(ctx, models.Combo{Name: name, Code: code, DiscountType: "fixed", DiscountValue: discount}, productIDs, qtyByProduct); err != nil {
			result.Skipped = append(result.Skipped, SkipReason{Reason: fmt.Sprintf("%s: %v", code, err)})
			continue
		}
		result.Created = append(result.Created, CreatedCombo{
			Code: code, Name: name, ComboMRP: row.mrp, SumComponentPrices: money(sumPrices), DiscountValue: discount, Components: skus,
		})
	}
	return result, nil
}

type UnresolvedSuffix struct {
	Suffix     string           `json:"suffix"`
	Candidates []models.Product `json:"candidates"`
}

type UnresolvedCombo struct {
	Code     string             `json:"code"`
	Name     string             `json:"name"`
	MRP      float64            `json:"mrp"`
	Suffixes []UnresolvedSuffix `json:"suffixes"`
}

// UnresolvedFromSheet is read-only: it reports exactly which CMB codes
// ImportCombosFromSheet could not auto-create, and the real candidate
// products for each ambiguous suffix, so a human (or AutoGuessFromSheet)
// can pick the right one instead of the system guessing blindly.
func (r *Repository) UnresolvedFromSheet(ctx context.Context, rows [][]string) ([]UnresolvedCombo, error) {
	byCode, err := loadComboRowsFromSheet(rows)
	if err != nil {
		return nil, err
	}
	bySuffix, err := r.AllProductsBySuffix(ctx)
	if err != nil {
		return nil, err
	}

	unresolved := []UnresolvedCombo{}
	for code, row := range byCode {
		if _, found, _ := r.FindComboByCode(ctx, code); found {
			continue
		}
		suffixes := parseSuffixes(code)
		if len(suffixes) < 2 {
			continue
		}
		allUnique := true
		for _, s := range suffixes {
			if len(bySuffix[s]) != 1 {
				allUnique = false
				break
			}
		}
		if allUnique {
			continue // ImportCombosFromSheet already handles this one
		}

		name := row.name
		if name == "" {
			name = code
		}
		uc := UnresolvedCombo{Code: code, Name: name, MRP: row.mrp, Suffixes: []UnresolvedSuffix{}}
		for _, s := range suffixes {
			candidates := bySuffix[s]
			if candidates == nil {
				candidates = []models.Product{}
			}
			uc.Suffixes = append(uc.Suffixes, UnresolvedSuffix{Suffix: s, Candidates: candidates})
		}
		unresolved = append(unresolved, uc)
	}
	return unresolved, nil
}

// --- Auto-guess heuristic (only used when a suffix is genuinely ambiguous) ---

// nameStopwords mirrors NAME_STOPWORDS in the Node app exactly, including
// the generic marketing words added after a real false-positive: "Self Care
// Love Combo" was matching "Self Adhesive Tape" purely because both contain
// "self".
var nameStopwords = map[string]bool{}

func init() {
	for _, w := range []string{
		"combo", "deal", "with", "from", "this", "that", "free", "gift", "pack", "set",
		"origin", "nepal", "the", "and", "for", "pieces", "piece",
		"self", "care", "pure", "fine", "best", "special", "premium", "natural",
		"design", "original", "authentic", "certified", "royal", "classic",
		"mini", "small", "large", "new", "edition", "style", "energised",
		"energized", "blessed", "sacred", "divine", "power", "energy",
	} {
		nameStopwords[w] = true
	}
}

// minPlausibleComponentPrice: below this, an item is almost certainly a
// packaging/filler SKU (tape, box, key chain filler) rather than a genuine
// bundled product — a soft filter, never excluding every candidate.
const minPlausibleComponentPrice = 150

var nonAlnumPattern = regexp.MustCompile(`[^a-z0-9\s]`)

func significantWords(text string) []string {
	cleaned := nonAlnumPattern.ReplaceAllString(strings.ToLower(text), " ")
	var words []string
	for _, w := range strings.Fields(cleaned) {
		if len(w) > 3 && !nameStopwords[w] {
			words = append(words, w)
		}
	}
	return words
}

func priceScore(sum, mrp float64) float64 {
	if sum <= 0 {
		return -1000
	}
	discountPct := (sum - mrp) / sum * 100
	if discountPct < -20 || discountPct > 70 {
		return -500 - math.Abs(discountPct)
	}
	return -math.Abs(discountPct - 12)
}

// findBestGuessCombination brute-forces every combination across the
// suffixes' candidate lists (each list is small — real combos have 2-4
// components), scoring name-overlap with the combo's own name far above
// price plausibility, matching the Node app's findBestGuessCombination().
func findBestGuessCombination(suffixCandidatesRaw [][]models.Product, mrp float64, comboName string) (chosen []models.Product, confident bool) {
	comboWords := significantWords(comboName)

	suffixCandidates := make([][]models.Product, len(suffixCandidatesRaw))
	for i, list := range suffixCandidatesRaw {
		var above []models.Product
		for _, p := range list {
			if p.Price >= minPlausibleComponentPrice {
				above = append(above, p)
			}
		}
		if len(above) > 0 {
			suffixCandidates[i] = above
		} else {
			suffixCandidates[i] = list
		}
	}

	bestScore := math.Inf(-1)
	var best []models.Product
	bestNameScore := 0

	var recurse func(i int, current []models.Product, sumPrice float64, nameScore int)
	recurse = func(i int, current []models.Product, sumPrice float64, nameScore int) {
		if i == len(suffixCandidates) {
			score := float64(nameScore)*1000 + priceScore(sumPrice, mrp)
			if score > bestScore {
				bestScore = score
				best = append([]models.Product{}, current...)
				bestNameScore = nameScore
			}
			return
		}
		for _, candidate := range suffixCandidates[i] {
			match := 0
			lowerName := strings.ToLower(candidate.Name)
			for _, w := range comboWords {
				if strings.Contains(lowerName, w) {
					match = 1
					break
				}
			}
			recurse(i+1, append(current, candidate), sumPrice+candidate.Price, nameScore+match)
		}
	}
	recurse(0, nil, 0, 0)

	return best, bestNameScore > 0
}

// AutoGuessFromSheet creates a combo for every still-ambiguous CMB code
// using the heuristic above, and creates them immediately — but tags every
// one's description as UNVERIFIED so it's clearly flagged for manual review
// before being trusted on a real invoice.
func (r *Repository) AutoGuessFromSheet(ctx context.Context, rows [][]string) (ComboImportResult, error) {
	byCode, err := loadComboRowsFromSheet(rows)
	if err != nil {
		return ComboImportResult{}, err
	}
	bySuffix, err := r.AllProductsBySuffix(ctx)
	if err != nil {
		return ComboImportResult{}, err
	}

	result := ComboImportResult{Created: []CreatedCombo{}, Skipped: []SkipReason{}}
	for code, row := range byCode {
		if _, found, _ := r.FindComboByCode(ctx, code); found {
			result.Skipped = append(result.Skipped, SkipReason{Reason: fmt.Sprintf("%s: combo already exists", code)})
			continue
		}
		suffixes := parseSuffixes(code)
		if len(suffixes) < 2 {
			result.Skipped = append(result.Skipped, SkipReason{Reason: fmt.Sprintf("%s: could not parse component codes from product code", code)})
			continue
		}

		candidateLists := make([][]models.Product, len(suffixes))
		zeroMatch := false
		for i, s := range suffixes {
			candidateLists[i] = bySuffix[s]
			if len(candidateLists[i]) == 0 {
				zeroMatch = true
			}
		}
		if zeroMatch {
			result.Skipped = append(result.Skipped, SkipReason{Reason: fmt.Sprintf("%s: at least one component suffix matched zero products", code)})
			continue
		}

		name := row.name
		if name == "" {
			name = code
		}
		chosen, confident := findBestGuessCombination(candidateLists, row.mrp, name)

		sumPrices := 0.0
		skus := make([]string, 0, len(chosen))
		for _, p := range chosen {
			sumPrices += p.Price
			skus = append(skus, p.SKU)
		}
		productIDs, qtyByProduct := dedupeWithQuantity(chosen)
		discount := math.Max(0, money(sumPrices-row.mrp))

		description := "UNVERIFIED — LOW CONFIDENCE guess (no name match, price-only), please review first"
		if confident {
			description = "UNVERIFIED — components auto-guessed by name match, please review"
		}

		if _, err := r.CreateCombo(ctx, models.Combo{
			Name: name, Code: code, Description: description, DiscountType: "fixed", DiscountValue: discount,
		}, productIDs, qtyByProduct); err != nil {
			result.Skipped = append(result.Skipped, SkipReason{Reason: fmt.Sprintf("%s: %v", code, err)})
			continue
		}

		result.Created = append(result.Created, CreatedCombo{
			Code: code, Name: name, ComboMRP: row.mrp, SumComponentPrices: money(sumPrices),
			DiscountValue: discount, Components: skus, Confident: confident,
		})
	}
	return result, nil
}

// ResolveCombo creates one combo from manually-picked component selections
// (the human counterpart to AutoGuessFromSheet, for the entries a reviewer
// wants to pick themselves rather than trust the heuristic).
func (r *Repository) ResolveCombo(ctx context.Context, code, name string, mrp float64, productIDs []int64) (int64, error) {
	if _, found, _ := r.FindComboByCode(ctx, code); found {
		return 0, fmt.Errorf("a combo with this code already exists")
	}
	sumPrices := 0.0
	for _, id := range productIDs {
		var price float64
		if err := r.pool.QueryRow(ctx, `SELECT price FROM products WHERE id = $1`, id).Scan(&price); err != nil {
			return 0, fmt.Errorf("product %d not found", id)
		}
		sumPrices += price
	}
	discount := math.Max(0, money(sumPrices-mrp))
	if name == "" {
		name = code
	}
	return r.CreateCombo(ctx, models.Combo{Name: name, Code: code, DiscountType: "fixed", DiscountValue: discount}, productIDs, nil)
}
