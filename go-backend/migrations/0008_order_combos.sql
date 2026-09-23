-- Supports an order referencing 2+ different combo codes at once (a real
-- pattern the sheet import was previously skipping outright). orders.combo_id
-- stays exactly as-is - the order's first/primary combo, unchanged for every
-- existing single-combo order. This table holds any ADDITIONAL combos beyond
-- that one, so a genuinely multi-combo order gets every one of its combos
-- represented (each with its own bold group on the invoice), not just the
-- first.
CREATE TABLE IF NOT EXISTS order_combos (
    id             BIGSERIAL PRIMARY KEY,
    order_id       BIGINT NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    combo_id       BIGINT NOT NULL REFERENCES combos (id) ON DELETE RESTRICT,
    combo_quantity NUMERIC(10, 2) NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_order_combos_order_id ON order_combos (order_id);
