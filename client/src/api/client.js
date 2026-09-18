import axios from "axios";

const api = axios.create({ baseURL: "/api" });

export const ProductsApi = {
  list: (params = {}) => api.get("/products", { params }).then((r) => r.data),
  create: (data) => api.post("/products", data).then((r) => r.data),
  update: (id, data) => api.put(`/products/${id}`, data).then((r) => r.data),
  remove: (id) => api.delete(`/products/${id}`),
  importGoogleSheet: (data) => api.post("/products/import-google-sheet", data).then((r) => r.data)
};

export const CombosApi = {
  list: (params = {}) => api.get("/combos", { params }).then((r) => r.data),
  get: (id) => api.get(`/combos/${id}`).then((r) => r.data),
  create: (data) => api.post("/combos", data).then((r) => r.data),
  remove: (id) => api.delete(`/combos/${id}`),
  importGoogleSheet: (data) => api.post("/combos/import-google-sheet", data).then((r) => r.data),
  unresolvedFromSheet: (data) => api.post("/combos/unresolved-from-sheet", data).then((r) => r.data),
  autoGuessFromSheet: (data) => api.post("/combos/auto-guess-from-sheet", data).then((r) => r.data),
  resolve: (data) => api.post("/combos/resolve", data).then((r) => r.data)
};

export const OrdersApi = {
  list: () => api.get("/orders").then((r) => r.data),
  get: (id) => api.get(`/orders/${id}`).then((r) => r.data),
  create: (data) => api.post("/orders", data).then((r) => r.data),
  pdfUrl: (id) => `/api/orders/${id}/pdf`
};

export const InvoicesApi = {
  list: (orderId) => api.get("/invoices", { params: orderId ? { order_id: orderId } : {} }).then((r) => r.data),
  pdfUrl: (id) => `/api/invoices/${id}/pdf`
};

export const SettingsApi = {
  get: () => api.get("/settings").then((r) => r.data),
  gstStates: () => api.get("/settings/gst-states").then((r) => r.data),
  update: (data) => api.put("/settings", data).then((r) => r.data),
  uploadLogo: (file) => {
    const form = new FormData();
    form.append("logo", file);
    return api.post("/settings/logo", form, { headers: { "Content-Type": "multipart/form-data" } }).then((r) => r.data);
  }
};

export default api;
