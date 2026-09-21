import { useEffect, useState } from "react";
import { ProductsApi } from "../api/client.js";

/** Text input + dropdown of matching products, backed by a server-side search — avoids rendering a 4,800-option <select>. */
export default function ProductAutocomplete({ selected, onSelect }) {
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
