import { useEffect, useState } from "react";
import { ProductsApi } from "../api/client.js";

const emptyForm = { name: "", sku: "", hsn_code: "", price: "", tax_rate: "" };
const PAGE_SIZE = 50;

export default function ProductsPage() {
  const [products, setProducts] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [loading, setLoading] = useState(false);

  const [form, setForm] = useState(emptyForm);
  const [error, setError] = useState("");

  const [sheetUrl, setSheetUrl] = useState("");
  const [sheetRange, setSheetRange] = useState("Sheet1");
  const [importing, setImporting] = useState(false);
  const [importResult, setImportResult] = useState(null);

  const load = () => {
    setLoading(true);
    ProductsApi.list({ search, page, pageSize: PAGE_SIZE })
      .then((res) => {
        setProducts(res.items);
        setTotal(res.total);
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
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
  }, [search, page]);

  const submit = async (e) => {
    e.preventDefault();
    setError("");
    if (!form.name || form.price === "") {
      setError("Name and price are required");
      return;
    }
    try {
      await ProductsApi.create({
        name: form.name,
        sku: form.sku || null,
        hsn_code: form.hsn_code || null,
        price: Number(form.price),
        tax_rate: Number(form.tax_rate) || 0
      });
      setForm(emptyForm);
      load();
    } catch (err) {
      setError(err.response?.data?.error || err.message);
    }
  };

  const remove = async (id) => {
    setError("");
    try {
      await ProductsApi.remove(id);
      load();
    } catch (err) {
      setError(err.response?.data?.error || err.message);
    }
  };

  const importSheet = async (e) => {
    e.preventDefault();
    setError("");
    setImportResult(null);
    if (!sheetUrl) {
      setError("Paste the Google Sheet URL first");
      return;
    }
    setImporting(true);
    try {
      const result = await ProductsApi.importGoogleSheet({ sheet_url: sheetUrl, range: sheetRange || "Sheet1" });
      setImportResult(result);
      load();
    } catch (err) {
      setError(err.response?.data?.error || err.message);
    } finally {
      setImporting(false);
    }
  };

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <div>
      <h2>Products</h2>
      {error && <div className="alert error">{error}</div>}

      <div className="card">
        <h3>Import from Google Sheet</h3>
        <p className="muted">
          Share the sheet (Viewer access) with the service account's client email, then paste the sheet URL here. Expected
          columns (any order, case-insensitive): Name, SKU, HSN Code, Price, Tax Rate (GST%). Rows are matched by SKU — importing
          again updates existing products instead of duplicating them.
        </p>
        <form onSubmit={importSheet}>
          <div className="form-grid">
            <div>
              <label>Google Sheet URL</label>
              <input
                value={sheetUrl}
                onChange={(e) => setSheetUrl(e.target.value)}
                placeholder="https://docs.google.com/spreadsheets/d/..."
              />
            </div>
            <div>
              <label>Sheet/Range (optional)</label>
              <input value={sheetRange} onChange={(e) => setSheetRange(e.target.value)} placeholder="Sheet1" />
            </div>
          </div>
          <div style={{ marginTop: 14 }}>
            <button type="submit" disabled={importing}>
              {importing ? "Importing..." : "Import Products"}
            </button>
          </div>
        </form>
        {importResult && (
          <div className="alert success" style={{ marginTop: 12 }}>
            Created {importResult.created}, updated {importResult.updated} of {importResult.totalRows} rows.
            {importResult.skipped.length > 0 && ` Skipped ${importResult.skipped.length} row(s).`}
          </div>
        )}
      </div>

      <div className="card">
        <h3>Add Product Manually</h3>
        <form onSubmit={submit}>
          <div className="form-grid">
            <div>
              <label>Name</label>
              <input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
            </div>
            <div>
              <label>SKU / Product Code (optional)</label>
              <input value={form.sku} onChange={(e) => setForm({ ...form, sku: e.target.value })} />
            </div>
            <div>
              <label>HSN Code (optional)</label>
              <input value={form.hsn_code} onChange={(e) => setForm({ ...form, hsn_code: e.target.value })} />
            </div>
            <div>
              <label>Price (₹, GST-inclusive)</label>
              <input type="number" step="0.01" value={form.price} onChange={(e) => setForm({ ...form, price: e.target.value })} />
            </div>
            <div>
              <label>GST Rate (%)</label>
              <input type="number" step="0.01" value={form.tax_rate} onChange={(e) => setForm({ ...form, tax_rate: e.target.value })} />
            </div>
          </div>
          <div style={{ marginTop: 14 }}>
            <button type="submit">Add Product</button>
          </div>
        </form>
      </div>

      <div className="card">
        <div className="row between">
          <h3 style={{ margin: 0 }}>All Products ({total.toLocaleString()})</h3>
          <input
            style={{ maxWidth: 280 }}
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            placeholder="Search by name or SKU..."
          />
        </div>
        <table style={{ marginTop: 14 }}>
          <thead>
            <tr>
              <th>Name</th>
              <th>SKU</th>
              <th>HSN</th>
              <th>Price</th>
              <th>GST %</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {products.map((p) => (
              <tr key={p.id}>
                <td>{p.name}</td>
                <td>{p.sku || "-"}</td>
                <td>{p.hsn_code || "-"}</td>
                <td>₹{p.price.toFixed(2)}</td>
                <td>{p.tax_rate}%</td>
                <td>
                  <button className="danger" onClick={() => remove(p.id)}>
                    Delete
                  </button>
                </td>
              </tr>
            ))}
            {!loading && products.length === 0 && (
              <tr>
                <td colSpan={6} className="muted">
                  {search ? "No products match your search." : "No products yet."}
                </td>
              </tr>
            )}
          </tbody>
        </table>
        <div className="pagination">
          <button className="secondary" onClick={() => setPage((p) => Math.max(1, p - 1))} disabled={page <= 1 || loading}>
            Prev
          </button>
          <span className="muted">
            Page {page} of {totalPages}
          </span>
          <button className="secondary" onClick={() => setPage((p) => Math.min(totalPages, p + 1))} disabled={page >= totalPages || loading}>
            Next
          </button>
        </div>
      </div>
    </div>
  );
}
