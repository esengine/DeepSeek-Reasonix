import type { DictKey } from "../locales/en";

export interface ContextWindowStatus {
  tone: "good" | "notice" | "warn";
  key: DictKey;
}

export function formatCacheHitRate(hitTokens: number, missTokens: number): string {
  const denom = hitTokens + missTokens;
  if (denom <= 0) return "-";
  return `${((hitTokens / denom) * 100).toFixed(2)}%`;
}

export function contextWindowStatus(rawUsagePct: number, compactPct: number): ContextWindowStatus {
  if (rawUsagePct > 100) return { tone: "warn", key: "context.windowStatusOverLimit" };
  const usagePct = Math.min(100, Math.max(0, rawUsagePct));
  if (usagePct >= 90) return { tone: "warn", key: "context.windowStatusNearLimit" };
  if (compactPct > 0 && usagePct >= compactPct) return { tone: "warn", key: "context.windowStatusPastCompact" };
  if (compactPct > 0 && usagePct >= Math.max(0, compactPct - 10)) return { tone: "notice", key: "context.windowStatusWatch" };
  return { tone: "good", key: "context.windowStatusHealthy" };
}
