import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { OrdersApi } from "../api/client.js";

export default function OrdersPage() {
  const [orders, setOrders] = useState([]);
  const [error, setError] = useState("");

  useEffect(() => {
    OrdersApi.list().then(setOrders).catch((e) => setError(e.message));
  }, []);

  return (
    <div>
      <h2>Orders</h2>
      {error && <div className="alert error">{error}</div>}
      <div className="card">
        <table>
          <thead>
            <tr>
              <th>Order</th>
              <th>Customer</th>
              <th>Date</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {orders.map((o) => (
              <tr key={o.id}>
                <td>ORD-{String(o.id).padStart(5, "0")}</td>
                <td>{o.customer_name}</td>
                <td>{new Date(o.order_date).toLocaleDateString("en-IN")}</td>
                <td>
                  <Link to={`/orders/${o.id}`}>View Invoices</Link>
                </td>
              </tr>
            ))}
            {orders.length === 0 && (
              <tr>
                <td colSpan={4} className="muted">
                  No orders yet.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
