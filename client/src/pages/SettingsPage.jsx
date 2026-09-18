import { useEffect, useState } from "react";
import { SettingsApi } from "../api/client.js";

export default function SettingsPage() {
  const [form, setForm] = useState(null);
  const [logoFile, setLogoFile] = useState(null);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    SettingsApi.get().then(setForm).catch((e) => setError(e.message));
  }, []);

  if (!form) return <p className="muted">Loading...</p>;

  const set = (key) => (e) => setForm({ ...form, [key]: e.target.value });

  const save = async (e) => {
    e.preventDefault();
    setError("");
    setMessage("");
    try {
      const updated = await SettingsApi.update({
        company_name: form.company_name,
        company_address: form.company_address,
        company_gstin: form.company_gstin,
        company_email: form.company_email,
        company_phone: form.company_phone,
        invoice_prefix: form.invoice_prefix,
        shipped_from_address: form.shipped_from_address,
        jurisdiction: form.jurisdiction,
        default_order_no: form.default_order_no,
        default_portal: form.default_portal,
        default_payment_mode_code: form.default_payment_mode_code,
        default_payment_mode_label: form.default_payment_mode_label,
        default_dispatch_through: form.default_dispatch_through,
        default_awb_no: form.default_awb_no,
        fulfillment_platform_name: form.fulfillment_platform_name
      });
      setForm(updated);
      setMessage("Settings saved.");
    } catch (err) {
      setError(err.response?.data?.error || err.message);
    }
  };

  const uploadLogo = async () => {
    if (!logoFile) return;
    setError("");
    try {
      const updated = await SettingsApi.uploadLogo(logoFile);
      setForm(updated);
      setMessage("Logo uploaded.");
    } catch (err) {
      setError(err.response?.data?.error || err.message);
    }
  };

  return (
    <div>
      <h2>Company Settings</h2>
      <p className="muted">
        These details appear on every generated invoice. Update them anytime — placeholders are fine until you have final
        details.
      </p>
      {error && <div className="alert error">{error}</div>}
      {message && <div className="alert success">{message}</div>}

      <div className="card">
        <h3>Company (Bill From)</h3>
        <form onSubmit={save}>
          <div className="form-grid">
            <div>
              <label>Company Name</label>
              <input value={form.company_name} onChange={set("company_name")} />
            </div>
            <div>
              <label>GSTIN</label>
              <input value={form.company_gstin} onChange={set("company_gstin")} />
              <p className="muted" style={{ marginTop: 4 }}>
                First 2 digits identify your state — used to decide CGST+SGST vs IGST on each invoice.
              </p>
            </div>
            <div>
              <label>Email</label>
              <input value={form.company_email} onChange={set("company_email")} />
            </div>
            <div>
              <label>Phone</label>
              <input value={form.company_phone} onChange={set("company_phone")} />
            </div>
            <div>
              <label>Invoice Number Prefix</label>
              <input value={form.invoice_prefix} onChange={set("invoice_prefix")} />
            </div>
          </div>
          <div className="form-grid full" style={{ marginTop: 12 }}>
            <div>
              <label>Registered Address</label>
              <textarea rows={2} value={form.company_address} onChange={set("company_address")} />
            </div>
            <div>
              <label>Shipped From Address (warehouse)</label>
              <textarea rows={2} value={form.shipped_from_address} onChange={set("shipped_from_address")} />
            </div>
          </div>

          <h3 style={{ marginTop: 20 }}>Order Defaults</h3>
          <p className="muted">
            Used whenever a new order doesn't specify its own value — keeps invoices looking complete without re-typing
            the same values every time.
          </p>
          <div className="form-grid">
            <div>
              <label>Default Order No (placeholder for testing)</label>
              <input value={form.default_order_no} onChange={set("default_order_no")} />
            </div>
            <div>
              <label>Default Portal</label>
              <input value={form.default_portal} onChange={set("default_portal")} />
            </div>
            <div>
              <label>Default Dispatch Through (courier)</label>
              <input value={form.default_dispatch_through} onChange={set("default_dispatch_through")} />
            </div>
            <div>
              <label>Default Payment Mode Code</label>
              <input value={form.default_payment_mode_code} onChange={set("default_payment_mode_code")} />
            </div>
            <div>
              <label>Default Payment Mode Label</label>
              <input value={form.default_payment_mode_label} onChange={set("default_payment_mode_label")} />
            </div>
            <div>
              <label>Default AWB No (leave blank unless you want a placeholder)</label>
              <input value={form.default_awb_no} onChange={set("default_awb_no")} />
            </div>
            <div>
              <label>Jurisdiction (for the declaration line)</label>
              <input value={form.jurisdiction} onChange={set("jurisdiction")} />
            </div>
            <div>
              <label>Fulfillment Platform (footer "Powered By", blank = hide)</label>
              <input value={form.fulfillment_platform_name} onChange={set("fulfillment_platform_name")} />
            </div>
          </div>

          <div style={{ marginTop: 14 }}>
            <button type="submit">Save Settings</button>
          </div>
        </form>
      </div>

      <div className="card">
        <h3>Logo</h3>
        {form.company_logo_path && <p className="muted">Current logo: {form.company_logo_path}</p>}
        <input type="file" accept="image/png,image/jpeg" onChange={(e) => setLogoFile(e.target.files[0])} />
        <div style={{ marginTop: 10 }}>
          <button type="button" onClick={uploadLogo}>
            Upload Logo
          </button>
        </div>
      </div>
    </div>
  );
}
