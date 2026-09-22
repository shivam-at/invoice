-- Real Unicommerce-issued orders already have their own invoice number
-- ("Invoice Code") assigned when they were originally invoiced. When that's
-- present we print it instead of minting our own CMD######## number, since
-- the number on a real GST invoice can't be silently swapped for a
-- different one. Orders created fresh through this system (no external
-- code) still get our own generated number.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS external_invoice_code TEXT;
