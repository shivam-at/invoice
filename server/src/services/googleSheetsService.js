const path = require("path");
const { google } = require("googleapis");

const KEY_FILE = path.join(__dirname, "..", "..", "config", "google-service-account.json");

async function getSheetsClient() {
  const auth = new google.auth.GoogleAuth({
    keyFile: KEY_FILE,
    scopes: ["https://www.googleapis.com/auth/spreadsheets.readonly"]
  });
  const client = await auth.getClient();
  return google.sheets({ version: "v4", auth: client });
}

function extractSpreadsheetId(idOrUrl) {
  const match = idOrUrl.match(/\/d\/([a-zA-Z0-9-_]+)/);
  return match ? match[1] : idOrUrl;
}

async function fetchSheetRows(spreadsheetIdOrUrl, range) {
  const sheets = await getSheetsClient();
  const spreadsheetId = extractSpreadsheetId(spreadsheetIdOrUrl);
  const res = await sheets.spreadsheets.values.get({
    spreadsheetId,
    range: range || "Sheet1"
  });
  return res.data.values || [];
}

module.exports = { fetchSheetRows, extractSpreadsheetId };
