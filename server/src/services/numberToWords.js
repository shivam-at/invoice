const ONES = [
  "", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine",
  "Ten", "Eleven", "Twelve", "Thirteen", "Fourteen", "Fifteen", "Sixteen",
  "Seventeen", "Eighteen", "Nineteen"
];
const TENS = [
  "", "", "Twenty", "Thirty", "Forty", "Fifty", "Sixty", "Seventy", "Eighty", "Ninety"
];

function threeDigitsToWords(n) {
  const parts = [];
  if (n >= 100) {
    parts.push(ONES[Math.floor(n / 100)], "Hundred");
    n %= 100;
  }
  if (n >= 20) {
    parts.push(TENS[Math.floor(n / 10)]);
    n %= 10;
    if (n > 0) parts.push(ONES[n]);
  } else if (n > 0) {
    parts.push(ONES[n]);
  }
  return parts.join(" ");
}

// Indian numbering system: crore / lakh / thousand / hundred.
function integerToIndianWords(n) {
  if (n === 0) return "Zero";
  const crore = Math.floor(n / 10000000);
  n %= 10000000;
  const lakh = Math.floor(n / 100000);
  n %= 100000;
  const thousand = Math.floor(n / 1000);
  n %= 1000;
  const hundred = n;

  const segments = [];
  if (crore) segments.push(threeDigitsToWords(crore), "Crore");
  if (lakh) segments.push(threeDigitsToWords(lakh), "Lakh");
  if (thousand) segments.push(threeDigitsToWords(thousand), "Thousand");
  if (hundred) segments.push(threeDigitsToWords(hundred));
  return segments.join(" ").trim();
}

function amountInWordsINR(amount) {
  const rupees = Math.floor(Math.round(amount * 100) / 100);
  const paise = Math.round((amount - rupees) * 100);

  const rupeeWords = integerToIndianWords(rupees);
  let words = `${rupeeWords} Rupee${rupees === 1 ? "" : "s"}`;
  if (paise > 0) {
    words += ` and ${integerToIndianWords(paise)} Paise`;
  }
  return `${words} Only`;
}

module.exports = { amountInWordsINR };
