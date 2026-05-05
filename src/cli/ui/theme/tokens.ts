export const FG = {
  strong: "#e6edf3",
  body: "#c9d1d9",
  sub: "#8b949e",
  meta: "#6e7681",
  faint: "#484f58",
} as const;

export const TONE = {
  brand: "#79c0ff",
  accent: "#d2a8ff",
  violet: "#b395f5",
  ok: "#7ee787",
  warn: "#f0b07d",
  err: "#ff8b81",
  info: "#79c0ff",
} as const;

/** Used only while a card is streaming/running, so live cards stand out from settled history. */
export const TONE_ACTIVE = {
  brand: "#a5d6ff",
  accent: "#e2c5ff",
  violet: "#c8aaff",
  ok: "#a8f5ad",
  warn: "#ffc99e",
  err: "#ffaba3",
  info: "#a5d6ff",
} as const;

export const SURFACE = {
  bg: "#0a0c10",
  bgInput: "#0d1015",
  bgCode: "#06080c",
  bgElev: "#11141a",
} as const;

export const CARD = {
  user: { color: FG.meta, glyph: "◇" },
  reasoning: { color: TONE.accent, glyph: "◆" },
  streaming: { color: TONE.brand, glyph: "◈" },
  task: { color: TONE.warn, glyph: "▶" },
  tool: { color: TONE.info, glyph: "▣" },
  plan: { color: TONE.accent, glyph: "⊞" },
  diff: { color: TONE.ok, glyph: "±" },
  error: { color: TONE.err, glyph: "✖" },
  warn: { color: TONE.warn, glyph: "⚠" },
  usage: { color: FG.meta, glyph: "Σ" },
  subagent: { color: TONE.violet, glyph: "⌬" },
  approval: { color: TONE.warn, glyph: "?" },
  search: { color: TONE.info, glyph: "⊙" },
  memory: { color: FG.meta, glyph: "⌑" },
  ctx: { color: TONE.brand, glyph: "◔" },
  doctor: { color: FG.meta, glyph: "⚕" },
  branch: { color: TONE.violet, glyph: "⎇" },
} as const;

export type CardTone = keyof typeof CARD;

/** DeepSeek prices in CNY; our internal table is USD divided by 7.2. Multiply back for display. */
export const USD_TO_CNY = 7.2;

export function formatCNY(usd: number, fractionDigits = 4): string {
  const cny = usd * USD_TO_CNY;
  return `¥${cny.toFixed(fractionDigits)}`;
}

export function formatBalance(balance: { currency: string; total: number }): string {
  const sym = balance.currency === "USD" ? "$" : balance.currency === "CNY" ? "¥" : "";
  return sym
    ? `${sym}${balance.total.toFixed(2)}`
    : `${balance.currency} ${balance.total.toFixed(2)}`;
}

export function formatBalanceLabel(balance: { currency: string; total: number }): string {
  const sym = balance.currency === "USD" ? "$" : balance.currency === "CNY" ? "¥" : "";
  const suf = sym ? "" : ` ${balance.currency}`;
  return `w ${sym}${balance.total.toFixed(2)}${suf}`;
}

export function formatWalletDisplay(
  balance: number | undefined,
  balanceCurrency?: string,
): string | null {
  if (balance === undefined) return null;
  const cur = balanceCurrency ?? "";
  const sym = cur === "USD" ? "$" : cur === "CNY" ? "¥" : "";
  if (sym) return `${sym}${balance.toFixed(2)}`;
  if (cur) return `${cur} ${balance.toFixed(2)}`;
  return `${balance.toFixed(2)}`;
}

export function balanceColorCny(cny: number): string {
  if (cny < 5) return TONE.err;
  if (cny < 20) return TONE.warn;
  return TONE.brand;
}

export function formatCost(costUsd: number, balanceCurrency?: string, fractionDigits = 4): string {
  if (balanceCurrency === "USD") return `$${costUsd.toFixed(fractionDigits)}`;
  const cny = costUsd * USD_TO_CNY;
  return `¥${cny.toFixed(fractionDigits)}`;
}

export function balanceColorForBalance(balance: number, currency: string): string {
  const cny = currency === "USD" ? balance * USD_TO_CNY : balance;
  return balanceColorCny(cny);
}
