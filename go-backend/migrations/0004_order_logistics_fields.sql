-- Carries over the logistics/portal fields from the Node app's invoice
-- header (Order No / Dispatch Through / AWB / Portal / Payment Mode),
-- needed to reproduce that invoice layout. Defaults are applied by the API
-- at order-creation time, not here, since there's no settings table yet.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS shopify_order_no TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS portal TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS payment_mode_code TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS payment_mode_label TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS dispatch_through TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS awb_no TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS shipping_name TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS shipping_address TEXT;
