import axios from "axios";

const api = axios.create({ baseURL: "/api" });

export const ProductsApi = {
  list: (params = {}) => api.get("/products", { params }).then((r) => r.data),
  create: (data) => api.post("/products", data).then((r) => r.data),
  remove: (id) => api.delete(`/products/${id}`),
  importGoogleSheet: (data) => api.post("/products/import-google-sheet", data).then((r) => r.data)
};

export const CombosApi = {
  list: (params = {}) => api.get("/combos", { params }).then((r) => r.data),
  create: (data) => api.post("/combos", data).then((r) => r.data),
  remove: (id) => api.delete(`/combos/${id}`),
  importGoogleSheet: (data) => api.post("/combos/import-google-sheet", data).then((r) => r.data),
  unresolvedFromSheet: (data) => api.post("/combos/unresolved-from-sheet", data).then((r) => r.data),
  autoGuessFromSheet: (data) => api.post("/combos/auto-guess-from-sheet", data).then((r) => r.data),
  resolve: (data) => api.post("/combos/resolve", data).then((r) => r.data)
};

export const OrdersApi = {
  list: (params = {}) => api.get("/orders", { params }).then((r) => r.data),
  get: (id) => api.get(`/orders/${id}`).then((r) => r.data),
  create: (data) => api.post("/orders", data).then((r) => r.data),
  identifyCMD: (limit) => api.post("/orders/identify-cmd", { limit }).then((r) => r.data),
  importGoogleSheet: (data) => api.post("/orders/import-google-sheet", data).then((r) => r.data),
  deleteRecent: (count) => api.post("/orders/delete-recent", { count, confirm: true }).then((r) => r.data),
  pdfUrl: (id) => `/api/orders/${id}/pdf`
};

export const StatsApi = {
  get: () => api.get("/stats").then((r) => r.data)
};

export default api;
