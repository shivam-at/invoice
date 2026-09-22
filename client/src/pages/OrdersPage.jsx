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

const PAGE_SIZE = 50;

export default function OrdersPage() {
  const [orders, setOrders] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [stats, setStats] = useState(null);
  const [error, setError] = useState("");
  const [running, setRunning] = useState(false);

  const [sheetUrl, setSheetUrl] = useState(() => {
    try {
      return localStorage.getItem("orders_sheet_url") || "";
    } catch (_) {
      return "";
    }
  });
  const [sheetRange, setSheetRange] = useState(() => {
    try {
      return localStorage.getItem("orders_sheet_range") || "";
    } catch (_) {
      return "";
    }
  });
  const [rangeStart, setRangeStart] = useState(0);
  const [rangeCount, setRangeCount] = useState(50);
  const [importing, setImporting] = useState(false);
  const [importResult, setImportResult] = useState(null);

  const load = () => {
    OrdersApi.list({ search, page, pageSize: PAGE_SIZE })
      .then((res) => {
        setOrders(res.items);
        setTotal(res.total);
      })
      .catch((e) => setError(e.response?.data?.error || e.message));
    StatsApi.get().then(setStats).catch(() => {});
  };

  // Debounce the search box so we don't fire a request per keystroke.
  useEffect(() => {
    const handle = setTimeout(() => {
      setPage(1);
      setSearch(searchInput);
    }, 300);
    return () => clearTimeout(handle);
  }, [searchInput]);

  useEffect(() => {
    load();
    const handle = setInterval(load, 3000);
    return () => clearInterval(handle);
  }, [search, page]);

  useEffect(() => {
    try {
      localStorage.setItem("orders_sheet_url", sheetUrl);
      localStorage.setItem("orders_sheet_range", sheetRange);
    } catch (_) {
      // localStorage unavailable (private browsing etc.) — not critical, just skip persisting
    }
  }, [sheetUrl, sheetRange]);

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

  const runImport = async (e) => {
    e.preventDefault();
    setError("");
    setImportResult(null);
    if (!sheetUrl) {
      setError("Paste the Google Sheet URL first");
      return;
    }
    setImporting(true);
    try {
      const result = await OrdersApi.importGoogleSheet({
        sheet_url: sheetUrl,
        range: sheetRange || undefined,
        offset: Number(rangeStart) || 0,
        limit: Number(rangeCount) || 50
      });
      setImportResult(result);
      load();
    } catch (err) {
      setError(err.response?.data?.error || err.message);
    } finally {
      setImporting(false);
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

      <div className="card">
        <h3>Import Orders from Google Sheet</h3>
        <p className="muted">
          Reads a Unicommerce "Sale Order Item" export (one row per line item, grouped here by Display Order Code into
          one order each). Pick which range of orders to bring in — useful for importing a large sheet in batches.
        </p>
        <form onSubmit={runImport}>
          <div className="form-grid">
            <div>
              <label>Google Sheet URL</label>
              <input value={sheetUrl} onChange={(e) => setSheetUrl(e.target.value)} placeholder="https://docs.google.com/spreadsheets/d/..." />
            </div>
            <div>
              <label>Sheet/Tab Name</label>
              <input value={sheetRange} onChange={(e) => setSheetRange(e.target.value)} placeholder="Sheet1" />
            </div>
            <div>
              <label>Start from order # (0 = first)</label>
              <input type="number" min="0" value={rangeStart} onChange={(e) => setRangeStart(e.target.value)} />
            </div>
            <div>
              <label>How many orders to import</label>
              <input type="number" min="1" value={rangeCount} onChange={(e) => setRangeCount(e.target.value)} />
            </div>
          </div>
          <div style={{ marginTop: 14 }}>
            <button type="submit" disabled={importing}>
              {importing ? "Importing..." : "Import Orders"}
            </button>
          </div>
        </form>
        {importResult && (
          <div className="alert success" style={{ marginTop: 12 }}>
            Created {importResult.createdCount}, skipped {importResult.skippedCount} — out of {importResult.totalGroups}{" "}
            distinct orders in the sheet. Range covered: orders {rangeStart} to{" "}
            {Number(rangeStart) + importResult.createdCount + importResult.skippedCount}.
            {importResult.skippedCount > 0 && (
              <details style={{ marginTop: 8 }}>
                <summary>Why orders were skipped</summary>
                <ul>
                  {importResult.skipped.slice(0, 20).map((s, i) => (
                    <li key={i}>
                      {s.order_code}: {s.reason}
                    </li>
                  ))}
                </ul>
                {importResult.skipped.length > 20 && <p className="muted">...and {importResult.skipped.length - 20} more.</p>}
              </details>
            )}
          </div>
        )}
      </div>

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
              {running ? "Running..." : "Process pending orders"}
            </button>
          </div>
        </div>
      )}

      <div className="card">
        <div className="row between">
          <h3 style={{ margin: 0 }}>All Orders ({total.toLocaleString()})</h3>
          <input
            style={{ maxWidth: 280 }}
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            placeholder="Search by order no or customer..."
          />
        </div>
        <table style={{ marginTop: 14 }}>
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
                  {search ? "No orders match your search." : "No orders yet."}
                </td>
              </tr>
            )}
          </tbody>
        </table>
        <div className="pagination">
          <button className="secondary" onClick={() => setPage((p) => Math.max(1, p - 1))} disabled={page <= 1}>
            Prev
          </button>
          <span className="muted">
            Page {page} of {Math.max(1, Math.ceil(total / PAGE_SIZE))}
          </span>
          <button
            className="secondary"
            onClick={() => setPage((p) => Math.min(Math.ceil(total / PAGE_SIZE) || 1, p + 1))}
            disabled={page >= Math.ceil(total / PAGE_SIZE)}
          >
            Next
          </button>
        </div>
      </div>
    </div>
  );
}
