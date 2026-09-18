const express = require("express");
const cors = require("cors");

const productsRouter = require("./routes/products");
const combosRouter = require("./routes/combos");
const ordersRouter = require("./routes/orders");
const invoicesRouter = require("./routes/invoices");
const settingsRouter = require("./routes/settings");

const app = express();
const PORT = process.env.PORT || 4000;

app.use(cors());
app.use(express.json());

app.use("/api/products", productsRouter);
app.use("/api/combos", combosRouter);
app.use("/api/orders", ordersRouter);
app.use("/api/invoices", invoicesRouter);
app.use("/api/settings", settingsRouter);

app.use((err, _req, res, _next) => {
  console.error(err);
  res.status(500).json({ error: err.message || "Internal server error" });
});

app.listen(PORT, () => {
  console.log(`Invoice server running on http://localhost:${PORT}`);
});
