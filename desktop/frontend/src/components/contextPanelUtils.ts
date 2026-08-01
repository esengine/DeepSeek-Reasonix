// Pure utility functions extracted from ContextPanel.tsx so that initial-path
// consumers (ContextWindowRing → Composer) can import them without pulling the
// full ContextPanel React component into the initial bundle.
import type { Locale } from "../lib/i18n";
import type { DictKey } from "../locales/en";
import type { ContextInfo, ContextPanelInfo, UsageSourceStats, WireUsage } from "../lib/types";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface MetricTokenDisplay {
  display: string;
  exact: string;
}

export type MetricTone = "accent" | "good" | "notice" | "warn";
export type UsageAnalysisView = "source" | "type";

export type ContextUsageRefreshFields = Pick<
  WireUsage,
  "totalTokens" | "promptTokens" | "completionTokens" | "reasoningTokens" | "sessionCacheHitTokens" | "sessionCacheMissTokens"
>;

export interface ContextWindowStatus {
  tone: "good" | "notice" | "warn";
  key: DictKey;
}

export interface ContextBreakdown {
  promptTokens: number;
  completionTokens: number;
  reasoningTokens: number;
  otherTokens: number;
  promptPct: number;
  completionPct: number;
  reasoningPct: number;
  otherPct: number;
}

export interface ContextSourceRow {
  source: string;
  label: string;
  promptTokens: number;
  completionTokens: number;
  cacheHitTokens: number;
  cacheMissTokens: number;
  totalTokens: number;
  cost: number;
  currency?: string;
  requests: number;
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

function numberLocale(locale: Locale | string): string {
  if (locale === "zh") return "zh-CN";
  if (locale === "zh-TW") return "zh-TW";
  return "en";
}

function nonNegativeTokenCount(value: number): number {
  return Number.isFinite(value) ? Math.max(0, value) : 0;
}

const SOURCE_ORDER = ["executor", "planner", "subagent", "compaction", "classifier", "title"];

function sourceCost(stats: UsageSourceStats): number {
  return stats.sessionCost && stats.sessionCost > 0 ? stats.sessionCost : stats.sessionCostUsd ?? 0;
}

// ---------------------------------------------------------------------------
// Exported utilities
// ---------------------------------------------------------------------------

export function formatMetricTokens(tokens: number | undefined, locale: Locale | string): MetricTokenDisplay {
  if (typeof tokens !== "number" || tokens <= 0) {
    return { display: "-", exact: "-" };
  }
  const tag = numberLocale(locale);
  const exact = tokens.toLocaleString(tag);
  return { display: exact, exact };
}

export function formatCacheHitRate(hitTokens: number, missTokens: number): string {
  const denom = hitTokens + missTokens;
  if (denom <= 0) return "-";
  return `${((hitTokens / denom) * 100).toFixed(2)}%`;
}

export function contextUsageRefreshKey(usage?: ContextUsageRefreshFields): string {
  if (!usage) return "";
  return [
    usage.totalTokens ?? 0,
    usage.promptTokens ?? 0,
    usage.completionTokens ?? 0,
    usage.reasoningTokens ?? 0,
    usage.sessionCacheHitTokens ?? 0,
    usage.sessionCacheMissTokens ?? 0,
  ].join(":");
}

export function cacheHitTone(hitTokens: number, missTokens: number): MetricTone | undefined {
  const denom = hitTokens + missTokens;
  if (denom <= 0) return undefined;
  const pct = (hitTokens / denom) * 100;
  if (pct >= 80) return "good";
  if (pct >= 60) return "notice";
  return "warn";
}

export function contextCostDisplay({
  info,
  sessionCost,
  sessionCurrency,
  usage,
}: {
  info?: Pick<ContextPanelInfo, "sessionCost" | "sessionCurrency" | "sessionCostUsd"> | null;
  sessionCost?: number;
  sessionCurrency?: string;
  usage?: Pick<WireUsage, "cost" | "costUsd" | "currency">;
}): { amount: number; currency?: string } {
  if (info?.sessionCost && info.sessionCost > 0) {
    return { amount: info.sessionCost, currency: info.sessionCurrency || sessionCurrency || usage?.currency };
  }
  if (sessionCost && sessionCost > 0) {
    return { amount: sessionCost, currency: sessionCurrency || info?.sessionCurrency || usage?.currency };
  }
  if (info?.sessionCostUsd && info.sessionCostUsd > 0) {
    return { amount: info.sessionCostUsd, currency: info.sessionCurrency || sessionCurrency || usage?.currency };
  }
  return { amount: 0, currency: info?.sessionCurrency || sessionCurrency || usage?.currency };
}

export function contextSessionCache(
  info?: Pick<ContextPanelInfo, "sessionCacheHitTokens" | "sessionCacheMissTokens"> | null,
  context?: Pick<ContextInfo, "cacheHitTokens" | "cacheMissTokens">,
  usage?: Pick<WireUsage, "sessionCacheHitTokens" | "sessionCacheMissTokens">,
): { hit: number; miss: number } {
  const ctxHit = context?.cacheHitTokens ?? 0;
  const ctxMiss = context?.cacheMissTokens ?? 0;
  if (ctxHit + ctxMiss > 0) return { hit: ctxHit, miss: ctxMiss };
  const infoHit = info?.sessionCacheHitTokens ?? 0;
  const infoMiss = info?.sessionCacheMissTokens ?? 0;
  if (infoHit + infoMiss > 0) return { hit: infoHit, miss: infoMiss };
  return { hit: usage?.sessionCacheHitTokens ?? 0, miss: usage?.sessionCacheMissTokens ?? 0 };
}

export function contextBreakdown(
  usedTokens: number,
  windowTokens: number,
  promptTokens: number,
  completionTokens: number,
  reasoningTokens: number,
): ContextBreakdown {
  const used = nonNegativeTokenCount(usedTokens);
  const window = nonNegativeTokenCount(windowTokens);
  let prompt = nonNegativeTokenCount(promptTokens);
  let reasoning = Math.min(nonNegativeTokenCount(reasoningTokens), nonNegativeTokenCount(completionTokens));
  let completion = Math.max(0, nonNegativeTokenCount(completionTokens) - reasoning);
  const known = prompt + completion + reasoning;

  if (known > used && known > 0) {
    const scale = used / known;
    prompt *= scale;
    completion *= scale;
    reasoning *= scale;
  }

  const normalizedKnown = Math.min(used, prompt + completion + reasoning);
  const other = Math.max(0, used - normalizedKnown);
  const hasWindow = window > 0;
  const promptPct = hasWindow ? Math.min(100, (prompt / window) * 100) : 0;
  const completionPct = hasWindow ? Math.min(100, ((prompt + completion) / window) * 100) : 0;
  const reasoningPct = hasWindow ? Math.min(100, ((prompt + completion + reasoning) / window) * 100) : 0;
  const otherPct = hasWindow ? Math.min(100, (used / window) * 100) : 0;

  return {
    promptTokens: Math.round(prompt),
    completionTokens: Math.round(completion),
    reasoningTokens: Math.round(reasoning),
    otherTokens: Math.round(other),
    promptPct,
    completionPct,
    reasoningPct,
    otherPct,
  };
}

export function contextWindowStatus(usagePct: number, compactPct: number): ContextWindowStatus {
  if (usagePct >= 90) return { tone: "warn", key: "context.windowStatusNearLimit" };
  if (compactPct > 0 && usagePct >= compactPct) return { tone: "warn", key: "context.windowStatusPastCompact" };
  if (compactPct > 0 && usagePct >= Math.max(0, compactPct - 10)) return { tone: "notice", key: "context.windowStatusWatch" };
  return { tone: "good", key: "context.windowStatusHealthy" };
}

export function contextSourceRows(info: ContextPanelInfo | null, sessionCurrency?: string): ContextSourceRow[] {
  const entries = Object.entries(info?.sources ?? {});
  if (entries.length === 0) return [];
  return entries
    .filter(([, stats]) =>
      (stats.requestCount ?? 0) > 0 ||
      (stats.promptTokens ?? 0) > 0 ||
      (stats.completionTokens ?? 0) > 0 ||
      (stats.cacheHitTokens ?? 0) > 0 ||
      (stats.cacheMissTokens ?? 0) > 0 ||
      sourceCost(stats) > 0
    )
    .sort(([a], [b]) => {
      const ia = SOURCE_ORDER.indexOf(a);
      const ib = SOURCE_ORDER.indexOf(b);
      if (ia >= 0 || ib >= 0) return (ia >= 0 ? ia : SOURCE_ORDER.length) - (ib >= 0 ? ib : SOURCE_ORDER.length);
      return a.localeCompare(b);
    })
    .map(([source, stats]) => ({
      source,
      label: source,
      promptTokens: stats.promptTokens ?? 0,
      completionTokens: stats.completionTokens ?? 0,
      cacheHitTokens: stats.cacheHitTokens ?? 0,
      cacheMissTokens: stats.cacheMissTokens ?? 0,
      totalTokens: stats.totalTokens ?? 0,
      cost: sourceCost(stats),
      currency: stats.sessionCurrency || sessionCurrency || info?.sessionCurrency,
      requests: stats.requestCount ?? 0,
    }));
}
