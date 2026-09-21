import { useEffect, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { OrdersApi } from "../api/client.js";

const TERMINAL_STATUSES = ["PRINTED", "FAILED"];

export default function OrderDetailPage() {
  const { id } = useParams();
  const [data, setData] = useState(null);
  const [error, setError] = useState("");
  const [printUrl, setPrintUrl] = useState(null);
  const intervalRef = useRef(null);
  const printFrameRef = useRef(null);

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

  // Loads the PDF into a hidden iframe and, once it's actually rendered,
  // triggers the browser's native print dialog on it — that dialog is what
  // lets the user pick which installed printer to send it to (or save as
  // PDF), rather than just downloading the file.
  const handlePrint = () => {
    setPrintUrl(OrdersApi.pdfUrl(id) + `?t=${Date.now()}`);
  };

  useEffect(() => {
    if (!printUrl || !printFrameRef.current) return;
    const iframe = printFrameRef.current;
    const onLoad = () => {
      try {
        iframe.contentWindow.focus();
        iframe.contentWindow.print();
      } catch (_) {
        // Some browsers block scripted printing of cross-origin/plugin-rendered
        // PDFs — falling back to just opening it is still better than nothing.
        window.open(OrdersApi.pdfUrl(id), "_blank");
      }
    };
    iframe.addEventListener("load", onLoad);
    return () => iframe.removeEventListener("load", onLoad);
  }, [printUrl, id]);

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
            <div className="row">
              <button onClick={handlePrint}>Print Invoice</button>
              <a href={OrdersApi.pdfUrl(order.id)} target="_blank" rel="noreferrer">
                <button className="secondary">Download Invoice PDF</button>
              </a>
            </div>
          )}
        </div>
      </div>

      {printUrl && (
        <iframe
          ref={printFrameRef}
          src={printUrl}
          title="print-invoice"
          // Chrome's PDF viewer won't render (and so can't print) inside a
          // display:none iframe — position it off-screen instead so it
          // still lays out normally.
          style={{ position: "fixed", top: 0, left: "-9999px", width: "600px", height: "800px", border: 0 }}
        />
      )}

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
