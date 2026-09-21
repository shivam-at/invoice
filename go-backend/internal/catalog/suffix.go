package catalog

import "regexp"

var trailingSuffixPattern = regexp.MustCompile(`_(\d+)$`)

// trailingNumericSuffix extracts the trailing _NNNN from a SKU like
// "SEL_0004" -> "0004", the convention combo codes reference their
// components by (e.g. CMB_0004_0428 bundles SKUs ending _0004 and _0428).
func trailingNumericSuffix(sku string) string {
	m := trailingSuffixPattern.FindStringSubmatch(sku)
	if m == nil {
		return ""
	}
	return m[1]
}
