import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { OrdersApi, StatsApi } from "../api/client.js";

const STATUS_CLASS = {
  PENDING: "badge",
  QUEUED: "badge",
  GENERATING: "badge",
  GENERATED: "badge",
  PRINTING: "badge",
  PRINTED: "badge success",
  FAILED: "badge danger"
};

export default function OrdersPage() {
  const [orders, setOrders] = useState([]);
  const [stats, setStats] = useState(null);
  const [error, setError] = useState("");
  const [running, setRunning] = useState(false);

  const load = () => {
    OrdersApi.list().then(setOrders).catch((e) => setError(e.response?.data?.error || e.message));
    StatsApi.get().then(setStats).catch(() => {});
  };

  useEffect(() => {
    load();
    const handle = setInterval(load, 3000);
    return () => clearInterval(handle);
  }, []);

  const runIdentify = async () => {
    setRunning(true);
    setError("");
    try {
      await OrdersApi.identifyCMD(100);
      load();
    } catch (err) {
      setError(err.response?.data?.error || err.message);
    } finally {
      setRunning(false);
    }
  };

  return (
    <div>
      <h2>Orders</h2>
      <p className="muted">
        Orders move through the pipeline automatically: PENDING → QUEUED → GENERATING → GENERATED → PRINTING → PRINTED.
        This list refreshes every few seconds.
      </p>
      {error && <div className="alert error">{error}</div>}

      {stats && (
        <div className="card">
          <div className="row between">
            <div className="row" style={{ gap: 16 }}>
              {Object.entries(stats).map(([status, count]) => (
                <span key={status} className="muted">
                  {status}: <strong>{count}</strong>
                </span>
              ))}
            </div>
            <button className="secondary" onClick={runIdentify} disabled={running}>
              {running ? "Running..." : "Process pending CMD orders"}
            </button>
          </div>
        </div>
      )}

      <div className="card">
        <table>
          <thead>
            <tr>
              <th>Order No</th>
              <th>Customer</th>
              <th>Status</th>
              <th>Created</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {orders.map((o) => (
              <tr key={o.id}>
                <td>{o.order_no}</td>
                <td>{o.customer_name}</td>
                <td>
                  <span className={STATUS_CLASS[o.status] || "badge"}>{o.status}</span>
                </td>
                <td>{new Date(o.created_at).toLocaleString("en-IN")}</td>
                <td>
                  <Link to={`/orders/${o.id}`}>View</Link>
                </td>
              </tr>
            ))}
            {orders.length === 0 && (
              <tr>
                <td colSpan={5} className="muted">
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
