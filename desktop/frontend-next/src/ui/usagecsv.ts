import type { UsageReport } from "../port/usage";

const cell = (v: string | number) => {
  const s = String(v);
  return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
};

/** The range's days as rows, one money column per currency the range touched:
 *  amounts in two currencies are not one number and are not added here. */
export function usageCsv(report: UsageReport): string {
  const currencies: string[] = [];
  for (const d of report.daily) for (const m of d.cost ?? []) if (!currencies.includes(m.currency)) currencies.push(m.currency);
  const head = ["day", "provider_total_tokens", "input_cache_hit", "input_cache_miss", "requests", ...currencies.map((c) => `cost_${c}`)];
  const rows = report.daily.map((d) => [
    d.day, d.total, d.cacheHit, d.cacheMiss, d.requests,
    ...currencies.map((c) => d.cost?.find((m) => m.currency === c)?.amount ?? ""),
  ]);
  return [head, ...rows].map((r) => r.map(cell).join(",")).join("\n") + "\n";
}
