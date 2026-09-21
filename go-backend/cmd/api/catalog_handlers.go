package main

import (
	"encoding/json"
	"net/http"
	"strconv"

	"invoice-system/internal/models"
	"invoice-system/internal/sheets"
)

func parsePageParams(r *http.Request, defaultPageSize int) (page, pageSize int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ = strconv.Atoi(r.URL.Query().Get("pageSize"))
	if pageSize < 1 || pageSize > 2000 {
		pageSize = defaultPageSize
	}
	return page, pageSize
}

// --- Products ---

func (a *api) listProducts(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePageParams(r, 50)
	search := r.URL.Query().Get("search")
	result, err := a.catalogRepo.ListProducts(r.Context(), search, page, pageSize)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result.Items, "total": result.Total, "page": page, "pageSize": pageSize})
}

type createProductReq struct {
	Name    string  `json:"name"`
	SKU     string  `json:"sku"`
	HSNCode string  `json:"hsn_code"`
	Price   float64 `json:"price"`
	TaxRate float64 `json:"tax_rate"`
}

func (a *api) createProduct(w http.ResponseWriter, r *http.Request) {
	var req createProductReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Name == "" {
		httpError(w, http.StatusBadRequest, "name and price are required")
		return
	}
	id, err := a.catalogRepo.CreateProduct(r.Context(), models.Product{
		Name: req.Name, SKU: req.SKU, HSNCode: req.HSNCode, Price: req.Price, TaxRate: req.TaxRate,
	})
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *api) deleteProduct(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := a.catalogRepo.DeleteProduct(r.Context(), id); err != nil {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type sheetImportReq struct {
	SpreadsheetID string `json:"spreadsheet_id"`
	SheetURL      string `json:"sheet_url"`
	Range         string `json:"range"`
}

func (req sheetImportReq) source() string {
	if req.SpreadsheetID != "" {
		return req.SpreadsheetID
	}
	return req.SheetURL
}

func (a *api) importProductsFromSheet(w http.ResponseWriter, r *http.Request) {
	var req sheetImportReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.source() == "" {
		httpError(w, http.StatusBadRequest, "spreadsheet_id or sheet_url is required")
		return
	}
	rows, err := sheets.FetchRows(r.Context(), a.googleKeyFile, req.source(), req.Range)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	result, err := a.catalogRepo.ImportProductsFromRows(r.Context(), rows)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// --- Combos ---

func (a *api) listCombos(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePageParams(r, 25)
	search := r.URL.Query().Get("search")
	unverified := r.URL.Query().Get("unverified") == "true"
	result, err := a.catalogRepo.ListCombos(r.Context(), search, unverified, page, pageSize)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result.Items, "total": result.Total, "page": page, "pageSize": pageSize})
}

type createComboReq struct {
	Name          string  `json:"name"`
	Code          string  `json:"code"`
	Description   string  `json:"description"`
	DiscountType  string  `json:"discount_type"`
	DiscountValue float64 `json:"discount_value"`
	Items         []struct {
		ProductID int64   `json:"product_id"`
		Quantity  float64 `json:"quantity"`
	} `json:"items"`
}

func (a *api) createCombo(w http.ResponseWriter, r *http.Request) {
	var req createComboReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Name == "" || len(req.Items) < 2 {
		httpError(w, http.StatusBadRequest, "name and at least 2 combo items are required")
		return
	}
	discountType := req.DiscountType
	if discountType != "percent" && discountType != "fixed" {
		discountType = "none"
	}

	var productIDs []int64
	qtyByProduct := map[int64]float64{}
	for _, it := range req.Items {
		if it.ProductID == 0 {
			httpError(w, http.StatusBadRequest, "each item requires product_id")
			return
		}
		productIDs = append(productIDs, it.ProductID)
		qtyByProduct[it.ProductID] = it.Quantity
	}

	id, err := a.catalogRepo.CreateCombo(r.Context(), models.Combo{
		Name: req.Name, Code: req.Code, Description: req.Description,
		DiscountType: discountType, DiscountValue: req.DiscountValue,
	}, productIDs, qtyByProduct)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (a *api) deleteCombo(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := a.catalogRepo.DeleteCombo(r.Context(), id); err != nil {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) importCombosFromSheet(w http.ResponseWriter, r *http.Request) {
	var req sheetImportReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.source() == "" {
		httpError(w, http.StatusBadRequest, "spreadsheet_id or sheet_url is required")
		return
	}
	rows, err := sheets.FetchRows(r.Context(), a.googleKeyFile, req.source(), req.Range)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	result, err := a.catalogRepo.ImportCombosFromSheet(r.Context(), rows)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"createdCount": len(result.Created), "created": result.Created,
		"skippedCount": len(result.Skipped), "skipped": result.Skipped,
	})
}

func (a *api) unresolvedCombosFromSheet(w http.ResponseWriter, r *http.Request) {
	var req sheetImportReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.source() == "" {
		httpError(w, http.StatusBadRequest, "spreadsheet_id or sheet_url is required")
		return
	}
	rows, err := sheets.FetchRows(r.Context(), a.googleKeyFile, req.source(), req.Range)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	unresolved, err := a.catalogRepo.UnresolvedFromSheet(r.Context(), rows)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unresolvedCount": len(unresolved), "unresolved": unresolved})
}

func (a *api) autoGuessCombosFromSheet(w http.ResponseWriter, r *http.Request) {
	var req sheetImportReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.source() == "" {
		httpError(w, http.StatusBadRequest, "spreadsheet_id or sheet_url is required")
		return
	}
	rows, err := sheets.FetchRows(r.Context(), a.googleKeyFile, req.source(), req.Range)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	result, err := a.catalogRepo.AutoGuessFromSheet(r.Context(), rows)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"createdCount": len(result.Created), "created": result.Created,
		"skippedCount": len(result.Skipped), "skipped": result.Skipped,
	})
}

type resolveComboReq struct {
	Code       string  `json:"code"`
	Name       string  `json:"name"`
	MRP        float64 `json:"mrp"`
	Selections []struct {
		ProductID int64 `json:"product_id"`
	} `json:"selections"`
}

func (a *api) resolveCombo(w http.ResponseWriter, r *http.Request) {
	var req resolveComboReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Code == "" || len(req.Selections) < 2 {
		httpError(w, http.StatusBadRequest, "code and at least 2 product selections are required")
		return
	}
	var productIDs []int64
	for _, s := range req.Selections {
		if s.ProductID == 0 {
			httpError(w, http.StatusBadRequest, "every component needs a selected product")
			return
		}
		productIDs = append(productIDs, s.ProductID)
	}
	id, err := a.catalogRepo.ResolveCombo(r.Context(), req.Code, req.Name, req.MRP, productIDs)
	if err != nil {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}
