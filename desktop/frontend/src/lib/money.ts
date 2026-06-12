const DEFAULT_CURRENCY_SYMBOL = "\u00A5";

export function currencySymbol(currency?: string): string {
  const value = (currency || DEFAULT_CURRENCY_SYMBOL).trim();
  const lower = value.toLowerCase();

  if (/^(cny|rmb|yuan|renminbi|cnh)$/.test(lower)) return DEFAULT_CURRENCY_SYMBOL;
  if (/^(usd|dollar|dollars|us dollar|us dollars|us\$)$/.test(lower)) return "$";
  if (/^(eur|euro|euros)$/.test(lower)) return "\u20AC";
  if (/^(gbp|pound|pounds|sterling)$/.test(lower)) return "\u00A3";
  if (/^(jpy|yen)$/.test(lower)) return DEFAULT_CURRENCY_SYMBOL;
  if (value === "\uFFE5" || value === "\u00A5") return DEFAULT_CURRENCY_SYMBOL;
  // contains, not equals \u2014 keep multi-char symbols (A$, HK$) off the \u00A5 default.
  if (/\p{Sc}/u.test(value)) return value;
  if (/^[a-z]{3}$/i.test(value)) return `${value.toUpperCase()} `;

  return DEFAULT_CURRENCY_SYMBOL;
}

export function formatMoney(amount?: number, currency?: string, empty: "zero" | "dash" = "zero"): string {
  const symbol = currencySymbol(currency);
  if (typeof amount !== "number" || amount <= 0) {
    return empty === "dash" ? "-" : `${symbol}0.0000`;
  }
  return `${symbol}${amount < 1 ? amount.toFixed(4) : amount.toFixed(2)}`;
}
