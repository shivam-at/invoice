// Package catalog owns products and combos: CRUD, and importing both from
// the Google Sheet "Item Master" (ported from the Node app's products.js /
// combos.js routes).
package catalog

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"invoice-system/internal/models"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// --- Products ---

type ProductPage struct {
	Items []models.Product
	Total int
}

func (r *Repository) ListProducts(ctx context.Context, search string, page, pageSize int) (ProductPage, error) {
	offset := (page - 1) * pageSize
	where := ""
	args := []any{}
	if search != "" {
		where = "WHERE name ILIKE $1 OR sku ILIKE $1"
		args = append(args, "%"+search+"%")
	}

	var total int
	countSQL := fmt.Sprintf("SELECT count(*) FROM products %s", where)
	if err := r.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return ProductPage{}, err
	}

	args = append(args, pageSize, offset)
	listSQL := fmt.Sprintf(`
		SELECT id, COALESCE(sku,''), name, COALESCE(hsn_code,''), price, tax_rate, COALESCE(category_code,'')
		FROM products %s ORDER BY id DESC LIMIT $%d OFFSET $%d
	`, where, len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return ProductPage{}, err
	}
	defer rows.Close()

	items := []models.Product{}
	for rows.Next() {
		var p models.Product
		if err := rows.Scan(&p.ID, &p.SKU, &p.Name, &p.HSNCode, &p.Price, &p.TaxRate, &p.CategoryCode); err != nil {
			return ProductPage{}, err
		}
		items = append(items, p)
	}
	return ProductPage{Items: items, Total: total}, nil
}

func (r *Repository) CreateProduct(ctx context.Context, p models.Product) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO products (sku, name, hsn_code, price, tax_rate, category_code)
		VALUES (NULLIF($1,''), $2, NULLIF($3,''), $4, $5, NULLIF($6,''))
		RETURNING id
	`, p.SKU, p.Name, p.HSNCode, p.Price, p.TaxRate, p.CategoryCode).Scan(&id)
	return id, err
}

func (r *Repository) FindProductBySKU(ctx context.Context, sku string) (models.Product, bool, error) {
	var p models.Product
	err := r.pool.QueryRow(ctx, `
		SELECT id, COALESCE(sku,''), name, COALESCE(hsn_code,''), price, tax_rate, COALESCE(category_code,'')
		FROM products WHERE sku = $1
	`, sku).Scan(&p.ID, &p.SKU, &p.Name, &p.HSNCode, &p.Price, &p.TaxRate, &p.CategoryCode)
	if err != nil {
		return p, false, nil // not found is not an error for the import's upsert check
	}
	return p, true, nil
}

func (r *Repository) UpdateProduct(ctx context.Context, id int64, p models.Product) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE products SET name = $1, hsn_code = NULLIF($2,''), price = $3, tax_rate = $4, category_code = NULLIF($5,'')
		WHERE id = $6
	`, p.Name, p.HSNCode, p.Price, p.TaxRate, p.CategoryCode, id)
	return err
}

func (r *Repository) DeleteProduct(ctx context.Context, id int64) error {
	var inUse bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM combo_items WHERE product_id = $1)`, id).Scan(&inUse); err != nil {
		return err
	}
	if inUse {
		return fmt.Errorf("cannot delete a product that is used in a combo")
	}
	_, err := r.pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, id)
	return err
}

// AllProductsBySuffix mirrors buildProductSuffixIndex() from the Node app:
// groups every SKU'd product by the trailing _NNNN numeric suffix in its
// SKU, since that's the convention combo codes encode their components with.
func (r *Repository) AllProductsBySuffix(ctx context.Context) (map[string][]models.Product, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, sku, name, price, COALESCE(category_code,'') FROM products WHERE sku IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	bySuffix := map[string][]models.Product{}
	for rows.Next() {
		var p models.Product
		if err := rows.Scan(&p.ID, &p.SKU, &p.Name, &p.Price, &p.CategoryCode); err != nil {
			return nil, err
		}
		suffix := trailingNumericSuffix(p.SKU)
		if suffix == "" {
			continue
		}
		bySuffix[suffix] = append(bySuffix[suffix], p)
	}
	return bySuffix, nil
}

// --- Combos ---

type ComboPage struct {
	Items []ComboWithItems
	Total int
}

type ComboWithItems struct {
	models.Combo
	Items []models.ComboItem `json:"items"`
}

func (r *Repository) ListCombos(ctx context.Context, search string, unverifiedOnly bool, page, pageSize int) (ComboPage, error) {
	offset := (page - 1) * pageSize
	conditions := []string{}
	args := []any{}
	if search != "" {
		args = append(args, "%"+search+"%")
		conditions = append(conditions, fmt.Sprintf("(name ILIKE $%d OR code ILIKE $%d)", len(args), len(args)))
	}
	if unverifiedOnly {
		conditions = append(conditions, "description LIKE 'UNVERIFIED%'")
	}
	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + joinAND(conditions)
	}

	var total int
	if err := r.pool.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM combos %s", where), args...).Scan(&total); err != nil {
		return ComboPage{}, err
	}

	args = append(args, pageSize, offset)
	listSQL := fmt.Sprintf(`
		SELECT id, name, COALESCE(code,''), COALESCE(description,''), discount_type, discount_value
		FROM combos %s ORDER BY id DESC LIMIT $%d OFFSET $%d
	`, where, len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return ComboPage{}, err
	}
	combos := []ComboWithItems{}
	var ids []int64
	for rows.Next() {
		var c models.Combo
		if err := rows.Scan(&c.ID, &c.Name, &c.Code, &c.Description, &c.DiscountType, &c.DiscountValue); err != nil {
			rows.Close()
			return ComboPage{}, err
		}
		combos = append(combos, ComboWithItems{Combo: c, Items: []models.ComboItem{}})
		ids = append(ids, c.ID)
	}
	rows.Close()

	if len(ids) > 0 {
		itemRows, err := r.pool.Query(ctx, `
			SELECT ci.combo_id, ci.product_id, ci.quantity, p.sku, p.name, p.price, p.tax_rate
			FROM combo_items ci JOIN products p ON p.id = ci.product_id
			WHERE ci.combo_id = ANY($1)
		`, ids)
		if err != nil {
			return ComboPage{}, err
		}
		defer itemRows.Close()
		byCombo := map[int64][]models.ComboItem{}
		for itemRows.Next() {
			var it models.ComboItem
			if err := itemRows.Scan(&it.ComboID, &it.ProductID, &it.Quantity, &it.Product.SKU, &it.Product.Name,
				&it.Product.Price, &it.Product.TaxRate); err != nil {
				return ComboPage{}, err
			}
			byCombo[it.ComboID] = append(byCombo[it.ComboID], it)
		}
		for i := range combos {
			if its, ok := byCombo[combos[i].ID]; ok {
				combos[i].Items = its
			}
		}
	}

	return ComboPage{Items: combos, Total: total}, nil
}

func (r *Repository) FindComboByCode(ctx context.Context, code string) (int64, bool, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `SELECT id FROM combos WHERE code = $1`, code).Scan(&id)
	if err != nil {
		return 0, false, nil
	}
	return id, true, nil
}

// CreateCombo inserts a combo with its items in one transaction — either all
// of it lands or none of it does, so a partially-created combo can never
// exist (matches the Node app's db.transaction() usage in POST /combos).
func (r *Repository) CreateCombo(ctx context.Context, c models.Combo, itemProductIDs []int64, itemQty map[int64]float64) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var comboID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO combos (name, code, description, discount_type, discount_value)
		VALUES ($1, NULLIF($2,''), NULLIF($3,''), $4, $5) RETURNING id
	`, c.Name, c.Code, c.Description, c.DiscountType, c.DiscountValue).Scan(&comboID)
	if err != nil {
		return 0, err
	}

	for _, pid := range itemProductIDs {
		qty := itemQty[pid]
		if qty <= 0 {
			qty = 1
		}
		if _, err := tx.Exec(ctx, `INSERT INTO combo_items (combo_id, product_id, quantity) VALUES ($1, $2, $3)`,
			comboID, pid, qty); err != nil {
			return 0, err
		}
	}
	return comboID, tx.Commit(ctx)
}

func (r *Repository) DeleteCombo(ctx context.Context, id int64) error {
	var inUse bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM orders WHERE combo_id = $1)`, id).Scan(&inUse); err != nil {
		return err
	}
	if inUse {
		return fmt.Errorf("cannot delete a combo that already has orders")
	}
	_, err := r.pool.Exec(ctx, `DELETE FROM combos WHERE id = $1`, id)
	return err
}

func joinAND(parts []string) string {
	out := parts[0]
	for _, p := range parts[1:] {
		out += " AND " + p
	}
	return out
}
