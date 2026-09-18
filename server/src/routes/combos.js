const express = require("express");
const db = require("../db");
const { fetchSheetRows } = require("../services/googleSheetsService");

const router = express.Router();

function money(n) {
  return Number(n.toFixed(2));
}

function getComboWithItems(comboId) {
  const combo = db.prepare("SELECT * FROM combos WHERE id = ?").get(comboId);
  if (!combo) return null;
  const items = db
    .prepare(
      `SELECT ci.id, ci.product_id, ci.quantity, p.name, p.sku, p.price, p.tax_rate
       FROM combo_items ci JOIN products p ON p.id = ci.product_id
       WHERE ci.combo_id = ?`
    )
    .all(comboId);
  return { ...combo, items };
}

/** Loads the sheet once and groups its CMB (combo) rows by Product Code, de-duped. */
async function loadComboRowsFromSheet(source, range) {
  const rows = await fetchSheetRows(source, range);
  if (rows.length < 2) throw new Error("Sheet has no data rows below the header");

  const header = rows[0].map((h) => String(h || "").trim().toLowerCase());
  const idx = {
    category: header.indexOf("category code"),
    code: header.indexOf("product code"),
    name: header.indexOf("name"),
    mrp: header.indexOf("mrp")
  };
  if (idx.category === -1 || idx.code === -1 || idx.name === -1 || idx.mrp === -1) {
    const err = new Error("Sheet must have Category Code, Product Code, Name, and MRP columns");
    err.detectedHeader = rows[0];
    throw err;
  }

  const comboRowsByCode = new Map();
  for (const row of rows.slice(1)) {
    const category = (row[idx.category] || "").trim().toUpperCase();
    const code = row[idx.code];
    if (category === "CMB" && code && !comboRowsByCode.has(code)) {
      comboRowsByCode.set(code, row);
    }
  }
  return { comboRowsByCode, idx };
}

/** Suffix (trailing _NNNN in a SKU) -> every product in the catalog ending with it. */
function buildProductSuffixIndex() {
  const allProducts = db.prepare("SELECT id, name, sku, price, category_code FROM products WHERE sku IS NOT NULL").all();
  const productsBySuffix = new Map();
  for (const product of allProducts) {
    const match = product.sku.match(/_(\d+)$/);
    if (!match) continue;
    const suffix = match[1];
    if (!productsBySuffix.has(suffix)) productsBySuffix.set(suffix, []);
    productsBySuffix.get(suffix).push(product);
  }
  return productsBySuffix;
}

router.get("/", (req, res) => {
  const search = (req.query.search || "").trim();
  const unverifiedOnly = req.query.unverified === "true";
  const page = Math.max(1, parseInt(req.query.page, 10) || 1);
  const pageSize = Math.min(2000, Math.max(1, parseInt(req.query.pageSize, 10) || 25));
  const offset = (page - 1) * pageSize;

  const conditions = [];
  const params = [];
  if (search) {
    conditions.push("(name LIKE ? OR code LIKE ?)");
    params.push(`%${search}%`, `%${search}%`);
  }
  if (unverifiedOnly) {
    conditions.push("description LIKE 'UNVERIFIED%'");
  }
  const where = conditions.length ? `WHERE ${conditions.join(" AND ")}` : "";

  const total = db.prepare(`SELECT COUNT(*) AS count FROM combos ${where}`).get(...params).count;
  const rows = db
    .prepare(`SELECT * FROM combos ${where} ORDER BY created_at DESC LIMIT ? OFFSET ?`)
    .all(...params, pageSize, offset);

  // Fetch every item for this page's combos in one query instead of one query per combo.
  const itemsByCombo = new Map();
  if (rows.length > 0) {
    const placeholders = rows.map(() => "?").join(",");
    const allItems = db
      .prepare(
        `SELECT ci.id, ci.combo_id, ci.product_id, ci.quantity, p.name, p.sku, p.price, p.tax_rate
         FROM combo_items ci JOIN products p ON p.id = ci.product_id
         WHERE ci.combo_id IN (${placeholders})`
      )
      .all(...rows.map((r) => r.id));
    for (const item of allItems) {
      if (!itemsByCombo.has(item.combo_id)) itemsByCombo.set(item.combo_id, []);
      itemsByCombo.get(item.combo_id).push(item);
    }
  }

  const items = rows.map((r) => ({ ...r, items: itemsByCombo.get(r.id) || [] }));
  res.json({ items, total, page, pageSize });
});

router.get("/:id", (req, res) => {
  const combo = getComboWithItems(req.params.id);
  if (!combo) return res.status(404).json({ error: "Combo not found" });
  res.json(combo);
});

router.post("/", (req, res) => {
  const { name, code, description, discount_type, discount_value, items } = req.body;
  if (!name || !Array.isArray(items) || items.length < 2) {
    return res.status(400).json({ error: "name and at least 2 combo items are required" });
  }
  const type = ["none", "percent", "fixed"].includes(discount_type) ? discount_type : "none";

  const tx = db.transaction(() => {
    const info = db
      .prepare("INSERT INTO combos (name, code, description, discount_type, discount_value) VALUES (?, ?, ?, ?, ?)")
      .run(name, code || null, description || null, type, Number(discount_value) || 0);
    const comboId = info.lastInsertRowid;
    const insertItem = db.prepare(
      "INSERT INTO combo_items (combo_id, product_id, quantity) VALUES (?, ?, ?)"
    );
    for (const item of items) {
      if (!item.product_id) throw new Error("Each item requires product_id");
      insertItem.run(comboId, item.product_id, Number(item.quantity) || 1);
    }
    return comboId;
  });

  try {
    const comboId = tx();
    res.status(201).json(getComboWithItems(comboId));
  } catch (err) {
    res.status(400).json({ error: err.message });
  }
});

/**
 * Auto-detects combo deals from an Item Master sheet. Rows with Category Code
 * "CMB" are combos; their Product Code encodes component SKUs as trailing
 * numeric suffixes joined by underscore (e.g. CMB_0047_0196 bundles whichever
 * two products already in our catalog have SKUs ending in _0047 and _0196).
 * A combo's discount is reconstructed as (sum of component prices - combo MRP).
 * Combos where a suffix doesn't resolve to exactly one product are skipped
 * for manual review (see /unresolved-from-sheet) rather than guessed at,
 * since this feeds real invoices.
 */
router.post("/import-google-sheet", async (req, res) => {
  const { spreadsheet_id, sheet_url, range } = req.body;
  const source = spreadsheet_id || sheet_url;
  if (!source) {
    return res.status(400).json({ error: "spreadsheet_id or sheet_url is required" });
  }

  try {
    const { comboRowsByCode, idx } = await loadComboRowsFromSheet(source, range);
    const productsBySuffix = buildProductSuffixIndex();

    const findComboByCode = db.prepare("SELECT id FROM combos WHERE code = ?");
    const insertCombo = db.prepare(
      "INSERT INTO combos (name, code, discount_type, discount_value) VALUES (?, ?, 'fixed', ?)"
    );
    const insertComboItem = db.prepare("INSERT INTO combo_items (combo_id, product_id, quantity) VALUES (?, ?, 1)");

    const created = [];
    const skipped = [];

    const tx = db.transaction(() => {
      for (const [code, row] of comboRowsByCode) {
        if (findComboByCode.get(code)) {
          skipped.push({ code, reason: "combo already exists" });
          continue;
        }

        const suffixes = code
          .replace(/^CMB_/i, "")
          .split("_")
          .filter((s) => /^\d+$/.test(s));
        if (suffixes.length < 2) {
          skipped.push({ code, reason: "could not parse component codes from product code" });
          continue;
        }

        const resolvedProducts = [];
        let unresolvedReason = null;
        for (const suffix of suffixes) {
          const matches = productsBySuffix.get(suffix) || [];
          if (matches.length !== 1) {
            unresolvedReason = `component suffix _${suffix} matched ${matches.length} products (need exactly 1)`;
            break;
          }
          resolvedProducts.push(matches[0]);
        }
        if (unresolvedReason) {
          skipped.push({ code, reason: unresolvedReason });
          continue;
        }

        const comboMrp = Number(String(row[idx.mrp] || "0").replace(/[^0-9.-]/g, "")) || 0;
        const sumComponentPrices = resolvedProducts.reduce((acc, p) => acc + p.price, 0);
        const discountValue = Math.max(0, money(sumComponentPrices - comboMrp));

        const info = insertCombo.run(row[idx.name] || code, code, discountValue);
        const comboId = info.lastInsertRowid;
        for (const product of resolvedProducts) insertComboItem.run(comboId, product.id);

        created.push({
          code,
          name: row[idx.name],
          comboMrp,
          sumComponentPrices: money(sumComponentPrices),
          discountValue,
          components: resolvedProducts.map((p) => p.sku)
        });
      }
    });
    tx();

    res.json({ createdCount: created.length, created, skippedCount: skipped.length, skipped });
  } catch (err) {
    res.status(err.detectedHeader ? 400 : 500).json({ error: err.message, detectedHeader: err.detectedHeader });
  }
});

/**
 * Returns combo codes that import-google-sheet could NOT auto-create because
 * a suffix matched zero or multiple products, along with the actual candidate
 * products for each suffix — so the caller can render a "pick the right one"
 * UI instead of guessing. Read-only: creates nothing.
 */
router.post("/unresolved-from-sheet", async (req, res) => {
  const { spreadsheet_id, sheet_url, range } = req.body;
  const source = spreadsheet_id || sheet_url;
  if (!source) {
    return res.status(400).json({ error: "spreadsheet_id or sheet_url is required" });
  }

  try {
    const { comboRowsByCode, idx } = await loadComboRowsFromSheet(source, range);
    const productsBySuffix = buildProductSuffixIndex();
    const findComboByCode = db.prepare("SELECT id FROM combos WHERE code = ?");

    const unresolved = [];
    for (const [code, row] of comboRowsByCode) {
      if (findComboByCode.get(code)) continue; // already created

      const suffixes = code
        .replace(/^CMB_/i, "")
        .split("_")
        .filter((s) => /^\d+$/.test(s));
      if (suffixes.length < 2) continue; // not the expected code shape, nothing to offer

      const allUnique = suffixes.every((suffix) => (productsBySuffix.get(suffix) || []).length === 1);
      if (allUnique) continue; // import-google-sheet already handles (or would handle) this one

      const comboMrp = Number(String(row[idx.mrp] || "0").replace(/[^0-9.-]/g, "")) || 0;
      unresolved.push({
        code,
        name: row[idx.name] || code,
        mrp: comboMrp,
        suffixes: suffixes.map((suffix) => ({
          suffix,
          candidates: (productsBySuffix.get(suffix) || []).map((p) => ({
            id: p.id,
            name: p.name,
            sku: p.sku,
            price: p.price,
            category_code: p.category_code
          }))
        }))
      });
    }

    res.json({ unresolvedCount: unresolved.length, unresolved });
  } catch (err) {
    res.status(err.detectedHeader ? 400 : 500).json({ error: err.message, detectedHeader: err.detectedHeader });
  }
});

const NAME_STOPWORDS = new Set([
  "combo", "deal", "with", "from", "this", "that", "free", "gift", "pack", "set",
  "origin", "nepal", "the", "and", "for", "pieces", "piece",
  // Generic marketing/descriptor words that coincidentally overlap with unrelated
  // product names (e.g. "Self Care Love Combo" matching "SELF Adhesive Tape").
  "self", "care", "pure", "fine", "best", "special", "premium", "natural",
  "design", "original", "authentic", "certified", "royal", "classic",
  "mini", "small", "large", "new", "edition", "style", "energised",
  "energized", "blessed", "sacred", "divine", "power", "energy"
]);

// Below this price, an item is almost certainly a packaging/filler SKU
// (tape, box, key chain filler, etc.) rather than a genuine bundled product
// in this catalog — used as a soft filter, never excluding every candidate.
const MIN_PLAUSIBLE_COMPONENT_PRICE = 150;

function significantWords(text) {
  return String(text || "")
    .toLowerCase()
    .replace(/[^a-z0-9\s]/g, " ")
    .split(/\s+/)
    .filter((w) => w.length > 3 && !NAME_STOPWORDS.has(w));
}

/**
 * Scores a candidate combination of products for a combo: rewards products
 * whose names share words with the combo's own name (e.g. a "Rose Quartz"
 * combo naming a "Rose Quartz Bracelet"), and penalizes a resulting discount
 * that isn't in a plausible retail range (roughly 0-70% off, near ~12% is
 * scored best). Used only for the auto-guess path — flagged as unverified
 * since this is a heuristic, not a real data match.
 */
function findBestGuessCombination(suffixCandidatesRaw, mrp, comboName) {
  const comboWords = significantWords(comboName);
  const suffixCandidates = suffixCandidatesRaw.map((list) => {
    const abovePriceFloor = list.filter((p) => p.price >= MIN_PLAUSIBLE_COMPONENT_PRICE);
    return abovePriceFloor.length > 0 ? abovePriceFloor : list;
  });
  let best = null;
  let bestScore = -Infinity;

  function priceScore(sum) {
    if (sum <= 0) return -1000;
    const discountPct = ((sum - mrp) / sum) * 100;
    if (discountPct < -20 || discountPct > 70) return -500 - Math.abs(discountPct);
    return -Math.abs(discountPct - 12);
  }

  let bestNameScore = 0;

  function recurse(i, chosen, sumPrice, nameScore) {
    if (i === suffixCandidates.length) {
      const score = nameScore * 1000 + priceScore(sumPrice);
      if (score > bestScore) {
        bestScore = score;
        best = [...chosen];
        bestNameScore = nameScore;
      }
      return;
    }
    for (const candidate of suffixCandidates[i]) {
      const matches = comboWords.some((w) => candidate.name.toLowerCase().includes(w)) ? 1 : 0;
      chosen.push(candidate);
      recurse(i + 1, chosen, sumPrice + candidate.price, nameScore + matches);
      chosen.pop();
    }
  }

  recurse(0, [], 0, 0);
  // A combo where at least one chosen product's name actually matched a word
  // in the combo's own name is a real signal; one where every suffix had zero
  // name overlap was picked on price plausibility alone — much less trustworthy.
  return { chosen: best, confident: bestNameScore > 0 };
}

/**
 * Auto-guesses components for every still-ambiguous CMB combo (see
 * /unresolved-from-sheet) using name-similarity + plausible-discount scoring,
 * and creates them immediately — but marks every one's description as
 * unverified so they're clearly flagged for manual review before being
 * trusted on real invoices. Skips combos whose code can't even be parsed
 * into 2+ numeric suffixes (nothing to guess from).
 */
router.post("/auto-guess-from-sheet", async (req, res) => {
  const { spreadsheet_id, sheet_url, range } = req.body;
  const source = spreadsheet_id || sheet_url;
  if (!source) {
    return res.status(400).json({ error: "spreadsheet_id or sheet_url is required" });
  }

  try {
    const { comboRowsByCode, idx } = await loadComboRowsFromSheet(source, range);
    const productsBySuffix = buildProductSuffixIndex();
    const findComboByCode = db.prepare("SELECT id FROM combos WHERE code = ?");
    const insertCombo = db.prepare(
      "INSERT INTO combos (name, code, description, discount_type, discount_value) VALUES (?, ?, ?, 'fixed', ?)"
    );
    const insertComboItem = db.prepare("INSERT INTO combo_items (combo_id, product_id, quantity) VALUES (?, ?, 1)");

    const created = [];
    const skipped = [];

    const tx = db.transaction(() => {
      for (const [code, row] of comboRowsByCode) {
        if (findComboByCode.get(code)) {
          skipped.push({ code, reason: "combo already exists" });
          continue;
        }

        const suffixes = code
          .replace(/^CMB_/i, "")
          .split("_")
          .filter((s) => /^\d+$/.test(s));
        if (suffixes.length < 2) {
          skipped.push({ code, reason: "could not parse component codes from product code" });
          continue;
        }

        const suffixCandidates = suffixes.map((s) => productsBySuffix.get(s) || []);
        if (suffixCandidates.some((list) => list.length === 0)) {
          skipped.push({ code, reason: "at least one component suffix matched zero products" });
          continue;
        }

        const comboName = row[idx.name] || code;
        const comboMrp = Number(String(row[idx.mrp] || "0").replace(/[^0-9.-]/g, "")) || 0;
        const { chosen, confident } = findBestGuessCombination(suffixCandidates, comboMrp, comboName);

        const sumComponentPrices = chosen.reduce((acc, p) => acc + p.price, 0);
        const discountValue = Math.max(0, money(sumComponentPrices - comboMrp));

        const description = confident
          ? "UNVERIFIED — components auto-guessed by name match, please review"
          : "UNVERIFIED — LOW CONFIDENCE guess (no name match, price-only), please review first";

        const info = insertCombo.run(comboName, code, description, discountValue);
        const comboId = info.lastInsertRowid;
        for (const product of chosen) insertComboItem.run(comboId, product.id);

        created.push({
          code,
          name: comboName,
          comboMrp,
          sumComponentPrices: money(sumComponentPrices),
          components: chosen.map((p) => p.sku),
          confident
        });
      }
    });
    tx();

    res.json({ createdCount: created.length, created, skippedCount: skipped.length, skipped });
  } catch (err) {
    res.status(err.detectedHeader ? 400 : 500).json({ error: err.message, detectedHeader: err.detectedHeader });
  }
});

/** Creates one combo from manually picked components (see /unresolved-from-sheet). */
router.post("/resolve", (req, res) => {
  const { code, name, mrp, selections } = req.body;
  if (!code || !Array.isArray(selections) || selections.length < 2) {
    return res.status(400).json({ error: "code and at least 2 product selections are required" });
  }
  if (selections.some((s) => !s.product_id)) {
    return res.status(400).json({ error: "Every component needs a selected product" });
  }
  if (db.prepare("SELECT id FROM combos WHERE code = ?").get(code)) {
    return res.status(409).json({ error: "A combo with this code already exists" });
  }

  const productIds = selections.map((s) => s.product_id);
  const placeholders = productIds.map(() => "?").join(",");
  const products = db.prepare(`SELECT id, price FROM products WHERE id IN (${placeholders})`).all(...productIds);
  if (products.length !== new Set(productIds).size) {
    return res.status(400).json({ error: "One or more selected products were not found" });
  }
  const priceById = new Map(products.map((p) => [p.id, p.price]));
  const sumComponentPrices = productIds.reduce((acc, id) => acc + priceById.get(id), 0);
  const discountValue = Math.max(0, money(sumComponentPrices - (Number(mrp) || 0)));

  const tx = db.transaction(() => {
    const info = db
      .prepare("INSERT INTO combos (name, code, discount_type, discount_value) VALUES (?, ?, 'fixed', ?)")
      .run(name || code, code, discountValue);
    const comboId = info.lastInsertRowid;
    const insertItem = db.prepare("INSERT INTO combo_items (combo_id, product_id, quantity) VALUES (?, ?, 1)");
    for (const id of productIds) insertItem.run(comboId, id);
    return comboId;
  });

  try {
    const comboId = tx();
    res.status(201).json(getComboWithItems(comboId));
  } catch (err) {
    res.status(400).json({ error: err.message });
  }
});

router.delete("/:id", (req, res) => {
  const usedInOrder = db.prepare("SELECT 1 FROM orders WHERE combo_id = ? LIMIT 1").get(req.params.id);
  if (usedInOrder) {
    return res.status(409).json({ error: "Cannot delete a combo that already has orders" });
  }
  db.prepare("DELETE FROM combos WHERE id = ?").run(req.params.id);
  res.status(204).end();
});

module.exports = router;
