import { useEffect, useState } from "react";
import { CombosApi, ProductsApi } from "../api/client.js";

const isUnverifiedDescription = (d) => typeof d === "string" && d.startsWith("UNVERIFIED");
const isLowConfidence = (d) => typeof d === "string" && d.includes("LOW CONFIDENCE");
const COMBO_PAGE_SIZE = 20;

/** Text input + dropdown of matching products, backed by a server-side search — avoids rendering a 4,800-option <select>. */
function ProductAutocomplete({ selected, onSelect }) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState([]);
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!query.trim()) {
      setResults([]);
      return;
    }
    setLoading(true);
    const handle = setTimeout(() => {
      ProductsApi.list({ search: query, pageSize: 15 })
        .then((res) => setResults(res.items))
        .catch(() => setResults([]))
        .finally(() => setLoading(false));
    }, 250);
    return () => clearTimeout(handle);
  }, [query]);

  return (
    <div className="autocomplete">
      <input
        value={selected ? `${selected.name} — ₹${selected.price.toFixed(2)}` : query}
        onChange={(e) => {
          if (selected) onSelect(null);
          setQuery(e.target.value);
          setOpen(true);
        }}
        onFocus={() => setOpen(true)}
        onBlur={() => setTimeout(() => setOpen(false), 150)}
        placeholder="Search product by name or SKU..."
      />
      {open && query.trim() && (
        <div className="autocomplete-menu">
          {loading && <div className="autocomplete-empty">Searching...</div>}
          {!loading && results.length === 0 && <div className="autocomplete-empty">No matches</div>}
          {!loading &&
            results.map((p) => (
              <div
                key={p.id}
                className="autocomplete-item"
                onClick={() => {
                  onSelect(p);
                  setQuery("");
                  setOpen(false);
                }}
              >
                {p.name} ({p.sku || "-"}) — ₹{p.price.toFixed(2)}
              </div>
            ))}
        </div>
      )}
    </div>
  );
}

export default function CombosPage() {
  const [combos, setCombos] = useState([]);
  const [comboTotal, setComboTotal] = useState(0);
  const [comboPage, setComboPage] = useState(1);
  const [comboSearchInput, setComboSearchInput] = useState("");
  const [comboSearch, setComboSearch] = useState("");
  const [showOnlyUnverified, setShowOnlyUnverified] = useState(false);

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [discountType, setDiscountType] = useState("none");
  const [discountValue, setDiscountValue] = useState("");
  const [items, setItems] = useState([{ product: null, quantity: 1 }, { product: null, quantity: 1 }]);
  const [error, setError] = useState("");

  const [sheetUrl, setSheetUrl] = useState(() => {
    try {
      return localStorage.getItem("combo_sheet_url") || "";
    } catch (_) {
      return "";
    }
  });
  const [sheetRange, setSheetRange] = useState(() => {
    try {
      return localStorage.getItem("combo_sheet_range") || "";
    } catch (_) {
      return "";
    }
  });
  const [importing, setImporting] = useState(false);
  const [importResult, setImportResult] = useState(null);
  const [loadingUnresolved, setLoadingUnresolved] = useState(false);
  const [unresolved, setUnresolved] = useState(null);
  const [selections, setSelections] = useState({});
  const [resolvingCode, setResolvingCode] = useState(null);
  const [guessing, setGuessing] = useState(false);
  const [guessResult, setGuessResult] = useState(null);
  const [fixingId, setFixingId] = useState(null);

  const load = () => {
    CombosApi.list({
      search: comboSearch,
      page: comboPage,
      pageSize: COMBO_PAGE_SIZE,
      unverified: showOnlyUnverified ? "true" : undefined
    })
      .then((res) => {
        setCombos(res.items);
        setComboTotal(res.total);
      })
      .catch((e) => setError(e.message));
  };

  // Debounce the search box so we don't fire a request per keystroke.
  useEffect(() => {
    const handle = setTimeout(() => {
      setComboPage(1);
      setComboSearch(comboSearchInput);
    }, 300);
    return () => clearTimeout(handle);
  }, [comboSearchInput]);

  useEffect(() => {
    load();
  }, [comboSearch, comboPage, showOnlyUnverified]);

  useEffect(() => {
    if (error) window.scrollTo({ top: 0, behavior: "smooth" });
  }, [error]);

  useEffect(() => {
    try {
      localStorage.setItem("combo_sheet_url", sheetUrl);
      localStorage.setItem("combo_sheet_range", sheetRange);
    } catch (_) {
      // localStorage unavailable (private browsing etc.) — not critical, just skip persisting
    }
  }, [sheetUrl, sheetRange]);

  const updateItem = (idx, patch) => {
    setItems((prev) => prev.map((it, i) => (i === idx ? { ...it, ...patch } : it)));
  };

  const addItemRow = () => setItems((prev) => [...prev, { product: null, quantity: 1 }]);
  const removeItemRow = (idx) => setItems((prev) => prev.filter((_, i) => i !== idx));

  const submit = async (e) => {
    e.preventDefault();
    setError("");
    const validItems = items.filter((it) => it.product);
    if (!name || validItems.length < 2) {
      setError("Combo needs a name and at least 2 selected products");
      return;
    }
    try {
      await CombosApi.create({
        name,
        description,
        discount_type: discountType,
        discount_value: Number(discountValue) || 0,
        items: validItems.map((it) => ({ product_id: it.product.id, quantity: Number(it.quantity) || 1 }))
      });
      setName("");
      setDescription("");
      setDiscountType("none");
      setDiscountValue("");
      setItems([{ product: null, quantity: 1 }, { product: null, quantity: 1 }]);
      load();
    } catch (err) {
      setError(err.response?.data?.error || err.message);
    }
  };

  const remove = async (id) => {
    setError("");
    try {
      await CombosApi.remove(id);
      load();
    } catch (err) {
      setError(err.response?.data?.error || err.message);
    }
  };

  const runImport = async () => {
    setError("");
    setImportResult(null);
    if (!sheetUrl) {
      setError("Paste the Google Sheet URL first");
      return;
    }
    setImporting(true);
    try {
      const result = await CombosApi.importGoogleSheet({ sheet_url: sheetUrl, range: sheetRange || undefined });
      setImportResult(result);
      load();
    } catch (err) {
      setError(err.response?.data?.error || err.message);
    } finally {
      setImporting(false);
    }
  };

  const findUnresolved = async () => {
    setError("");
    if (!sheetUrl) {
      setError("Paste the Google Sheet URL first");
      return;
    }
    setLoadingUnresolved(true);
    try {
      const result = await CombosApi.unresolvedFromSheet({ sheet_url: sheetUrl, range: sheetRange || undefined });
      setUnresolved(result.unresolved);
      setSelections({});
    } catch (err) {
      setError(err.response?.data?.error || err.message);
    } finally {
      setLoadingUnresolved(false);
    }
  };

  const runAutoGuess = async () => {
    setError("");
    setGuessResult(null);
    if (!sheetUrl) {
      setError("Paste the Google Sheet URL first");
      return;
    }
    setGuessing(true);
    try {
      const result = await CombosApi.autoGuessFromSheet({ sheet_url: sheetUrl, range: sheetRange || undefined });
      setGuessResult(result);
      setUnresolved(null);
      load();
    } catch (err) {
      setError(err.response?.data?.error || err.message);
    } finally {
      setGuessing(false);
    }
  };

  const pickCandidate = (code, suffix, productId) => {
    setSelections((prev) => ({
      ...prev,
      [code]: { ...(prev[code] || {}), [suffix]: productId }
    }));
  };

  const resolveCombo = async (combo) => {
    setError("");
    const picked = selections[combo.code] || {};
    const selectionList = combo.suffixes.map((s) => ({ suffix: s.suffix, product_id: Number(picked[s.suffix]) || null }));
    if (selectionList.some((s) => !s.product_id)) {
      setError(`Pick a product for every component of ${combo.code} first`);
      return;
    }
    setResolvingCode(combo.code);
    try {
      await CombosApi.resolve({ code: combo.code, name: combo.name, mrp: combo.mrp, selections: selectionList });
      setUnresolved((prev) => prev.filter((c) => c.code !== combo.code));
      load();
    } catch (err) {
      setError(err.response?.data?.error || err.message);
    } finally {
      setResolvingCode(null);
    }
  };

  const fixCombo = async (combo) => {
    setError("");
    if (!sheetUrl) {
      setError("Paste the Google Sheet URL above first, then click Fix — I need it to re-fetch this combo's candidate products.");
      return;
    }
    setFixingId(combo.id);
    try {
      await CombosApi.remove(combo.id);
      const result = await CombosApi.unresolvedFromSheet({ sheet_url: sheetUrl, range: sheetRange || undefined });
      const target = result.unresolved.find((c) => c.code === combo.code);
      setUnresolved(target ? [target] : result.unresolved);
      load();
      window.scrollTo({ top: 0, behavior: "smooth" });
    } catch (err) {
      setError(err.response?.data?.error || err.message);
    } finally {
      setFixingId(null);
    }
  };

  const totalPages = Math.max(1, Math.ceil(comboTotal / COMBO_PAGE_SIZE));

  return (
    <div>
      <h2>Combo Deals</h2>
      {error && <div className="alert error">{error}</div>}

      <div className="card">
        <h3>Import Combos from Google Sheet</h3>
        <p className="muted">
          Reads rows with Category Code "CMB" and matches component products by the numeric suffixes in the Product
          Code (e.g. CMB_0047_0196). Combos that match cleanly are created automatically; the rest show up below for you
          to pick the right product yourself.
        </p>
        <div className="form-grid">
          <div>
            <label>Google Sheet URL</label>
            <input value={sheetUrl} onChange={(e) => setSheetUrl(e.target.value)} placeholder="https://docs.google.com/spreadsheets/d/..." />
          </div>
          <div>
            <label>Sheet/Range (optional)</label>
            <input value={sheetRange} onChange={(e) => setSheetRange(e.target.value)} placeholder="Sheet1" />
          </div>
        </div>
        <div className="row" style={{ marginTop: 14 }}>
          <button type="button" onClick={runImport} disabled={importing}>
            {importing ? "Importing..." : "Auto-Import Combos"}
          </button>
          <button type="button" className="secondary" onClick={findUnresolved} disabled={loadingUnresolved}>
            {loadingUnresolved ? "Checking..." : "Find Combos Needing Review"}
          </button>
          <button type="button" className="danger" onClick={runAutoGuess} disabled={guessing}>
            {guessing ? "Guessing..." : "Auto-Guess Remaining (flag for review)"}
          </button>
        </div>
        {importResult && (
          <div className="alert success" style={{ marginTop: 12 }}>
            Created {importResult.createdCount} combos automatically. {importResult.skippedCount} need review — click
            "Find Combos Needing Review" to resolve them.
          </div>
        )}
        {guessResult && (
          <div className="alert error" style={{ marginTop: 12 }}>
            Auto-guessed and created {guessResult.createdCount} combos, marked "UNVERIFIED" in their description —
            check the Existing Combos list below and fix any that picked the wrong product.
          </div>
        )}
      </div>

      {unresolved && (
        <div className="card">
          <h3>Combos Needing Review ({unresolved.length})</h3>
          {unresolved.length === 0 && <p className="muted">Nothing left to review.</p>}
          {unresolved.map((combo) => (
            <div key={combo.code} className="card" style={{ background: "#fafbfc" }}>
              <div className="row between">
                <div>
                  <strong>{combo.name}</strong> <span className="badge">{combo.code}</span>
                  <div className="muted">Combo MRP: ₹{combo.mrp.toFixed(2)}</div>
                </div>
                <button onClick={() => resolveCombo(combo)} disabled={resolvingCode === combo.code}>
                  {resolvingCode === combo.code ? "Creating..." : "Create Combo"}
                </button>
              </div>
              <div className="form-grid" style={{ marginTop: 10 }}>
                {combo.suffixes.map((s) => (
                  <div key={s.suffix}>
                    <label>Component _{s.suffix}{s.candidates.length === 0 && " — no matching product found"}</label>
                    <select
                      value={(selections[combo.code] || {})[s.suffix] || ""}
                      onChange={(e) => pickCandidate(combo.code, s.suffix, e.target.value)}
                    >
                      <option value="">Select product</option>
                      {s.candidates.map((c) => (
                        <option key={c.id} value={c.id}>
                          {c.name} ({c.sku}) — ₹{c.price.toFixed(2)}
                        </option>
                      ))}
                    </select>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}

      <div className="card">
        <h3>Create Combo Manually</h3>
        <form onSubmit={submit}>
          <div className="form-grid">
            <div>
              <label>Combo Name</label>
              <input value={name} onChange={(e) => setName(e.target.value)} />
            </div>
            <div>
              <label>Description (optional)</label>
              <input value={description} onChange={(e) => setDescription(e.target.value)} />
            </div>
            <div>
              <label>Discount Type</label>
              <select value={discountType} onChange={(e) => setDiscountType(e.target.value)}>
                <option value="none">None</option>
                <option value="percent">Percent off combo total</option>
                <option value="fixed">Fixed amount off combo total</option>
              </select>
            </div>
            <div>
              <label>Discount Value {discountType === "percent" ? "(%)" : "(₹)"}</label>
              <input
                type="number"
                step="0.01"
                value={discountValue}
                onChange={(e) => setDiscountValue(e.target.value)}
                disabled={discountType === "none"}
              />
            </div>
          </div>

          <h4 style={{ marginTop: 18, marginBottom: 8 }}>Products in this combo</h4>
          {items.map((item, idx) => (
            <div className="combo-item-row" key={idx}>
              <div>
                <label>Product</label>
                <ProductAutocomplete selected={item.product} onSelect={(product) => updateItem(idx, { product })} />
              </div>
              <div>
                <label>Qty</label>
                <input
                  type="number"
                  min="1"
                  value={item.quantity}
                  onChange={(e) => updateItem(idx, { quantity: e.target.value })}
                />
              </div>
              <button type="button" className="secondary" onClick={() => removeItemRow(idx)} disabled={items.length <= 2}>
                Remove
              </button>
            </div>
          ))}
          <button type="button" className="secondary" onClick={addItemRow}>
            + Add Product
          </button>

          <div style={{ marginTop: 16 }}>
            <button type="submit">Create Combo</button>
          </div>
        </form>
      </div>

      <div className="card">
        <div className="row between">
          <h3 style={{ margin: 0 }}>Existing Combos ({comboTotal.toLocaleString()})</h3>
          <div className="row">
            <input
              style={{ maxWidth: 240 }}
              value={comboSearchInput}
              onChange={(e) => setComboSearchInput(e.target.value)}
              placeholder="Search by name or code..."
            />
            <label style={{ display: "flex", alignItems: "center", gap: 6, whiteSpace: "nowrap" }}>
              <input
                type="checkbox"
                checked={showOnlyUnverified}
                onChange={(e) => {
                  setShowOnlyUnverified(e.target.checked);
                  setComboPage(1);
                }}
                style={{ width: "auto" }}
              />
              Show only unverified
            </label>
          </div>
        </div>
        {combos.map((combo) => {
          const isUnverified = isUnverifiedDescription(combo.description);
          const lowConfidence = isLowConfidence(combo.description);
          return (
            <div key={combo.id} className="card" style={{ background: lowConfidence ? "#fde2e2" : isUnverified ? "#fdecec" : "#fafbfc" }}>
              <div className="row between">
                <div>
                  <strong>{combo.name}</strong>{" "}
                  {combo.code && <span className="badge">{combo.code}</span>}{" "}
                  {combo.discount_type !== "none" && (
                    <span className="badge">
                      {combo.discount_type === "percent" ? `${combo.discount_value}% off` : `₹${combo.discount_value} off`}
                    </span>
                  )}
                  {isUnverified ? (
                    <div style={{ color: "#b3261e", fontWeight: 600, fontSize: 13, marginTop: 4 }}>
                      {lowConfidence ? "⚠⚠ LOW CONFIDENCE — no name match, guessed on price alone" : "⚠ UNVERIFIED — please check components"}
                    </div>
                  ) : (
                    <div className="muted">{combo.description}</div>
                  )}
                </div>
                <div className="row">
                  {isUnverified && (
                    <button onClick={() => fixCombo(combo)} disabled={fixingId === combo.id}>
                      {fixingId === combo.id ? "Loading..." : "Fix Components"}
                    </button>
                  )}
                  <button className="danger" onClick={() => remove(combo.id)}>
                    Delete
                  </button>
                </div>
              </div>
              <table style={{ marginTop: 10 }}>
                <thead>
                  <tr>
                    <th>Product</th>
                    <th>Qty</th>
                    <th>Unit Price</th>
                  </tr>
                </thead>
                <tbody>
                  {combo.items.map((it) => (
                    <tr key={it.id}>
                      <td>{it.name}</td>
                      <td>{it.quantity}</td>
                      <td>₹{it.price.toFixed(2)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          );
        })}
        {combos.length === 0 && (
          <p className="muted">{comboTotal === 0 && !comboSearch && !showOnlyUnverified ? "No combos yet." : "Nothing matches this filter."}</p>
        )}
        <div className="pagination">
          <button className="secondary" onClick={() => setComboPage((p) => Math.max(1, p - 1))} disabled={comboPage <= 1}>
            Prev
          </button>
          <span className="muted">
            Page {comboPage} of {totalPages}
          </span>
          <button className="secondary" onClick={() => setComboPage((p) => Math.min(totalPages, p + 1))} disabled={comboPage >= totalPages}>
            Next
          </button>
        </div>
      </div>
    </div>
  );
}
