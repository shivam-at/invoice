import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { CombosApi, OrdersApi } from "../api/client.js";
import { GST_STATES } from "../gstStates.js";

function defaultOrderNo() {
  return `ORD-${Date.now()}`;
}

export default function NewOrderPage() {
  const [combos, setCombos] = useState([]);
  const [comboFilter, setComboFilter] = useState("");
  const [comboId, setComboId] = useState("");
  const [comboQuantity, setComboQuantity] = useState(1);
  const [orderNo, setOrderNo] = useState(defaultOrderNo());
  const [customerName, setCustomerName] = useState("");
  const [customerAddress, setCustomerAddress] = useState("");
  const [customerStateCode, setCustomerStateCode] = useState("");

  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const navigate = useNavigate();

  useEffect(() => {
    // Combos are cheap enough to fetch in one page-size for this dropdown, unlike the products catalog.
    CombosApi.list({ pageSize: 2000 })
      .then((res) => setCombos(res.items || []))
      .catch((e) => setError(e.message));
  }, []);

  const selectedCombo = combos.find((c) => String(c.id) === String(comboId));
  const filteredCombos = comboFilter.trim()
    ? combos.filter((c) => {
        const q = comboFilter.trim().toLowerCase();
        return c.name.toLowerCase().includes(q) || (c.code || "").toLowerCase().includes(q);
      })
    : combos;

  const submit = async (e) => {
    e.preventDefault();
    setError("");
    if (!comboId || !customerName || !orderNo) {
      setError("Order No, combo and customer name are required");
      return;
    }
    if (!customerStateCode) {
      setError("Customer State is required — it decides CGST+SGST vs IGST and can't be guessed.");
      return;
    }
    setSubmitting(true);
    try {
      const created = await OrdersApi.create({
        order_no: orderNo,
        combo_id: Number(comboId),
        combo_quantity: Number(comboQuantity) || 1,
        customer_name: customerName,
        customer_address: customerAddress,
        customer_state_code: customerStateCode
      });
      // The order lands as PENDING — kick it into the invoice/print pipeline
      // right away instead of waiting for a periodic identify-cmd sweep.
      await OrdersApi.identifyCMD(10);
      navigate(`/orders/${created.id}`);
    } catch (err) {
      setError(err.response?.data?.error || err.message);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div>
      <h2>New Order</h2>
      <p className="muted">
        Pick a combo deal and customer details. The order is created as PENDING, then handed to the invoice-generation
        and print worker pools automatically — this page will jump to the order's status once it's queued.
      </p>
      {error && <div className="alert error">{error}</div>}

      <div className="card">
        <form onSubmit={submit}>
          <div className="form-grid">
            <div>
              <label>Order No</label>
              <input value={orderNo} onChange={(e) => setOrderNo(e.target.value)} />
            </div>
            <div>
              <label>Number of Combo Sets</label>
              <input type="number" min="1" value={comboQuantity} onChange={(e) => setComboQuantity(e.target.value)} />
            </div>
            <div>
              <label>Combo Deal ({combos.length} available)</label>
              <input
                value={comboFilter}
                onChange={(e) => setComboFilter(e.target.value)}
                placeholder="Type to filter by name or code..."
                style={{ marginBottom: 6 }}
              />
              <select value={comboId} onChange={(e) => setComboId(e.target.value)}>
                <option value="">Select a combo</option>
                {filteredCombos.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                    {c.code ? ` (${c.code})` : ""}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label>Customer Name</label>
              <input value={customerName} onChange={(e) => setCustomerName(e.target.value)} />
            </div>
            <div>
              <label>Customer Address (Bill To)</label>
              <input value={customerAddress} onChange={(e) => setCustomerAddress(e.target.value)} />
            </div>
            <div>
              <label>Customer State (for GST — CGST+SGST vs IGST) *</label>
              <select value={customerStateCode} onChange={(e) => setCustomerStateCode(e.target.value)} required>
                <option value="">-- Select customer state --</option>
                {GST_STATES.map((s) => (
                  <option key={s.code} value={s.code}>
                    {s.name}
                  </option>
                ))}
              </select>
            </div>
          </div>

          {selectedCombo && (
            <div className="card" style={{ background: "#fafbfc", marginTop: 16 }}>
              <strong>Preview: {selectedCombo.name}</strong>
              <table style={{ marginTop: 8 }}>
                <thead>
                  <tr>
                    <th>Product</th>
                    <th>Qty per set</th>
                    <th>Unit Price</th>
                  </tr>
                </thead>
                <tbody>
                  {(selectedCombo.items || []).map((it) => (
                    <tr key={it.product_id}>
                      <td>{it.product.name}</td>
                      <td>{it.quantity}</td>
                      <td>₹{it.product.price.toFixed(2)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {selectedCombo.discount_type !== "none" && (
                <p className="muted">
                  Combo discount: {selectedCombo.discount_type === "percent" ? `${selectedCombo.discount_value}%` : `₹${selectedCombo.discount_value}`}
                </p>
              )}
            </div>
          )}

          <div style={{ marginTop: 16 }}>
            <button type="submit" disabled={submitting}>
              {submitting ? "Creating..." : "Create Order"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
