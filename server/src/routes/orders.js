const express = require("express");
const fs = require("fs");
const path = require("path");
const db = require("../db");
const { createInvoiceRecordsForOrder, generateOrderInvoicePdf, INVOICES_DIR } = require("../services/invoiceService");
const { getAllSettings } = require("../services/settingsService");

const router = express.Router();

function getInvoicesForOrder(orderId) {
  return db
    .prepare(
      `SELECT i.*, p.name AS product_name, p.sku AS product_sku
       FROM invoices i JOIN products p ON p.id = i.product_id
       WHERE i.order_id = ? ORDER BY i.id`
    )
    .all(orderId);
}

router.get("/", (_req, res) => {
  const orders = db.prepare("SELECT * FROM orders ORDER BY created_at DESC").all();
  res.json(orders);
});

router.get("/:id", (req, res) => {
  const order = db.prepare("SELECT * FROM orders WHERE id = ?").get(req.params.id);
  if (!order) return res.status(404).json({ error: "Order not found" });
  res.json({ ...order, invoices: getInvoicesForOrder(order.id) });
});

router.get("/:id/pdf", (req, res) => {
  const order = db.prepare("SELECT * FROM orders WHERE id = ?").get(req.params.id);
  if (!order || !order.pdf_path) return res.status(404).json({ error: "Invoice not found for this order" });
  const filePath = path.join(INVOICES_DIR, order.pdf_path);
  if (!fs.existsSync(filePath)) return res.status(404).json({ error: "PDF file missing" });
  res.download(filePath, `${order.invoice_number}.pdf`);
});

router.post("/", async (req, res) => {
  const {
    combo_id,
    combo_quantity,
    customer_name,
    customer_email,
    customer_phone,
    customer_address,
    shipping_name,
    shipping_phone,
    shipping_address,
    shopify_order_no,
    portal,
    payment_mode_code,
    payment_mode_label,
    dispatch_through,
    awb_no,
    customer_state_code
  } = req.body;

  if (!combo_id || !customer_name) {
    return res.status(400).json({ error: "combo_id and customer_name are required" });
  }

  const combo = db.prepare("SELECT * FROM combos WHERE id = ?").get(combo_id);
  if (!combo) return res.status(404).json({ error: "Combo not found" });

  const comboItems = db
    .prepare(
      `SELECT ci.quantity, p.id, p.name, p.sku, p.hsn_code, p.price, p.tax_rate
       FROM combo_items ci JOIN products p ON p.id = ci.product_id
       WHERE ci.combo_id = ?`
    )
    .all(combo_id)
    .map((row) => ({
      quantity: row.quantity,
      product: {
        id: row.id,
        name: row.name,
        sku: row.sku,
        hsn_code: row.hsn_code,
        price: row.price,
        tax_rate: row.tax_rate
      }
    }));

  if (comboItems.length === 0) {
    return res.status(400).json({ error: "Combo has no items" });
  }

  const qty = Number(combo_quantity) > 0 ? Number(combo_quantity) : 1;
  const settings = getAllSettings();

  let order;
  let invoiceRecords;
  const tx = db.transaction(() => {
    const info = db
      .prepare(
        `INSERT INTO orders (
           combo_id, customer_name, customer_email, customer_phone, customer_address,
           shipping_name, shipping_phone, shipping_address,
           shopify_order_no, portal, payment_mode_code, payment_mode_label,
           dispatch_through, awb_no, customer_state_code, combo_quantity
         ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
      )
      .run(
        combo_id,
        customer_name,
        customer_email || null,
        customer_phone || null,
        customer_address || null,
        shipping_name || null,
        shipping_phone || null,
        shipping_address || null,
        shopify_order_no || settings.default_order_no || null,
        portal || settings.default_portal || null,
        payment_mode_code || settings.default_payment_mode_code || null,
        payment_mode_label || settings.default_payment_mode_label || null,
        dispatch_through || settings.default_dispatch_through || null,
        awb_no || settings.default_awb_no || null,
        customer_state_code || null,
        qty
      );
    order = db.prepare("SELECT * FROM orders WHERE id = ?").get(info.lastInsertRowid);
    invoiceRecords = createInvoiceRecordsForOrder(order, combo, comboItems, qty);
  });

  try {
    tx();
    await generateOrderInvoicePdf(invoiceRecords, { order, combo });
    res.status(201).json({ ...order, invoices: getInvoicesForOrder(order.id) });
  } catch (err) {
    res.status(400).json({ error: err.message });
  }
});

module.exports = router;
