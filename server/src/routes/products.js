const express = require("express");
const db = require("../db");
const { fetchSheetRows } = require("../services/googleSheetsService");

const router = express.Router();

const COLUMN_ALIASES = {
  name: ["name", "product name", "product"],
  sku: ["sku", "product code", "code"],
  hsn_code: ["hsn", "hsn code", "hsn_code"],
  price: ["price", "rate", "mrp", "selling price"],
  tax_rate: ["tax_rate", "tax rate", "gst", "gst%", "gst rate", "gst tax type code"],
  category_code: ["category code", "category_code"]
};

const COMBO_CATEGORY_CODE = "CMB";

function buildColumnIndex(headerRow) {
  const normalized = headerRow.map((h) => String(h || "").trim().toLowerCase());
  const index = {};
  for (const [field, aliases] of Object.entries(COLUMN_ALIASES)) {
    const pos = normalized.findIndex((h) => aliases.includes(h));
    if (pos !== -1) index[field] = pos;
  }
  return index;
}

router.get("/", (req, res) => {
  const search = (req.query.search || "").trim();
  const page = Math.max(1, parseInt(req.query.page, 10) || 1);
  const pageSize = Math.min(2000, Math.max(1, parseInt(req.query.pageSize, 10) || 50));
  const offset = (page - 1) * pageSize;

  const where = search ? "WHERE name LIKE ? OR sku LIKE ?" : "";
  const params = search ? [`%${search}%`, `%${search}%`] : [];

  const total = db.prepare(`SELECT COUNT(*) AS count FROM products ${where}`).get(...params).count;
  const items = db
    .prepare(`SELECT * FROM products ${where} ORDER BY created_at DESC LIMIT ? OFFSET ?`)
    .all(...params, pageSize, offset);

  res.json({ items, total, page, pageSize });
});

router.get("/:id", (req, res) => {
  const product = db.prepare("SELECT * FROM products WHERE id = ?").get(req.params.id);
  if (!product) return res.status(404).json({ error: "Product not found" });
  res.json(product);
});

router.post("/", (req, res) => {
  const { name, sku, hsn_code, price, tax_rate } = req.body;
  if (!name || price === undefined) {
    return res.status(400).json({ error: "name and price are required" });
  }
  const info = db
    .prepare("INSERT INTO products (name, sku, hsn_code, price, tax_rate) VALUES (?, ?, ?, ?, ?)")
    .run(name, sku || null, hsn_code || null, Number(price), Number(tax_rate) || 0);
  const product = db.prepare("SELECT * FROM products WHERE id = ?").get(info.lastInsertRowid);
  res.status(201).json(product);
});

router.post("/import-google-sheet", async (req, res) => {
  const { spreadsheet_id, sheet_url, range } = req.body;
  const source = spreadsheet_id || sheet_url;
  if (!source) {
    return res.status(400).json({ error: "spreadsheet_id or sheet_url is required" });
  }

  try {
    const rows = await fetchSheetRows(source, range);
    if (rows.length < 2) {
      return res.status(400).json({ error: "Sheet has no data rows below the header" });
    }

    const columnIndex = buildColumnIndex(rows[0]);
    if (columnIndex.name === undefined || columnIndex.price === undefined) {
      return res.status(400).json({
        error: "Could not find 'name' and 'price' columns in the sheet header",
        detectedHeader: rows[0]
      });
    }

    const findBySku = db.prepare("SELECT id FROM products WHERE sku = ?");
    const insert = db.prepare(
      "INSERT INTO products (name, sku, hsn_code, price, tax_rate, category_code) VALUES (?, ?, ?, ?, ?, ?)"
    );
    const update = db.prepare(
      "UPDATE products SET name = ?, hsn_code = ?, price = ?, tax_rate = ?, category_code = ? WHERE id = ?"
    );

    let created = 0;
    let updated = 0;
    const skipped = [];

    const tx = db.transaction(() => {
      for (let r = 1; r < rows.length; r++) {
        const row = rows[r];
        const categoryCode = columnIndex.category_code !== undefined ? row[columnIndex.category_code] || null : null;
        if ((categoryCode || "").trim().toUpperCase() === COMBO_CATEGORY_CODE) {
          skipped.push({ row: r + 1, reason: "combo item — import combos separately from Combo Deals page" });
          continue;
        }

        const name = row[columnIndex.name];
        const priceRaw = row[columnIndex.price];
        if (!name || priceRaw === undefined || priceRaw === "") {
          skipped.push({ row: r + 1, reason: "missing name or price" });
          continue;
        }
        const price = Number(String(priceRaw).replace(/[^0-9.-]/g, ""));
        if (Number.isNaN(price)) {
          skipped.push({ row: r + 1, reason: "price is not a number" });
          continue;
        }
        const sku = columnIndex.sku !== undefined ? row[columnIndex.sku] || null : null;
        const hsnCode = columnIndex.hsn_code !== undefined ? row[columnIndex.hsn_code] || null : null;
        const taxRateRaw = columnIndex.tax_rate !== undefined ? row[columnIndex.tax_rate] : 0;
        const taxRate = Number(String(taxRateRaw || 0).replace(/[^0-9.-]/g, "")) || 0;

        const existing = sku ? findBySku.get(sku) : undefined;
        if (existing) {
          update.run(name, hsnCode, price, taxRate, categoryCode, existing.id);
          updated++;
        } else {
          insert.run(name, sku, hsnCode, price, taxRate, categoryCode);
          created++;
        }
      }
    });
    tx();

    res.json({ created, updated, skipped, totalRows: rows.length - 1 });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.put("/:id", (req, res) => {
  const existing = db.prepare("SELECT * FROM products WHERE id = ?").get(req.params.id);
  if (!existing) return res.status(404).json({ error: "Product not found" });
  const { name, sku, hsn_code, price, tax_rate } = req.body;
  db.prepare("UPDATE products SET name = ?, sku = ?, hsn_code = ?, price = ?, tax_rate = ? WHERE id = ?").run(
    name ?? existing.name,
    sku ?? existing.sku,
    hsn_code ?? existing.hsn_code,
    price !== undefined ? Number(price) : existing.price,
    tax_rate !== undefined ? Number(tax_rate) : existing.tax_rate,
    req.params.id
  );
  res.json(db.prepare("SELECT * FROM products WHERE id = ?").get(req.params.id));
});

router.delete("/:id", (req, res) => {
  const usedInCombo = db.prepare("SELECT 1 FROM combo_items WHERE product_id = ? LIMIT 1").get(req.params.id);
  if (usedInCombo) {
    return res.status(409).json({ error: "Cannot delete a product that is used in a combo" });
  }
  db.prepare("DELETE FROM products WHERE id = ?").run(req.params.id);
  res.status(204).end();
});

module.exports = router;
