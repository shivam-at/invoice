-- Core schema for the CMD (combo) order invoice printing pipeline.
-- "CMD order" = an order whose combo's code starts with 'CMB' (the existing
-- combo-SKU convention). Status columns use TEXT + CHECK instead of native
-- Postgres enums so new states can be added later with a plain migration.

CREATE TABLE IF NOT EXISTS products (
    id            BIGSERIAL PRIMARY KEY,
    sku           TEXT UNIQUE,
    name          TEXT NOT NULL,
    hsn_code      TEXT,
    price         NUMERIC(12, 2) NOT NULL,
    tax_rate      NUMERIC(5, 2) NOT NULL DEFAULT 0,
    category_code TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS combos (
    id             BIGSERIAL PRIMARY KEY,
    name           TEXT NOT NULL,
    code           TEXT UNIQUE,
    description    TEXT,
    discount_type  TEXT NOT NULL DEFAULT 'none' CHECK (discount_type IN ('none', 'percent', 'fixed')),
    discount_value NUMERIC(12, 2) NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Combo codes follow the CMB_xxxx_yyyy convention; this is what "identify
-- CMD orders" filters on.
CREATE INDEX IF NOT EXISTS idx_combos_code_prefix ON combos (code) WHERE code LIKE 'CMB%';

CREATE TABLE IF NOT EXISTS combo_items (
    id         BIGSERIAL PRIMARY KEY,
    combo_id   BIGINT NOT NULL REFERENCES combos (id) ON DELETE CASCADE,
    product_id BIGINT NOT NULL REFERENCES products (id) ON DELETE RESTRICT,
    quantity   NUMERIC(10, 2) NOT NULL DEFAULT 1,
    UNIQUE (combo_id, product_id)
);

CREATE INDEX IF NOT EXISTS idx_combo_items_combo_id ON combo_items (combo_id);

CREATE TABLE IF NOT EXISTS orders (
    id                  BIGSERIAL PRIMARY KEY,
    order_no            TEXT UNIQUE NOT NULL,
    combo_id            BIGINT REFERENCES combos (id) ON DELETE SET NULL,
    combo_quantity      NUMERIC(10, 2) NOT NULL DEFAULT 1,
    customer_name       TEXT NOT NULL,
    customer_address    TEXT,
    customer_state_code TEXT,
    -- Denormalized on write from combos.code so the identify-CMD-orders scan
    -- (the hottest query in the system) never has to join.
    is_cmd              BOOLEAN NOT NULL DEFAULT false,
    status              TEXT NOT NULL DEFAULT 'PENDING'
                         CHECK (status IN ('PENDING', 'QUEUED', 'GENERATING', 'GENERATED',
                                            'PRINTING', 'PRINTED', 'FAILED')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_orders_status ON orders (status);
CREATE INDEX IF NOT EXISTS idx_orders_is_cmd_status ON orders (is_cmd, status) WHERE is_cmd = true;

CREATE TABLE IF NOT EXISTS invoices (
    -- One invoice per order, enforced here rather than only at the
    -- application layer, so a duplicate job can never create a second row
    -- even under concurrent workers.
    order_id       BIGINT PRIMARY KEY REFERENCES orders (id) ON DELETE CASCADE,
    invoice_number TEXT UNIQUE NOT NULL,
    pdf_path       TEXT,
    total_amount   NUMERIC(12, 2) NOT NULL DEFAULT 0,
    generated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS print_jobs (
    id           BIGSERIAL PRIMARY KEY,
    -- Same reasoning as invoices.order_id: one live print job per invoice.
    invoice_id   BIGINT UNIQUE NOT NULL REFERENCES invoices (order_id) ON DELETE CASCADE,
    status       TEXT NOT NULL DEFAULT 'QUEUED'
                 CHECK (status IN ('QUEUED', 'PRINTING', 'PRINTED', 'FAILED')),
    attempts     INT NOT NULL DEFAULT 0,
    last_error   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    printed_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_print_jobs_status ON print_jobs (status);
