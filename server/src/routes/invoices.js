const express = require("express");
const fs = require("fs");
const path = require("path");
const db = require("../db");
const { INVOICES_DIR } = require("../services/invoiceService");

const router = express.Router();

router.get("/", (req, res) => {
  const { order_id } = req.query;
  const rows = order_id
    ? db
        .prepare(
          `SELECT i.*, p.name AS product_name, p.sku AS product_sku, o.customer_name
           FROM invoices i
           JOIN products p ON p.id = i.product_id
           JOIN orders o ON o.id = i.order_id
           WHERE i.order_id = ? ORDER BY i.id`
        )
        .all(order_id)
    : db
        .prepare(
          `SELECT i.*, p.name AS product_name, p.sku AS product_sku, o.customer_name
           FROM invoices i
           JOIN products p ON p.id = i.product_id
           JOIN orders o ON o.id = i.order_id
           ORDER BY i.id DESC`
        )
        .all();
  res.json(rows);
});

router.get("/:id/pdf", (req, res) => {
  const invoice = db.prepare("SELECT * FROM invoices WHERE id = ?").get(req.params.id);
  if (!invoice) return res.status(404).json({ error: "Invoice not found" });
  const filePath = path.join(INVOICES_DIR, invoice.pdf_path);
  if (!fs.existsSync(filePath)) return res.status(404).json({ error: "PDF file missing" });
  res.download(filePath, `${invoice.invoice_number}.pdf`);
});

module.exports = router;
