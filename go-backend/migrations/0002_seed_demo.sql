-- Minimal demo catalog so the load test has a real CMD combo to attach
-- synthetic orders to. Safe to run repeatedly (ON CONFLICT DO NOTHING).

INSERT INTO products (sku, name, hsn_code, price, tax_rate, category_code)
VALUES ('SEL_0004', 'Raw Selenite Plate', '83062190', 1100, 5, 'SEL'),
       ('BP_0428', 'Cancer Bracelet', '71179090', 1700, 3, 'BP')
ON CONFLICT (sku) DO NOTHING;

INSERT INTO combos (name, code, discount_type, discount_value)
VALUES ('Cancer Bracelet and Raw Selenite Plate', 'CMB_0004_0428', 'percent', 10.1)
ON CONFLICT (code) DO NOTHING;

INSERT INTO combo_items (combo_id, product_id, quantity)
SELECT c.id, p.id, 1
FROM combos c, products p
WHERE c.code = 'CMB_0004_0428' AND p.sku IN ('SEL_0004', 'BP_0428')
ON CONFLICT (combo_id, product_id) DO NOTHING;
