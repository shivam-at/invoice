const path = require("path");
const fs = require("fs");
const { DatabaseSync } = require("node:sqlite");

const DATA_DIR = path.join(__dirname, "..", "..", "data");
if (!fs.existsSync(DATA_DIR)) fs.mkdirSync(DATA_DIR, { recursive: true });

const db = new DatabaseSync(path.join(DATA_DIR, "invoice.db"));
db.exec("PRAGMA journal_mode = WAL");
db.exec("PRAGMA foreign_keys = ON");

// better-sqlite3-style transaction helper, since node:sqlite's DatabaseSync
// has no built-in .transaction() wrapper. Supports nesting via SAVEPOINTs,
// since invoice generation runs inside an order transaction that also
// calls settingsService's own transaction for invoice numbering.
let txDepth = 0;
db.transaction = function (fn) {
  return function (...args) {
    const isOuter = txDepth === 0;
    const savepoint = `sp_${txDepth}`;
    db.exec(isOuter ? "BEGIN" : `SAVEPOINT ${savepoint}`);
    txDepth++;
    try {
      const result = fn(...args);
      txDepth--;
      db.exec(isOuter ? "COMMIT" : `RELEASE SAVEPOINT ${savepoint}`);
      return result;
    } catch (err) {
      txDepth--;
      try {
        if (isOuter) {
          db.exec("ROLLBACK");
        } else {
          db.exec(`ROLLBACK TO SAVEPOINT ${savepoint}`);
          db.exec(`RELEASE SAVEPOINT ${savepoint}`);
        }
      } catch (_) {
        // ignore rollback failure, original error is what matters
      }
      throw err;
    }
  };
};

db.exec(`
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS products (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  sku TEXT,
  hsn_code TEXT,
  price REAL NOT NULL,
  tax_rate REAL NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS combos (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  code TEXT,
  description TEXT,
  discount_type TEXT NOT NULL DEFAULT 'none', -- 'none' | 'percent' | 'fixed'
  discount_value REAL NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS combo_items (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  combo_id INTEGER NOT NULL REFERENCES combos(id) ON DELETE CASCADE,
  product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
  quantity REAL NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS orders (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  combo_id INTEGER REFERENCES combos(id) ON DELETE SET NULL,
  customer_name TEXT NOT NULL,
  customer_email TEXT,
  customer_phone TEXT,
  customer_address TEXT,
  shipping_name TEXT,
  shipping_phone TEXT,
  shipping_address TEXT,
  shopify_order_no TEXT,
  portal TEXT,
  payment_mode_code TEXT,
  payment_mode_label TEXT,
  dispatch_through TEXT,
  awb_no TEXT,
  order_date TEXT NOT NULL DEFAULT (datetime('now')),
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS invoices (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  order_id INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
  invoice_number TEXT NOT NULL UNIQUE,
  hsn_code TEXT,
  quantity REAL NOT NULL,
  base_unit_price REAL NOT NULL,
  gross_amount REAL NOT NULL,
  discount_amount REAL NOT NULL DEFAULT 0,
  allocated_amount REAL NOT NULL,
  tax_rate REAL NOT NULL,
  tax_amount REAL NOT NULL,
  total_amount REAL NOT NULL,
  pdf_path TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
`);

function ensureColumn(table, column, definition) {
  const columns = db.prepare(`PRAGMA table_info(${table})`).all();
  if (!columns.some((c) => c.name === column)) {
    db.exec(`ALTER TABLE ${table} ADD COLUMN ${column} ${definition}`);
  }
}

// Added after the initial release; ALTER TABLE keeps existing dev databases
// (already populated via product imports) working without a manual reset.
ensureColumn("products", "category_code", "TEXT");
ensureColumn("orders", "invoice_number", "TEXT");
ensureColumn("orders", "pdf_path", "TEXT");
ensureColumn("orders", "customer_state_code", "TEXT");
ensureColumn("orders", "combo_quantity", "REAL NOT NULL DEFAULT 1");
ensureColumn("invoices", "cgst_amount", "REAL NOT NULL DEFAULT 0");
ensureColumn("invoices", "sgst_amount", "REAL NOT NULL DEFAULT 0");
ensureColumn("invoices", "igst_amount", "REAL NOT NULL DEFAULT 0");

const defaultSettings = {
  company_name: "[COMPANY NAME PLACEHOLDER]",
  company_address: "[COMPANY ADDRESS PLACEHOLDER]",
  company_gstin: "[GSTIN PLACEHOLDER]",
  company_email: "[EMAIL PLACEHOLDER]",
  company_phone: "[PHONE PLACEHOLDER]",
  company_logo_path: "",
  shipped_from_address: "[SHIPPED FROM ADDRESS PLACEHOLDER]",
  jurisdiction: "[JURISDICTION PLACEHOLDER]",
  default_order_no: "",
  default_portal: "ASTROTALK_STORE_SHOPIFY",
  default_payment_mode_code: "P1",
  default_payment_mode_label: "PREPAID",
  default_dispatch_through: "Shipway",
  default_awb_no: "",
  fulfillment_platform_name: "Unicommerce",
  invoice_prefix: "INV",
  invoice_next_seq: "1"
};

const insertSetting = db.prepare(
  "INSERT INTO settings (key, value) SELECT ?, ? WHERE NOT EXISTS (SELECT 1 FROM settings WHERE key = ?)"
);
for (const [key, value] of Object.entries(defaultSettings)) {
  insertSetting.run(key, value, key);
}

module.exports = db;
