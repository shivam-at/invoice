const fs = require("fs");
const path = require("path");
const PDFDocument = require("pdfkit");
const bwipjs = require("bwip-js");
const db = require("../db");
const { getAllSettings, getNextInvoiceNumber } = require("./settingsService");
const { amountInWordsINR } = require("./numberToWords");
const { stateCodeFromGstin } = require("./gstStates");

const INVOICES_DIR = path.join(__dirname, "..", "..", "storage", "invoices");
if (!fs.existsSync(INVOICES_DIR)) fs.mkdirSync(INVOICES_DIR, { recursive: true });

function money(n) {
  return Number(n.toFixed(2));
}

/**
 * Splits a combo's discount across its line items in proportion to each
 * product's own GST-inclusive selling price, then inserts one invoice-line
 * row per product. All lines for an order share a single invoice number and
 * a single PDF (one combined tax invoice per order, products as line items) —
 * that invoice number/PDF path is written onto the order itself here, since
 * the whole order is now "the invoice". PDF rendering happens separately
 * (see generateOrderInvoicePdf) because it needs async barcode generation
 * and shouldn't run inside the DB transaction.
 */
function createInvoiceRecordsForOrder(order, combo, comboItemsWithProducts, comboQuantity) {
  const lines = comboItemsWithProducts.map((item) => {
    const lineQuantity = item.quantity * comboQuantity;
    const grossAmount = item.product.price * lineQuantity; // GST-inclusive, pre-discount
    return { item, lineQuantity, grossAmount };
  });

  const sumGross = lines.reduce((acc, l) => acc + l.grossAmount, 0);

  let discountTotal = 0;
  if (combo && combo.discount_type === "percent") {
    discountTotal = sumGross * (combo.discount_value / 100);
  } else if (combo && combo.discount_type === "fixed") {
    discountTotal = Math.min(combo.discount_value, sumGross);
  }

  const invoiceNumber = getNextInvoiceNumber();
  const pdfFileName = `${invoiceNumber}.pdf`;
  db.prepare("UPDATE orders SET invoice_number = ?, pdf_path = ? WHERE id = ?").run(invoiceNumber, pdfFileName, order.id);
  order.invoice_number = invoiceNumber;
  order.pdf_path = pdfFileName;

  // Place-of-supply check: same state as the company -> CGST+SGST (split evenly);
  // different state -> IGST. Unknown customer state is treated as intra-state,
  // since that's the same "just show it as GST" behavior this replaces.
  const settings = getAllSettings();
  const companyStateCode = stateCodeFromGstin(settings.company_gstin);
  const customerStateCode = order.customer_state_code || companyStateCode;
  const isInterstate = Boolean(companyStateCode) && customerStateCode !== companyStateCode;

  const insertInvoice = db.prepare(`
    INSERT INTO invoices
      (order_id, product_id, invoice_number, hsn_code, quantity, base_unit_price,
       gross_amount, discount_amount, allocated_amount, tax_rate, tax_amount,
       cgst_amount, sgst_amount, igst_amount, total_amount, pdf_path)
    VALUES (@order_id, @product_id, @invoice_number, @hsn_code, @quantity, @base_unit_price,
       @gross_amount, @discount_amount, @allocated_amount, @tax_rate, @tax_amount,
       @cgst_amount, @sgst_amount, @igst_amount, @total_amount, @pdf_path)
  `);

  const records = [];

  lines.forEach((line, index) => {
    const shareOfDiscount = sumGross > 0 ? money(discountTotal * (line.grossAmount / sumGross)) : 0;
    const totalAmount = money(line.grossAmount - shareOfDiscount); // GST-inclusive, after discount
    const taxRate = line.item.product.tax_rate || 0;
    const taxableValue = money(totalAmount / (1 + taxRate / 100));
    const taxAmount = money(totalAmount - taxableValue);

    let cgstAmount = 0;
    let sgstAmount = 0;
    let igstAmount = 0;
    if (isInterstate) {
      igstAmount = taxAmount;
    } else {
      cgstAmount = money(taxAmount / 2);
      sgstAmount = money(taxAmount - cgstAmount);
    }

    const record = {
      order_id: order.id,
      product_id: line.item.product.id,
      invoice_number: `${invoiceNumber}-L${index + 1}`,
      hsn_code: line.item.product.hsn_code || null,
      quantity: line.lineQuantity,
      base_unit_price: line.item.product.price,
      gross_amount: money(line.grossAmount),
      discount_amount: shareOfDiscount,
      allocated_amount: taxableValue,
      tax_rate: taxRate,
      tax_amount: taxAmount,
      cgst_amount: cgstAmount,
      sgst_amount: sgstAmount,
      igst_amount: igstAmount,
      total_amount: totalAmount,
      pdf_path: pdfFileName
    };

    const info = insertInvoice.run(record);
    record.id = info.lastInsertRowid;
    record.product = line.item.product;
    records.push(record);
  });

  return records;
}

async function generateOrderInvoicePdf(records, { order, combo }) {
  const settings = getAllSettings();
  const filePath = path.join(INVOICES_DIR, order.pdf_path);
  await renderInvoicePdf(filePath, { settings, order, combo, lineItems: records });
}

async function makeBarcodeBuffer(text) {
  if (!text) return null;
  try {
    return await bwipjs.toBuffer({ bcid: "code128", text: String(text), scale: 2, height: 8, includetext: false });
  } catch (_) {
    return null;
  }
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
function formatDate(value) {
  const d = new Date(value);
  return `${String(d.getDate()).padStart(2, "0")}-${MONTHS[d.getMonth()]}-${d.getFullYear()}`;
}

async function renderInvoicePdf(filePath, { settings, order, combo, lineItems }) {
  const orderBarcode = await makeBarcodeBuffer(order.shopify_order_no);
  const awbBarcode = await makeBarcodeBuffer(order.awb_no);

  const doc = new PDFDocument({ size: "A4", margin: 30 });
  const streamFinished = new Promise((resolve, reject) => {
    const stream = fs.createWriteStream(filePath);
    stream.on("finish", resolve);
    stream.on("error", reject);
    doc.pipe(stream);
  });

  const pageWidth = doc.page.width;
  const left = 30;
  const right = pageWidth - 30;
  const contentWidth = right - left;

  doc.fontSize(13).font("Helvetica-Bold").text("Tax Invoice", left, 30, { width: contentWidth, align: "center" });

  // ---- Header block: a 2-row x 3-column grid, like the reference invoice ----
  const row1Top = 50;
  const row2Top = 210;
  const row2Bottom = 300;
  const colA = left + contentWidth * 0.42;
  const colB = left + contentWidth * 0.72;

  let y = row1Top + 8;

  doc.fontSize(9).font("Helvetica-Bold").text("BILL FROM:", left + 4, y);
  doc.font("Helvetica-Bold").text(settings.company_name, left + 4, y + 12, { width: colA - left - 12 });
  doc.font("Helvetica").fontSize(8).text(settings.company_address, left + 4, y + 24, { width: colA - left - 12 });
  const afterAddressY = doc.y;
  doc.text(`GSTIN: ${settings.company_gstin}`, left + 4, afterAddressY + 2);

  const gstinDividerY = doc.y + 8;
  doc.moveTo(left + 4, gstinDividerY).lineTo(colA - 4, gstinDividerY).stroke();

  doc.font("Helvetica-Bold").fontSize(9).text("Shipped From:", left + 4, gstinDividerY + 6);
  doc.font("Helvetica").fontSize(8).text(settings.shipped_from_address, left + 4, gstinDividerY + 18, { width: colA - left - 12 });

  doc.font("Helvetica").fontSize(8);
  doc.text("Invoice No:", colA + 4, y);
  doc.font("Helvetica-Bold").text(order.invoice_number, colA + 4, y + 11, { width: colB - colA - 8 });

  doc.font("Helvetica").text("Order No:", colA + 4, y + 30);
  doc.font("Helvetica-Bold").text(order.shopify_order_no || "-", colA + 4, y + 41, { width: colB - colA - 8 });
  doc.font("Helvetica").text(`Order Date: ${formatDate(order.order_date)}`, colA + 4, y + 56);
  if (orderBarcode) {
    doc.image(orderBarcode, colA + 4, y + 70, { fit: [colB - colA - 8, 26] });
  }

  const col3Width = right - colB - 12;
  doc.font("Helvetica").text("Invoice Date:", colB + 4, y);
  doc.font("Helvetica-Bold").text(formatDate(order.order_date), colB + 4, y + 11, { width: col3Width });

  doc.font("Helvetica").fontSize(8).text(`Portal: ${order.portal || "-"}`, colB + 4, y + 40, { width: col3Width });
  doc.text(`Payment Mode: ${order.payment_mode_code || ""}`, colB + 4, doc.y + 6, { width: col3Width });
  doc.font("Helvetica-Bold").text(order.payment_mode_label || "", colB + 4, doc.y + 1, { width: col3Width });

  // ---- Bill To / Ship To / Dispatch (same 3 columns as the row above) ----
  y = row2Top + 8;

  doc.font("Helvetica-Bold").fontSize(9).text("Bill To:", left + 4, y);
  doc.font("Helvetica-Bold").fontSize(8).text(order.customer_name, left + 4, y + 11, { width: colA - left - 12 });
  doc.font("Helvetica").text(order.customer_address || "", left + 4, y + 22, { width: colA - left - 12 });
  doc.text(order.customer_phone ? `T: ${order.customer_phone}` : "", left + 4, doc.y + 2);

  doc.font("Helvetica-Bold").fontSize(9).text("Ship To:", colA + 4, y);
  doc.font("Helvetica-Bold").fontSize(8).text(order.shipping_name || order.customer_name, colA + 4, y + 11, { width: colB - colA - 12 });
  doc.font("Helvetica").text(order.shipping_address || order.customer_address || "", colA + 4, y + 22, { width: colB - colA - 12 });
  doc.text(order.shipping_phone ? `T: ${order.shipping_phone}` : "", colA + 4, doc.y + 2);

  doc.font("Helvetica-Bold").fontSize(9).text("Dispatch Through:", colB + 4, y);
  doc.font("Helvetica").fontSize(8).text(order.dispatch_through || "-", colB + 4, y + 11, { width: right - colB - 12 });
  doc.text(`AWB No: ${order.awb_no || "-"}`, colB + 4, y + 23);
  if (awbBarcode) {
    doc.image(awbBarcode, colB + 4, y + 36, { fit: [right - colB - 12, 24] });
  }

  // ---- Grid lines for the whole header block ----
  doc.rect(left, row1Top, contentWidth, row2Bottom - row1Top).stroke();
  doc.moveTo(left, row2Top).lineTo(right, row2Top).stroke();
  doc.moveTo(colA, row1Top).lineTo(colA, row2Bottom).stroke();
  doc.moveTo(colB, row1Top).lineTo(colB, row2Bottom).stroke();

  y = row2Bottom + 10;

  // ---- Table 1: one row per product, gross amount / discount / amount ----
  const t1Cols = [
    { key: "sr", label: "Sr", width: 20, align: "left" },
    { key: "product", label: "Product Name", width: 110, align: "left" },
    { key: "code", label: "Product Code", width: 55, align: "left" },
    { key: "hsn", label: "HSN Code", width: 50, align: "left" },
    { key: "qty", label: "Qty", width: 25, align: "right" },
    { key: "rate", label: "Rate", width: 48, align: "right" },
    { key: "gross", label: "Gross Amt\nIncl GST", width: 55, align: "right" },
    { key: "discount", label: "Discount", width: 45, align: "right" },
    { key: "credit", label: "Store\nCredit", width: 40, align: "right" },
    { key: "amount", label: "Amount\n(INR)", width: 60, align: "right" }
  ];

  const totalQty = lineItems.reduce((acc, inv) => acc + inv.quantity, 0);
  const totalAmount = lineItems.reduce((acc, inv) => acc + inv.total_amount, 0);
  const totalTaxable = lineItems.reduce((acc, inv) => acc + inv.allocated_amount, 0);

  // When the order came from a combo, group its products under a bold combo
  // header row (name/code/number of sets, no money columns) with each real
  // product listed below it, code in parentheses — matches the reference
  // Unicommerce/Shopify invoice format for bundle line items.
  const t1Rows = [];
  const t1BoldRows = [];
  if (combo) {
    t1Rows.push(["1", combo.name, combo.code || "-", "-", String(order.combo_quantity || 1), "", "", "", "", ""]);
    t1BoldRows.push(true);
    lineItems.forEach((invoice) => {
      t1Rows.push([
        "",
        invoice.product.name,
        `(${invoice.product.sku || "-"})`,
        invoice.hsn_code || "-",
        String(invoice.quantity),
        invoice.base_unit_price.toFixed(2),
        invoice.gross_amount.toFixed(2),
        invoice.discount_amount.toFixed(2),
        "0.00",
        invoice.total_amount.toFixed(2)
      ]);
      t1BoldRows.push(false);
    });
  } else {
    lineItems.forEach((invoice, i) => {
      t1Rows.push([
        String(i + 1),
        invoice.product.name,
        invoice.product.sku || "-",
        invoice.hsn_code || "-",
        String(invoice.quantity),
        invoice.base_unit_price.toFixed(2),
        invoice.gross_amount.toFixed(2),
        invoice.discount_amount.toFixed(2),
        "0.00",
        invoice.total_amount.toFixed(2)
      ]);
      t1BoldRows.push(false);
    });
  }

  y = drawTable(doc, {
    x: left,
    y,
    columns: t1Cols,
    rows: t1Rows,
    boldRows: t1BoldRows,
    totalRow: ["", "", "", "", String(totalQty), "", "", "", "", totalAmount.toFixed(2)],
    totalLabel: "Total:"
  });

  y += 8;

  // ---- Table 2: one row per product, taxable value / GST breakdown ----
  // Whether the order is intra-state (CGST+SGST) or inter-state (IGST) was
  // decided once at creation time and is frozen onto each line item, so the
  // PDF just reflects whichever tax columns actually have amounts.
  const isInterstate = lineItems.some((inv) => inv.igst_amount > 0);

  doc
    .font("Helvetica-Bold")
    .fontSize(8)
    .text(
      isInterstate ? "Place of Supply: Inter-State (IGST applicable)" : "Place of Supply: Intra-State (CGST + SGST applicable)",
      left,
      y
    );
  y = doc.y + 3;
  doc.font("Helvetica").fontSize(8).text("*Breakdown of Invoice Value is as follows", left, y);
  y = doc.y + 4;

  const totalCgst = lineItems.reduce((acc, inv) => acc + inv.cgst_amount, 0);
  const totalSgst = lineItems.reduce((acc, inv) => acc + inv.sgst_amount, 0);
  const totalIgst = lineItems.reduce((acc, inv) => acc + inv.igst_amount, 0);

  const t2Cols = [
    { key: "sr", label: "Sr", width: 20, align: "left" },
    { key: "product", label: "Product Name", width: 110, align: "left" },
    { key: "code", label: "Product Code", width: 55, align: "left" },
    { key: "hsn", label: "HSN Code", width: 50, align: "left" },
    { key: "qty", label: "Qty", width: 25, align: "right" },
    { key: "taxable", label: "Taxable\nValue (INR)", width: 60, align: "right" },
    ...(isInterstate
      ? [{ key: "igst", label: "IGST (INR)", width: 70, align: "right" }]
      : [
          { key: "cgst", label: "CGST (INR)", width: 55, align: "right" },
          { key: "sgst", label: "SGST (INR)", width: 55, align: "right" }
        ]),
    { key: "amount", label: "Amount\n(INR)", width: 60, align: "right" }
  ];

  const emptyTaxCells = isInterstate ? [""] : ["", ""];
  const t2Rows = [];
  const t2BoldRows = [];
  if (combo) {
    t2Rows.push(["1", combo.name, combo.code || "-", "-", String(order.combo_quantity || 1), "", ...emptyTaxCells, ""]);
    t2BoldRows.push(true);
    lineItems.forEach((invoice) => {
      t2Rows.push([
        "",
        invoice.product.name,
        `(${invoice.product.sku || "-"})`,
        invoice.hsn_code || "-",
        String(invoice.quantity),
        invoice.allocated_amount.toFixed(2),
        ...(isInterstate
          ? [`${invoice.igst_amount.toFixed(2)} (${invoice.tax_rate.toFixed(2)}%)`]
          : [
              `${invoice.cgst_amount.toFixed(2)} (${(invoice.tax_rate / 2).toFixed(2)}%)`,
              `${invoice.sgst_amount.toFixed(2)} (${(invoice.tax_rate / 2).toFixed(2)}%)`
            ]),
        invoice.total_amount.toFixed(2)
      ]);
      t2BoldRows.push(false);
    });
  } else {
    lineItems.forEach((invoice, i) => {
      t2Rows.push([
        String(i + 1),
        invoice.product.name,
        invoice.product.sku || "-",
        invoice.hsn_code || "-",
        String(invoice.quantity),
        invoice.allocated_amount.toFixed(2),
        ...(isInterstate
          ? [`${invoice.igst_amount.toFixed(2)} (${invoice.tax_rate.toFixed(2)}%)`]
          : [
              `${invoice.cgst_amount.toFixed(2)} (${(invoice.tax_rate / 2).toFixed(2)}%)`,
              `${invoice.sgst_amount.toFixed(2)} (${(invoice.tax_rate / 2).toFixed(2)}%)`
            ]),
        invoice.total_amount.toFixed(2)
      ]);
      t2BoldRows.push(false);
    });
  }

  y = drawTable(doc, {
    x: left,
    y,
    columns: t2Cols,
    rows: t2Rows,
    boldRows: t2BoldRows,
    totalRow: [
      "",
      "",
      "",
      "",
      String(totalQty),
      totalTaxable.toFixed(2),
      ...(isInterstate ? [totalIgst.toFixed(2)] : [totalCgst.toFixed(2), totalSgst.toFixed(2)]),
      totalAmount.toFixed(2)
    ],
    totalLabel: "Total:"
  });

  y += 12;
  doc.font("Helvetica-Bold").fontSize(8).text("Amount Chargeable (in words)", left, y);
  doc.font("Helvetica-Bold").text("E. & O.E", right - 60, y, { width: 60, align: "right" });
  y = doc.y + 2;
  doc.font("Helvetica-Bold").text(`INR ${amountInWordsINR(totalAmount)}`, left, y, { width: contentWidth });
  y = doc.y + 8;

  doc.font("Helvetica").fontSize(8).text("Tax is payable on reverse charge basis: No", left, y);
  y = doc.y + 4;
  doc.moveTo(left, y).lineTo(right, y).stroke();
  y += 8;

  const declarationWidth = contentWidth * 0.6;
  doc.font("Helvetica-Bold").fontSize(8).text("Declaration", left, y);
  doc.font("Helvetica").fontSize(7).text(
    "1. This is a computer generated Invoice. Doesn't require signature or stamp.\n" +
      "2. All figures are shown in INR.\n" +
      "3. Shipping/Handling charges are inclusive of GST.\n" +
      `4. All disputes are subject to ${settings.jurisdiction} jurisdiction only.`,
    left,
    y + 12,
    { width: declarationWidth }
  );

  const boxX = left + declarationWidth + 20;
  const boxWidth = right - boxX;
  doc.rect(boxX, y, boxWidth, 70).stroke();
  doc.font("Helvetica-Bold").fontSize(8).text(`For ${settings.company_name}`, boxX, y + 8, { width: boxWidth, align: "center" });
  doc.font("Helvetica").fontSize(7).text("Authorised Signatory", boxX, y + 55, { width: boxWidth, align: "center" });

  if (settings.fulfillment_platform_name) {
    const footerY = doc.page.height - 60;
    doc.rect(left, footerY, contentWidth, 26).stroke();
    doc.font("Helvetica-Bold").fontSize(8).text("Bill By:", left + 8, footerY + 4);
    doc.font("Helvetica").text(`Powered By ${settings.fulfillment_platform_name}`, left + 8, footerY + 15);
    doc.text("This is a computer generated Invoice", left, footerY + 10, { width: contentWidth, align: "center" });
  }

  doc.end();
  await streamFinished;
}

/**
 * Draws a simple bordered table with a header row, data rows, and an optional
 * total row. Returns the y position immediately below the table.
 */
function drawTable(doc, { x, y, columns, rows, totalRow, totalLabel, boldRows }) {
  const headerHeight = 22;
  const minRowHeight = 16;
  const totalWidth = columns.reduce((acc, c) => acc + c.width, 0);

  doc.font("Helvetica-Bold").fontSize(7);
  let cx = x;
  for (const col of columns) {
    doc.text(col.label, cx + 2, y + 2, { width: col.width - 4, align: col.align });
    cx += col.width;
  }
  doc.rect(x, y, totalWidth, headerHeight).stroke();
  cx = x;
  for (const col of columns) {
    doc.moveTo(cx, y).lineTo(cx, y + headerHeight).stroke();
    cx += col.width;
  }
  doc.moveTo(cx, y).lineTo(cx, y + headerHeight).stroke();

  let rowY = y + headerHeight;
  for (let r = 0; r < rows.length; r++) {
    const row = rows[r];
    const font = boldRows && boldRows[r] ? "Helvetica-Bold" : "Helvetica";
    doc.font(font).fontSize(7);

    // Long wrapped cells (e.g. a combo's full name) need more than one line —
    // measure the tallest cell so its text doesn't spill into the row below.
    let rowHeight = minRowHeight;
    for (let i = 0; i < columns.length; i++) {
      const cellHeight = doc.heightOfString(row[i] || "", { width: columns[i].width - 4 }) + 8;
      if (cellHeight > rowHeight) rowHeight = cellHeight;
    }

    cx = x;
    for (let i = 0; i < columns.length; i++) {
      doc.text(row[i], cx + 2, rowY + 4, { width: columns[i].width - 4, align: columns[i].align });
      cx += columns[i].width;
    }
    doc.rect(x, rowY, totalWidth, rowHeight).stroke();
    cx = x;
    for (const col of columns) {
      doc.moveTo(cx, rowY).lineTo(cx, rowY + rowHeight).stroke();
      cx += col.width;
    }
    doc.moveTo(cx, rowY).lineTo(cx, rowY + rowHeight).stroke();
    rowY += rowHeight;
  }

  if (totalRow) {
    cx = x;
    doc.font("Helvetica-Bold").fontSize(7);
    for (let i = 0; i < columns.length; i++) {
      const text = i === 1 && totalLabel ? totalLabel : totalRow[i];
      doc.text(text, cx + 2, rowY + 4, { width: columns[i].width - 4, align: columns[i].align });
      cx += columns[i].width;
    }
    doc.rect(x, rowY, totalWidth, minRowHeight).stroke();
    rowY += minRowHeight;
  }

  return rowY;
}

module.exports = { createInvoiceRecordsForOrder, generateOrderInvoicePdf, INVOICES_DIR };
