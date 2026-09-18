const express = require("express");
const multer = require("multer");
const path = require("path");
const fs = require("fs");
const { getAllSettings, updateSettings } = require("../services/settingsService");
const { GST_STATE_CODES } = require("../services/gstStates");

const router = express.Router();

router.get("/gst-states", (_req, res) => {
  res.json(GST_STATE_CODES);
});

const LOGOS_DIR = path.join(__dirname, "..", "..", "storage", "logos");
if (!fs.existsSync(LOGOS_DIR)) fs.mkdirSync(LOGOS_DIR, { recursive: true });

const upload = multer({
  storage: multer.diskStorage({
    destination: (_req, _file, cb) => cb(null, LOGOS_DIR),
    filename: (_req, file, cb) => cb(null, `logo-${Date.now()}${path.extname(file.originalname)}`)
  }),
  limits: { fileSize: 2 * 1024 * 1024 },
  fileFilter: (_req, file, cb) => {
    if (!/^image\/(png|jpeg|jpg)$/.test(file.mimetype)) {
      return cb(new Error("Only PNG/JPEG logos are allowed"));
    }
    cb(null, true);
  }
});

router.get("/", (_req, res) => {
  res.json(getAllSettings());
});

router.put("/", (req, res) => {
  const allowedKeys = [
    "company_name",
    "company_address",
    "company_gstin",
    "company_email",
    "company_phone",
    "invoice_prefix",
    "shipped_from_address",
    "jurisdiction",
    "default_order_no",
    "default_portal",
    "default_payment_mode_code",
    "default_payment_mode_label",
    "default_dispatch_through",
    "default_awb_no",
    "fulfillment_platform_name"
  ];
  const partial = {};
  for (const key of allowedKeys) {
    if (req.body[key] !== undefined) partial[key] = req.body[key];
  }
  const settings = updateSettings(partial);
  res.json(settings);
});

router.post("/logo", upload.single("logo"), (req, res) => {
  if (!req.file) return res.status(400).json({ error: "No file uploaded" });
  const settings = updateSettings({ company_logo_path: req.file.filename });
  res.json(settings);
});

module.exports = router;
