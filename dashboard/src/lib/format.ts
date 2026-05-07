export function fmtUsd(n: number | null | undefined): string {
  if (n === null || n === undefined) return "—";
  if (n === 0) return "$0";
  return `$${n.toFixed(n < 0.01 ? 6 : 4)}`;
}

/** Keep in sync with src/cli/ui/theme/tokens.ts USD_TO_CNY. */
const USD_TO_CNY = 7.2;

/** USD-internal cost rendered in the wallet's display currency. Undefined currency → CNY (matches CLI default). */
export function fmtCost(
  usd: number | null | undefined,
  currency: string | null | undefined,
  fractionDigits?: number,
): string {
  if (usd === null || usd === undefined) return "—";
  const cur = currency ?? "CNY";
  const amount = cur === "CNY" ? usd * USD_TO_CNY : usd;
  if (amount === 0) return cur === "CNY" ? "¥0" : "$0";
  const sym = cur === "CNY" ? "¥" : cur === "USD" ? "$" : `${cur} `;
  const digits = fractionDigits ?? (Math.abs(amount) < 0.01 ? 6 : 4);
  return `${sym}${amount.toFixed(digits)}`;
}

export function fmtPct(n: number | null | undefined): string {
  if (n === null || n === undefined) return "—";
  return `${(n * 100).toFixed(1)}%`;
}

export function fmtNum(n: number | null | undefined): string {
  if (n === null || n === undefined) return "—";
  return n.toLocaleString();
}

export function fmtBytes(n: number | null | undefined): string {
  if (n === null || n === undefined) return "—";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

export function fmtCompactNum(n: number | null | undefined): string {
  if (n === null || n === undefined) return "—";
  if (Math.abs(n) < 1000) return String(n);
  if (Math.abs(n) < 1_000_000) {
    const v = n / 1000;
    return `${v % 1 === 0 ? v.toFixed(0) : v.toFixed(1)}K`;
  }
  if (Math.abs(n) < 1_000_000_000) {
    const v = n / 1_000_000;
    return `${v % 1 === 0 ? v.toFixed(0) : v.toFixed(1)}M`;
  }
  return `${(n / 1_000_000_000).toFixed(1)}B`;
}

export function fmtRelativeTime(iso: string | number | null | undefined): string {
  if (!iso) return "—";
  const ms = typeof iso === "number" ? iso : Date.parse(iso);
  if (!Number.isFinite(ms)) return "—";
  const dSec = (Date.now() - ms) / 1000;
  if (dSec < 60) return "just now";
  if (dSec < 3600) return `${Math.floor(dSec / 60)}m ago`;
  if (dSec < 86400) return `${Math.floor(dSec / 3600)}h ago`;
  if (dSec < 30 * 86400) return `${Math.floor(dSec / 86400)}d ago`;
  return new Date(ms).toISOString().slice(0, 10);
}
