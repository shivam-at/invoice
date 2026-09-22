-- Partial-COD orders (e.g. GoKwik PPCOD) collect a small amount online up
-- front and the rest on delivery; the reference Unicommerce invoice shows
-- this as its own "Prepaid Amount" row just above the Total in both tables.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS prepaid_amount NUMERIC(12, 2) NOT NULL DEFAULT 0;
