-- Supports an order having standalone product lines beyond its combo (e.g.
-- a "FREE GIFT" item bundled onto a specific order) — the reference invoice
-- shows these as their own top-level Sr row, not grouped under the combo.
-- unit_price is stored per-line (not just looked up from products.price)
-- since a free-gift line is often priced far below the product's normal
-- catalog price.
CREATE TABLE IF NOT EXISTS order_extra_items (
    id         BIGSERIAL PRIMARY KEY,
    order_id   BIGINT NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    product_id BIGINT NOT NULL REFERENCES products (id) ON DELETE RESTRICT,
    quantity   NUMERIC(10, 2) NOT NULL DEFAULT 1,
    unit_price NUMERIC(12, 2) NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_order_extra_items_order_id ON order_extra_items (order_id);
