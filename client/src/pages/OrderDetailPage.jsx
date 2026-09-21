import { useEffect, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { OrdersApi } from "../api/client.js";

const TERMINAL_STATUSES = ["PRINTED", "FAILED"];

export default function OrderDetailPage() {
  const { id } = useParams();
  const [data, setData] = useState(null);
  const [error, setError] = useState("");
  const intervalRef = useRef(null);

  useEffect(() => {
    const load = () => {
      OrdersApi.get(id)
        .then((res) => {
          setData(res);
          if (TERMINAL_STATUSES.includes(res.order.status) && intervalRef.current) {
            clearInterval(intervalRef.current);
            intervalRef.current = null;
          }
        })
        .catch((e) => setError(e.response?.data?.error || e.message));
    };
    load();
    intervalRef.current = setInterval(load, 1500);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [id]);

  if (error) return <div className="alert error">{error}</div>;
  if (!data) return <p className="muted">Loading...</p>;

  const { order, invoice, extra_items: extraItems = [] } = data;
  const isTerminal = TERMINAL_STATUSES.includes(order.status);

  return (
    <div>
      <h2>Order {order.order_no}</h2>
      <div className="card">
        <div className="row between">
          <div>
            <strong>{order.customer_name}</strong>
            <div className="muted">{order.customer_address}</div>
            <div className="muted" style={{ marginTop: 6 }}>
              Status: <span className="badge">{order.status}</span>
              {!isTerminal && <span className="muted"> — updating live...</span>}
            </div>
            {invoice && (
              <div className="muted" style={{ marginTop: 6 }}>
                Invoice No: <strong>{invoice.invoice_number}</strong> — Total: ₹{invoice.total_amount.toFixed(2)}
              </div>
            )}
          </div>
          {invoice && (
            <a href={OrdersApi.pdfUrl(order.id)} target="_blank" rel="noreferrer">
              <button>Download Invoice PDF</button>
            </a>
          )}
        </div>
      </div>

      {extraItems.length > 0 && (
        <div className="card">
          <h3>Extra Items (not part of the combo)</h3>
          <table>
            <thead>
              <tr>
                <th>Product</th>
                <th>Qty</th>
                <th>Unit Price</th>
              </tr>
            </thead>
            <tbody>
              {extraItems.map((it) => (
                <tr key={it.id}>
                  <td>{it.product.name}</td>
                  <td>{it.quantity}</td>
                  <td>₹{it.unit_price.toFixed(2)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {order.status === "FAILED" && (
        <div className="alert error">
          This order failed to generate/print after retries. Check the worker/printer logs and the Redis dead-letter
          queues for details.
        </div>
      )}
    </div>
  );
}
