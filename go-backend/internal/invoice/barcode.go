package invoice

import (
	"bytes"
	"fmt"
	"image/png"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/code128"
	"github.com/go-pdf/fpdf"
)

// registerBarcode renders text as a Code128 barcode and registers it as an
// in-memory image fpdf can place with ImageOptions. Returns "" if text is
// empty or encoding fails — callers should just skip drawing the image then,
// matching the old app's "no barcode if no order/AWB number" behavior.
func registerBarcode(pdf *fpdf.Fpdf, name, text string) string {
	if text == "" {
		return ""
	}
	code, err := code128.Encode(text)
	if err != nil {
		return ""
	}
	scaled, err := barcode.Scale(code, 300, 60)
	if err != nil {
		return ""
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, scaled); err != nil {
		return ""
	}
	imageName := fmt.Sprintf("barcode:%s", name)
	pdf.RegisterImageOptionsReader(imageName, fpdf.ImageOptions{ImageType: "PNG"}, &buf)
	return imageName
}
