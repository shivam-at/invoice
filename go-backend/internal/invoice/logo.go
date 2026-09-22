package invoice

import (
	"image/png"
	"os"

	"github.com/go-pdf/fpdf"
)

const (
	fulfillmentLogoName = "unicommerce-logo"
	fulfillmentLogoPath = "assets/unicommerce-logo.png"
)

// registerLogo loads and registers the fulfillment platform's real logo
// image for this render, returning its pixel width/height (so the caller
// can scale it to a target height without distorting it) and whether it
// was found. Falls back to false if the asset is missing so the caller can
// fall back to plain text instead of erroring the whole invoice.
func registerLogo(pdf *fpdf.Fpdf) (width, height float64, ok bool) {
	f, err := os.Open(fulfillmentLogoPath)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()

	cfg, err := png.DecodeConfig(f)
	if err != nil {
		return 0, 0, false
	}
	if _, err := f.Seek(0, 0); err != nil {
		return 0, 0, false
	}
	pdf.RegisterImageOptionsReader(fulfillmentLogoName, fpdf.ImageOptions{ImageType: "PNG"}, f)
	return float64(cfg.Width), float64(cfg.Height), true
}
