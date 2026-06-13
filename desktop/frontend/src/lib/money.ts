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

interface MoneyFormatOptions {
  locale?: string;
  empty?: "zero" | "dash";
}

function isoCurrencyCode(currency?: string): string | null {
  const value = (currency || "").trim();
  if (!/^[a-z]{3}$/i.test(value)) return null;
  const code = value.toUpperCase();
  try {
    new Intl.NumberFormat("en", { style: "currency", currency: code }).format(0);
    return code;
  } catch {
    return null;
  }
}

export function formatMoneyLocalized(amount?: number, currency?: string, options: MoneyFormatOptions = {}): string {
  const empty = options.empty ?? "zero";
  if (typeof amount !== "number" || amount <= 0) {
    return empty === "dash" ? "-" : formatMoney(0, currency, empty);
  }

  const code = isoCurrencyCode(currency);
  if (!code) return formatMoney(amount, currency, empty);

  const digits = amount < 1 ? 4 : 2;
  return new Intl.NumberFormat(options.locale, {
    style: "currency",
    currency: code,
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(amount);
}
