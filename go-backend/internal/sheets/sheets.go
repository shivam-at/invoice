// Package sheets ports the Node app's googleSheetsService.js: read-only
// access to a Google Sheet via a service-account credential, used to pull
// the "Item Master" product/combo catalog into this system.
package sheets

import (
	"context"
	"fmt"
	"regexp"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

var spreadsheetIDPattern = regexp.MustCompile(`/d/([a-zA-Z0-9-_]+)`)

// ExtractSpreadsheetID accepts either a bare spreadsheet id or a full
// docs.google.com/spreadsheets/d/<id>/... URL and returns just the id.
func ExtractSpreadsheetID(idOrURL string) string {
	if m := spreadsheetIDPattern.FindStringSubmatch(idOrURL); m != nil {
		return m[1]
	}
	return idOrURL
}

// FetchRows reads every row of rangeName (default "Sheet1") as strings,
// authenticating with the service-account key at keyFile. Row 0 is expected
// to be the header by every caller in this package.
func FetchRows(ctx context.Context, keyFile, spreadsheetIDOrURL, rangeName string) ([][]string, error) {
	if rangeName == "" {
		rangeName = "Sheet1"
	}

	svc, err := sheets.NewService(ctx,
		option.WithCredentialsFile(keyFile),
		option.WithScopes(sheets.SpreadsheetsReadonlyScope),
	)
	if err != nil {
		return nil, fmt.Errorf("sheets client: %w", err)
	}

	spreadsheetID := ExtractSpreadsheetID(spreadsheetIDOrURL)
	resp, err := svc.Spreadsheets.Values.Get(spreadsheetID, rangeName).Do()
	if err != nil {
		return nil, fmt.Errorf("fetch sheet values: %w", err)
	}

	rows := make([][]string, len(resp.Values))
	for i, row := range resp.Values {
		strRow := make([]string, len(row))
		for j, cell := range row {
			strRow[j] = fmt.Sprintf("%v", cell)
		}
		rows[i] = strRow
	}
	return rows, nil
}

// Cell safely reads column idx from a row, treating a too-short row as
// blank rather than panicking — sheet rows often omit trailing empty cells.
func Cell(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return row[idx]
}
