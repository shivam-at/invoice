import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { CombosApi, OrdersApi, SettingsApi } from "../api/client.js";

export default function NewOrderPage() {
  const [combos, setCombos] = useState([]);
  const [comboFilter, setComboFilter] = useState("");
  const [comboId, setComboId] = useState("");
  const [comboQuantity, setComboQuantity] = useState(1);
  const [customerName, setCustomerName] = useState("");
  const [customerEmail, setCustomerEmail] = useState("");
  const [customerPhone, setCustomerPhone] = useState("");
  const [customerAddress, setCustomerAddress] = useState("");
  const [customerStateCode, setCustomerStateCode] = useState("");
  const [gstStates, setGstStates] = useState([]);

  const [showShippingDetails, setShowShippingDetails] = useState(false);
  const [shopifyOrderNo, setShopifyOrderNo] = useState("");
  const [dispatchThrough, setDispatchThrough] = useState("");
  const [awbNo, setAwbNo] = useState("");
  const [shipSameAsBilling, setShipSameAsBilling] = useState(true);
  const [shippingName, setShippingName] = useState("");
  const [shippingPhone, setShippingPhone] = useState("");
  const [shippingAddress, setShippingAddress] = useState("");

  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const navigate = useNavigate();

  useEffect(() => {
    // Combos are cheap enough to fetch in one page-size for this dropdown, unlike the products catalog.
    CombosApi.list({ pageSize: 2000 })
      .then((res) => setCombos(res.items))
      .catch((e) => setError(e.message));
    SettingsApi.gstStates()
      .then(setGstStates)
      .catch(() => setGstStates([]));
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
    if (!comboId || !customerName) {
      setError("Please select a combo and enter customer name");
      return;
    }
    setSubmitting(true);
    try {
      const order = await OrdersApi.create({
        combo_id: Number(comboId),
        combo_quantity: Number(comboQuantity) || 1,
        customer_name: customerName,
        customer_email: customerEmail,
        customer_phone: customerPhone,
        customer_address: customerAddress,
        customer_state_code: customerStateCode,
        shopify_order_no: shopifyOrderNo,
        dispatch_through: dispatchThrough,
        awb_no: awbNo,
        shipping_name: shipSameAsBilling ? "" : shippingName,
        shipping_phone: shipSameAsBilling ? "" : shippingPhone,
        shipping_address: shipSameAsBilling ? "" : shippingAddress
      });
      navigate(`/orders/${order.id}`);
    } catch (err) {
      setError(err.response?.data?.error || err.message);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div>
      <h2>New Order</h2>
      <p className="muted">Pick a combo deal and customer details — one invoice per product will be generated automatically.</p>
      {error && <div className="alert error">{error}</div>}

      <div className="card">
        <form onSubmit={submit}>
          <div className="form-grid">
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
              <label>Number of Combo Sets</label>
              <input type="number" min="1" value={comboQuantity} onChange={(e) => setComboQuantity(e.target.value)} />
            </div>
            <div>
              <label>Customer Name</label>
              <input value={customerName} onChange={(e) => setCustomerName(e.target.value)} />
            </div>
            <div>
              <label>Customer Email</label>
              <input value={customerEmail} onChange={(e) => setCustomerEmail(e.target.value)} />
            </div>
            <div>
              <label>Customer Phone</label>
              <input value={customerPhone} onChange={(e) => setCustomerPhone(e.target.value)} />
            </div>
            <div>
              <label>Customer Address (Bill To)</label>
              <input value={customerAddress} onChange={(e) => setCustomerAddress(e.target.value)} />
            </div>
            <div>
              <label>Customer State (for GST — CGST+SGST vs IGST)</label>
              <select value={customerStateCode} onChange={(e) => setCustomerStateCode(e.target.value)}>
                <option value="">Same state as company (default)</option>
                {gstStates.map((s) => (
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
                  {selectedCombo.items.map((it) => (
                    <tr key={it.id}>
                      <td>{it.name}</td>
                      <td>{it.quantity}</td>
                      <td>₹{it.price.toFixed(2)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {selectedCombo.discount_type !== "none" && (
                <p className="muted">
                  Combo discount: {selectedCombo.discount_type === "percent" ? `${selectedCombo.discount_value}%` : `₹${selectedCombo.discount_value}`}{" "}
                  will be split across each product's invoice, proportional to its own price.
                </p>
              )}
            </div>
          )}

          <div style={{ marginTop: 16 }}>
            <button type="button" className="secondary" onClick={() => setShowShippingDetails((v) => !v)}>
              {showShippingDetails ? "Hide" : "Add"} Order No / Dispatch / Ship To (optional)
            </button>
          </div>

          {showShippingDetails && (
            <div className="card" style={{ background: "#fafbfc", marginTop: 12 }}>
              <p className="muted">Leave any of these blank to fall back to the defaults set on the Settings page.</p>
              <div className="form-grid">
                <div>
                  <label>Shopify Order No (e.g. #92043533346082)</label>
                  <input value={shopifyOrderNo} onChange={(e) => setShopifyOrderNo(e.target.value)} />
                </div>
                <div>
                  <label>Dispatch Through (courier)</label>
                  <input value={dispatchThrough} onChange={(e) => setDispatchThrough(e.target.value)} />
                </div>
                <div>
                  <label>AWB No</label>
                  <input value={awbNo} onChange={(e) => setAwbNo(e.target.value)} />
                </div>
              </div>

              <label style={{ marginTop: 12 }}>
                <input
                  type="checkbox"
                  checked={shipSameAsBilling}
                  onChange={(e) => setShipSameAsBilling(e.target.checked)}
                  style={{ width: "auto", marginRight: 6 }}
                />
                Ship To same as Bill To
              </label>

              {!shipSameAsBilling && (
                <div className="form-grid" style={{ marginTop: 10 }}>
                  <div>
                    <label>Ship To Name</label>
                    <input value={shippingName} onChange={(e) => setShippingName(e.target.value)} />
                  </div>
                  <div>
                    <label>Ship To Phone</label>
                    <input value={shippingPhone} onChange={(e) => setShippingPhone(e.target.value)} />
                  </div>
                  <div>
                    <label>Ship To Address</label>
                    <input value={shippingAddress} onChange={(e) => setShippingAddress(e.target.value)} />
                  </div>
                </div>
              )}
            </div>
          )}

          <div style={{ marginTop: 16 }}>
            <button type="submit" disabled={submitting}>
              {submitting ? "Generating Invoices..." : "Generate Invoices"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
