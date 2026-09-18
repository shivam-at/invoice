const db = require("../db");

function getAllSettings() {
  const rows = db.prepare("SELECT key, value FROM settings").all();
  const settings = {};
  for (const row of rows) settings[row.key] = row.value;
  return settings;
}

function updateSettings(partial) {
  const upsert = db.prepare(
    "INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value"
  );
  const tx = db.transaction((entries) => {
    for (const [key, value] of entries) upsert.run(key, String(value));
  });
  tx(Object.entries(partial));
  return getAllSettings();
}

function getNextInvoiceNumber() {
  const settings = getAllSettings();
  const prefix = settings.invoice_prefix || "INV";
  const seq = parseInt(settings.invoice_next_seq || "1", 10);
  const number = `${prefix}-${String(seq).padStart(5, "0")}`;
  updateSettings({ invoice_next_seq: String(seq + 1) });
  return number;
}

module.exports = { getAllSettings, updateSettings, getNextInvoiceNumber };
