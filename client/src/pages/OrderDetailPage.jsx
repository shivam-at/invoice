import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { OrdersApi } from "../api/client.js";

export default function OrderDetailPage() {
  const { id } = useParams();
  const [order, setOrder] = useState(null);
  const [error, setError] = useState("");

  useEffect(() => {
    OrdersApi.get(id).then(setOrder).catch((e) => setError(e.response?.data?.error || e.message));
  }, [id]);

  if (error) return <div className="alert error">{error}</div>;
  if (!order) return <p className="muted">Loading...</p>;

  const total = order.invoices.reduce((acc, inv) => acc + inv.total_amount, 0);
  const isInterstate = order.invoices.some((inv) => inv.igst_amount > 0);

  return (
    <div>
      <h2>Order ORD-{String(order.id).padStart(5, "0")}</h2>
      <div className="card">
        <div className="row between">
          <div>
            <strong>{order.customer_name}</strong>
            <div className="muted">{order.customer_email} {order.customer_phone}</div>
            <div className="muted">{order.customer_address}</div>
            <div className="muted" style={{ marginTop: 6 }}>
              Invoice No: <strong>{order.invoice_number}</strong>
            </div>
          </div>
          <a href={OrdersApi.pdfUrl(order.id)}>
            <button>Download Invoice PDF</button>
          </a>
        </div>
      </div>

      <div className="card">
        <h3>Line Items</h3>
        <p className="muted">Each product prints as one line on the single order invoice above.</p>
        <p className="muted">
          Supply type: <strong>{isInterstate ? "Inter-State (IGST)" : "Intra-State (CGST + SGST)"}</strong>
        </p>
        <table>
          <thead>
            <tr>
              <th>Product</th>
              <th>Qty</th>
              <th>Taxable Value</th>
              <th>CGST</th>
              <th>SGST</th>
              <th>IGST</th>
              <th>Total</th>
            </tr>
          </thead>
          <tbody>
            {order.invoices.map((inv) => (
              <tr key={inv.id}>
                <td>{inv.product_name}</td>
                <td>{inv.quantity}</td>
                <td>₹{inv.allocated_amount.toFixed(2)}</td>
                <td>₹{inv.cgst_amount.toFixed(2)}</td>
                <td>₹{inv.sgst_amount.toFixed(2)}</td>
                <td>₹{inv.igst_amount.toFixed(2)}</td>
                <td>₹{inv.total_amount.toFixed(2)}</td>
              </tr>
            ))}
          </tbody>
          <tfoot>
            <tr>
              <td colSpan={6} style={{ textAlign: "right", fontWeight: 600 }}>
                Order Total
              </td>
              <td style={{ fontWeight: 600 }}>₹{total.toFixed(2)}</td>
            </tr>
          </tfoot>
        </table>
      </div>
    </div>
  );
}
