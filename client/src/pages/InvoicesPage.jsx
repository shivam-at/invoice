import { useEffect, useState } from "react";
import { InvoicesApi } from "../api/client.js";

export default function InvoicesPage() {
  const [invoices, setInvoices] = useState([]);
  const [error, setError] = useState("");

  useEffect(() => {
    InvoicesApi.list().then(setInvoices).catch((e) => setError(e.message));
  }, []);

  return (
    <div>
      <h2>All Invoices</h2>
      {error && <div className="alert error">{error}</div>}
      <div className="card">
        <table>
          <thead>
            <tr>
              <th>Invoice #</th>
              <th>Customer</th>
              <th>Product</th>
              <th>Qty</th>
              <th>Total</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {invoices.map((inv) => (
              <tr key={inv.id}>
                <td>{inv.invoice_number}</td>
                <td>{inv.customer_name}</td>
                <td>{inv.product_name}</td>
                <td>{inv.quantity}</td>
                <td>₹{inv.total_amount.toFixed(2)}</td>
                <td>
                  <a href={InvoicesApi.pdfUrl(inv.id)}>Download PDF</a>
                </td>
              </tr>
            ))}
            {invoices.length === 0 && (
              <tr>
                <td colSpan={6} className="muted">
                  No invoices generated yet.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
