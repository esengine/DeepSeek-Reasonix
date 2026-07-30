// ContextPanel shows the active tab's context gauge and token usage.
// All visible text is routed through the i18n dictionary.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { asArray } from "../lib/array";
import { app } from "../lib/bridge";
import { useI18n, type Locale, type Translator } from "../lib/i18n";
import { formatMoneyLocalized } from "../lib/money";
import type { DictKey } from "../locales/en";
import type { BalanceInfo, ContextInfo, ContextPanelInfo, UsageSourceStats, WireUsage } from "../lib/types";

interface ContextPanelProps {
  tabId?: string;
  context?: ContextInfo;
  usage?: WireUsage;
  sessionTokens?: number;
  sessionCost?: number;
  sessionCurrency?: string;
  sessionTurns?: number;
  turnTokens?: number;
  turnCost?: number;
  balance?: BalanceInfo;
  sessionGen?: number;
  refreshKey?: number;
  // Monotonic counter bumped by EVERY usage event (executor and subagent).
  // The executor-gated `usage` prop freezes during sub-agent runs, which used
  // to pin 会话指标/用量分析 for minutes; this keeps the snapshot ticking.
  usageSeq?: number;
}

function fmtFullTokens(n: number): string {
  if (n <= 0) return "0";
  return String(Math.round(n));
}

function fmtDuration(ms: number, t: Translator): string {
  if (ms <= 0) return "-";
  const totalSeconds = Math.max(1, Math.round(ms / 1000));
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes <= 0) return t("context.durationSeconds", { seconds });
  return t("context.durationMinutesSeconds", { minutes, seconds });
}

function fmtOptionalTokens(tokens?: number): string {
  if (typeof tokens !== "number" || tokens <= 0) return "-";
  return tokens.toLocaleString();
}

interface MetricTokenDisplay {
  display: string;
  exact: string;
}

function numberLocale(locale: Locale | string): string {
  if (locale === "zh") return "zh-CN";
  if (locale === "zh-TW") return "zh-TW";
  return "en";
}

export function formatMetricTokens(tokens: number | undefined, locale: Locale | string): MetricTokenDisplay {
  if (typeof tokens !== "number" || tokens <= 0) {
    return { display: "-", exact: "-" };
  }
  const tag = numberLocale(locale);
  const exact = tokens.toLocaleString(tag);
  return { display: exact, exact };
}

function fmtUsageCacheRate(usage?: WireUsage): string {
  if (!usage) return "-";
  const denom = usage.cacheHitTokens + usage.cacheMissTokens;
  if (denom <= 0) return "-";
  return `${((usage.cacheHitTokens / denom) * 100).toFixed(2)}%`;
}

export function formatCacheHitRate(hitTokens: number, missTokens: number): string {
  const denom = hitTokens + missTokens;
  if (denom <= 0) return "-";
  return `${((hitTokens / denom) * 100).toFixed(2)}%`;
}

type MetricTone = "accent" | "good" | "notice" | "warn";
type UsageAnalysisView = "source" | "type";
type ContextUsageRefreshFields = Pick<
  WireUsage,
  "totalTokens" | "promptTokens" | "completionTokens" | "reasoningTokens" | "sessionCacheHitTokens" | "sessionCacheMissTokens"
>;

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

function formatSharePercent(value: number, total: number): string {
  if (total <= 0 || value <= 0) return "-";
  const pct = (value / total) * 100;
  if (pct > 0 && pct < 1) return "<1%";
  return `${Math.round(pct)}%`;
}

interface ContextWindowStatus {
  tone: "good" | "notice" | "warn";
  key: DictKey;
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
  // Session-scoped sources only: this value renders under the 会话费用 label,
  // and falling back to a single request's usage.cost silently displayed one
  // turn's spend as the whole session's. usage now contributes currency only.
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

// contextSessionCache picks the session-cumulative cache hit/miss pair for the
// panel's session average. The shared ContextInfo is refreshed after every
// usage event and also drives StatusBar, so prefer it over the panel's
// independently throttled snapshot. Panel telemetry remains the all-sources
// fallback for callers without live context; executor-only wire counters only
// bridge the pre-refresh gap. The pair always comes from one source so the
// computed rate never mixes scopes.
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

interface ContextBreakdown {
  promptTokens: number;
  completionTokens: number;
  reasoningTokens: number;
  otherTokens: number;
  promptPct: number;
  completionPct: number;
  reasoningPct: number;
  otherPct: number;
}

function nonNegativeTokenCount(value: number): number {
  return Number.isFinite(value) ? Math.max(0, value) : 0;
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

const SOURCE_ORDER = ["executor", "planner", "subagent", "compaction", "classifier", "title"];

function sourceTone(source: string): string {
  switch (source) {
    case "executor": return "teal";
    case "planner": return "blue";
    case "subagent": return "amber";
    case "compaction": return "slate";
    case "classifier": return "violet";
    case "title": return "rose";
    default: return "default";
  }
}

function sourceLabel(source: string, t: Translator): string {
  switch (source) {
    case "executor": return t("context.sourceExecutor");
    case "planner": return t("context.sourcePlanner");
    case "subagent": return t("context.sourceSubagent");
    case "compaction": return t("context.sourceCompaction");
    case "classifier": return t("context.sourceClassifier");
    case "title": return t("context.sourceTitle");
    default: return source;
  }
}

function sourceCost(stats: UsageSourceStats): number {
  return stats.sessionCost && stats.sessionCost > 0 ? stats.sessionCost : stats.sessionCostUsd ?? 0;
}

function sourceTokenTotal(row: Pick<ContextSourceRow, "promptTokens" | "completionTokens" | "totalTokens">): number {
  return row.totalTokens > 0 ? row.totalTokens : row.promptTokens + row.completionTokens;
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

export function ContextPanel({
  tabId,
  context,
  usage,
  sessionTokens,
  sessionCost,
  sessionCurrency,
  turnTokens,
  turnCost,
  balance,
  sessionGen,
  refreshKey,
  usageSeq,
}: ContextPanelProps) {
  const { locale, t } = useI18n();
  const [info, setInfo] = useState<ContextPanelInfo | null>(null);
  const [analysisView, setAnalysisView] = useState<UsageAnalysisView>("source");
  const refreshSeq = useRef(0);
  const lastRefreshTime = useRef(0);
  const usageRefreshKey = contextUsageRefreshKey(usage);

  const refresh = useCallback(async () => {
    if (!tabId) return;
    const seq = ++refreshSeq.current;
    try {
      const next = await app.ContextPanel(tabId);
      if (refreshSeq.current === seq) {
        setInfo(next);
      }
    } catch {
      /* bridge unavailable */
    }
  }, [tabId]);

  useEffect(() => {
    refreshSeq.current += 1;
    setInfo(null);
    void refresh();
  }, [refresh, sessionGen]);

  useEffect(() => {
    void refresh();
  }, [refresh, refreshKey]);

  // Refresh the panel snapshot while usage events stream — from any source:
  // usageSeq covers sub-agent/title requests the executor-gated usage prop
  // never reflects, and usageRefreshKey keeps ticking for providers whose
  // events lack a seq. Throttled to once per second.
  useEffect(() => {
    if (!usageRefreshKey && !usageSeq) return;
    const now = Date.now();
    if (now - lastRefreshTime.current >= 1000) {
      lastRefreshTime.current = now;
      void refresh();
    }
  }, [usageRefreshKey, usageSeq, refresh]);

  const usedTokens = context?.used && context.used > 0 ? context.used : info?.usedTokens ?? 0;
  const windowTokens = context?.window && context.window > 0 ? context.window : info?.windowTokens ?? 0;
  // Prefer live usage props (updated in real-time by the reducer during streaming)
  // over the async-fetched info snapshot (only refreshed on turn_done).
  const promptTokens = usage?.promptTokens ?? info?.promptTokens ?? 0;
  const completionTokens = usage?.completionTokens ?? info?.completionTokens ?? 0;
  const totalTokens = info?.totalTokens && info.totalTokens > 0
    ? info.totalTokens
    : sessionTokens && sessionTokens > 0
      ? sessionTokens
      : usage?.totalTokens && usage.totalTokens > 0
        ? usage.totalTokens
        : promptTokens + completionTokens;
  const reasoningTokens = usage?.reasoningTokens ?? info?.reasoningTokens ?? 0;
  // Session-cumulative cache tokens for the top summary: all-sources telemetry
  // first (matching the session cost and per-source rows in this panel — the
  // wire session counters are executor-only), with the live counters bridging
  // only a fresh session's first turn before the telemetry refresh. Hit and
  // miss come as a pair from one source so the rate cannot mix scopes.
  const { hit: sessionCacheHit, miss: sessionCacheMiss } = contextSessionCache(info, context, usage);
  const totalTokensMetric = formatMetricTokens(totalTokens, locale);
  const cost = contextCostDisplay({ info, sessionCost, sessionCurrency, usage });
  const sourceUsageRows = contextSourceRows(info, sessionCurrency);
  const showSourceUsageRows = sourceUsageRows.length > 0;
  const sourceTotalTokens = sourceUsageRows.reduce((sum, row) => sum + sourceTokenTotal(row), 0);
  const visibleSourceRows = sourceUsageRows.slice(0, 3);
  const hiddenSourceRows = sourceUsageRows.slice(3);
  const readFiles = asArray(info?.readFiles);
  const changedFiles = asArray(info?.changedFiles);

  const usagePct = windowTokens > 0 ? Math.min(100, Math.round((usedTokens / windowTokens) * 100)) : 0;
  const compactRatio = context?.compactRatio && context.compactRatio > 0 ? context.compactRatio : 0.8;
  const compactPct = Math.round(compactRatio * 100);
  const compactTokens = windowTokens > 0 ? Math.round(windowTokens * compactRatio) : 0;
  const tokensUntilCompact = compactTokens > usedTokens ? compactTokens - usedTokens : 0;
  const breakdown = contextBreakdown(usedTokens, windowTokens, promptTokens, completionTokens, reasoningTokens);
  const eventTimes = [
    ...readFiles.map((file) => file.time),
    ...changedFiles.map((file) => file.latestTime ?? 0),
  ].filter((time) => time > 0);
  const derivedElapsed = eventTimes.length > 1 ? Math.max(...eventTimes) - Math.min(...eventTimes) : 0;
  const elapsed = info?.elapsedMs && info.elapsedMs > 0 ? info.elapsedMs : derivedElapsed;
  const derivedRequestCount = Math.max(readFiles.length + changedFiles.length, 0);
  const requestCount = info?.requestCount && info.requestCount > 0 ? info.requestCount : derivedRequestCount;
  const windowStatus = contextWindowStatus(usagePct, compactPct);
  const balanceLabel = balance?.available && balance.display ? balance.display : "-";
  const turnCostLabel = formatMoneyLocalized(turnCost, sessionCurrency, { locale, empty: "dash" });
  const sessionCostLabel = formatMoneyLocalized(cost.amount, cost.currency, { locale, empty: "dash" });
  const totalTokensTitle = totalTokensMetric.exact === "-" ? "-" : t("context.tokensValue", { value: totalTokensMetric.exact });
  const usedLabel = fmtFullTokens(usedTokens);
  const windowLabel = fmtFullTokens(windowTokens);
  const compactRemainingLabel = tokensUntilCompact > 0 ? fmtFullTokens(tokensUntilCompact) : "0";
  const compactMarkerPct = Math.max(0, Math.min(100, compactPct));
  const usageMarkerPct = Math.max(6, Math.min(94, usagePct));
  const compactLabelPct = Math.max(6, Math.min(94, compactMarkerPct));
  const usageSummary = t("context.windowUsageSummary", { used: usedLabel, window: windowLabel, pct: usagePct });
  const compactSummary = t("context.windowCompactRemaining", { used: usedLabel, window: windowLabel, tokens: compactRemainingLabel, pct: compactPct });
  const activeAnalysisView: UsageAnalysisView = showSourceUsageRows ? analysisView : "type";
  const tokenTypeRows = [
    { key: "prompt", label: t("context.prompt"), value: breakdown.promptTokens },
    { key: "completion", label: t("context.completion"), value: breakdown.completionTokens },
    { key: "reasoning", label: t("context.reasoning"), value: breakdown.reasoningTokens },
    { key: "other", label: t("context.other"), value: breakdown.otherTokens },
  ];
  const tokenCompositionTotal = tokenTypeRows.reduce((sum, row) => sum + row.value, 0);
  const renderSourceRow = (row: ContextSourceRow) => {
    const inputMetric = formatMetricTokens(row.promptTokens, locale);
    const outputMetric = formatMetricTokens(row.completionTokens, locale);
    const hitMetric = formatMetricTokens(row.cacheHitTokens, locale);
    const missMetric = formatMetricTokens(row.cacheMissTokens, locale);
    const totalMetric = formatMetricTokens(sourceTokenTotal(row), locale);
    const cacheReported = row.cacheHitTokens + row.cacheMissTokens > 0;
    const cacheRate = cacheReported ? formatCacheHitRate(row.cacheHitTokens, row.cacheMissTokens) : t("context.cacheNotReported");
    const costLabel = formatMoneyLocalized(row.cost, row.currency, { locale, empty: "dash" });
    return (
      <div className="context-panel__source-row" key={row.source}>
        <div className="context-panel__source-head">
          <span>
            <i className={`context-panel__source-dot context-panel__source-tone--${sourceTone(row.source)}`} aria-hidden="true" />
            {sourceLabel(row.label, t)}
          </span>
          <em>{t("context.sourceRequests", { count: row.requests })}</em>
        </div>
        <div className="context-panel__source-summary">
          <SourceMetric label={t("context.total")} value={totalMetric.display} title={totalMetric.exact} />
          <SourceMetric label={t("context.sourceCacheRate")} value={cacheRate} />
          <SourceMetric label={t("context.sourceCost")} value={costLabel} />
        </div>
        <details className="context-panel__source-details">
          <summary>{t("context.sourceDetails")}</summary>
          <div className="context-panel__source-details-body">
            <SourceSplitBar
              label={`${t("context.sourceInput")}/${t("context.sourceOutput")}`}
              segments={[
                { label: t("context.sourceInput"), value: row.promptTokens, tone: "input" },
                { label: t("context.sourceOutput"), value: row.completionTokens, tone: "output" },
              ]}
            />
            {cacheReported ? (
              <SourceSplitBar
                label={`${t("context.sourceCacheHit")}/${t("context.sourceCacheMiss")}`}
                segments={[
                  { label: t("context.sourceCacheHit"), value: row.cacheHitTokens, tone: "hit" },
                  { label: t("context.sourceCacheMiss"), value: row.cacheMissTokens, tone: "miss" },
                ]}
                compact
              />
            ) : (
              <SourceSplitBar label={`${t("context.sourceCacheHit")}/${t("context.sourceCacheMiss")}`} segments={[]} compact />
            )}
            <div className="context-panel__source-metrics">
              <SourceMetric label={t("context.sourceInput")} value={inputMetric.display} title={inputMetric.exact} />
              <SourceMetric label={t("context.sourceOutput")} value={outputMetric.display} title={outputMetric.exact} />
              <SourceMetric label={t("context.sourceCacheHit")} value={hitMetric.display} title={hitMetric.exact} />
              <SourceMetric label={t("context.sourceCacheMiss")} value={missMetric.display} title={missMetric.exact} />
            </div>
          </div>
        </details>
      </div>
    );
  };

  return (
    <div className="context-panel">
      <div className="context-panel__body">
        <section className="context-panel__overview">
          <section className="context-panel__usage">
            <SectionHeading title={t("context.windowTitle")} />
            <div className={`context-panel__capacity-card context-panel__capacity-card--${windowStatus.tone}`}>
              <div className="context-panel__capacity-top">
                <span className="context-panel__capacity-status">{t(windowStatus.key)}</span>
                <strong>{usedLabel}/{windowLabel}</strong>
              </div>
              <div className="context-panel__usage-progress context-panel__capacity-meter" aria-label={`${t(windowStatus.key)}. ${usageSummary}. ${compactSummary}`}>
                <div className="context-panel__capacity-scale" aria-hidden="true">
                  <span className="context-panel__capacity-pin context-panel__capacity-pin--used" style={{ left: `${usageMarkerPct}%` }}>{usagePct}%</span>
                  <span className="context-panel__capacity-pin context-panel__capacity-pin--compact" style={{ left: `${compactLabelPct}%` }}>{compactPct}%</span>
                </div>
                <div className="context-panel__progress-track" aria-hidden="true">
                  <span className="context-panel__progress-segment context-panel__progress-segment--prompt" style={{ width: `${breakdown.promptPct}%` }} />
                  <span className="context-panel__progress-segment context-panel__progress-segment--completion" style={{ width: `${Math.max(0, breakdown.completionPct - breakdown.promptPct)}%` }} />
                  <span className="context-panel__progress-segment context-panel__progress-segment--reasoning" style={{ width: `${Math.max(0, breakdown.reasoningPct - breakdown.completionPct)}%` }} />
                  <span className="context-panel__progress-segment context-panel__progress-segment--other" style={{ width: `${Math.max(0, breakdown.otherPct - breakdown.reasoningPct)}%` }} />
                  <span className="context-panel__compact-marker" style={{ left: `${compactMarkerPct}%` }} />
                </div>
              </div>
              <div className="context-panel__capacity-foot">
                <span>{t("context.windowUsedLabel")}</span>
                <span className="context-panel__capacity-remaining">
                  <span>{t("context.windowCompactDistance")}</span>
                  <strong>{compactRemainingLabel}</strong>
                </span>
              </div>
            </div>
          </section>
          <section className="context-panel__section context-panel__session-section">
            <SectionHeading title={t("context.sessionMetrics")} />
            <div className="context-panel__session-metrics">
              <div className="context-panel__summary-rows">
                <MiniStat label={t("status.cacheAvgLabel")} value={formatCacheHitRate(sessionCacheHit, sessionCacheMiss)} tone={cacheHitTone(sessionCacheHit, sessionCacheMiss)} />
                <MiniStat label={t("context.sessionCost")} value={sessionCostLabel} />
                <MiniStat label={t("context.time")} value={fmtDuration(elapsed, t)} />
                <MiniStat label={t("context.requests")} value={requestCount > 0 ? String(requestCount) : "-"} />
                <MiniStat label={t("context.sessionTokensShort")} value={totalTokensMetric.display} title={totalTokensTitle} wide />
              </div>
            </div>
          </section>
          <section className="context-panel__creation-grid" aria-label={t("context.overview")}>
            <MetricCard label={t("status.cacheLabel")} value={fmtUsageCacheRate(usage)} tone="accent" />
            <MetricCard label={t("status.turnTokensLabel")} value={fmtOptionalTokens(turnTokens)} />
            <MetricCard label={t("status.turnCostLabel")} value={turnCostLabel} />
            <MetricCard label={t("status.balanceLabel")} value={balanceLabel} tone="accent" />
          </section>
          <section className="context-panel__section context-panel__analysis">
            <SectionHeading title={t("context.usageAnalysis")}>
              {showSourceUsageRows && (
                <div className="context-panel__view-switch" role="tablist" aria-label={t("context.usageAnalysisView")}>
                  <button
                    type="button"
                    className={`context-panel__view-tab${activeAnalysisView === "source" ? " context-panel__view-tab--active" : ""}`}
                    role="tab"
                    aria-selected={activeAnalysisView === "source"}
                    onClick={() => setAnalysisView("source")}
                  >
                    {t("context.usageAnalysisSource")}
                  </button>
                  <button
                    type="button"
                    className={`context-panel__view-tab${activeAnalysisView === "type" ? " context-panel__view-tab--active" : ""}`}
                    role="tab"
                    aria-selected={activeAnalysisView === "type"}
                    onClick={() => setAnalysisView("type")}
                  >
                    {t("context.usageAnalysisType")}
                  </button>
                </div>
              )}
            </SectionHeading>
            {activeAnalysisView === "source" ? (
              <div className="context-panel__source-list" aria-label={t("context.sourceBreakdown")} role="tabpanel">
                <div className="context-panel__source-overview">
                  <div className="context-panel__source-overview-head">
                    <strong>{t("context.sourceShareTitle")}</strong>
                  </div>
                  <div className="context-panel__source-sharebar" aria-hidden="true">
                    {sourceUsageRows.map((row) => {
                      const sharePct = sourceTotalTokens > 0 ? (sourceTokenTotal(row) / sourceTotalTokens) * 100 : 0;
                      if (sharePct <= 0) return null;
                      return (
                        <span
                          className={`context-panel__source-share context-panel__source-tone--${sourceTone(row.source)}`}
                          key={row.source}
                          style={{ width: `${sharePct}%` }}
                        />
                      );
                    })}
                  </div>
                  <div className="context-panel__source-legend">
                    {sourceUsageRows.map((row) => {
                      const sharePct = sourceTotalTokens > 0 ? (sourceTokenTotal(row) / sourceTotalTokens) * 100 : 0;
                      return (
                        <span key={row.source}>
                          <i className={`context-panel__source-dot context-panel__source-tone--${sourceTone(row.source)}`} aria-hidden="true" />
                          {sourceLabel(row.label, t)} {sharePct > 0 ? `${sharePct.toFixed(0)}%` : "-"}
                        </span>
                      );
                    })}
                  </div>
                </div>
                {visibleSourceRows.map(renderSourceRow)}
                {hiddenSourceRows.length > 0 && (
                  <details className="context-panel__source-more">
                    <summary>{t("context.moreSources", { count: hiddenSourceRows.length })}</summary>
                    <div className="context-panel__source-more-list">
                      {hiddenSourceRows.map(renderSourceRow)}
                    </div>
                  </details>
                )}
              </div>
            ) : (
              <div className="context-panel__type-panel" aria-label={t("context.tokenBreakdown")} role="tabpanel">
                <div className="context-panel__type-overview">
                  <div className="context-panel__type-overview-head">
                    <strong>{t("context.tokenBreakdown")}</strong>
                  </div>
                  <div className="context-panel__type-sharebar" aria-hidden="true">
                    {tokenTypeRows.map((row) => row.value > 0 ? (
                      <span
                        className={`context-panel__type-share context-panel__type-share--${row.key}`}
                        key={row.key}
                        style={{ width: `${(row.value / Math.max(1, tokenCompositionTotal)) * 100}%` }}
                      />
                    ) : null)}
                  </div>
                  <div className="context-panel__type-legend">
                    {tokenTypeRows.map((row) => (
                      <span key={row.key}>
                        <i className={`context-panel__type-dot context-panel__type-dot--${row.key}`} aria-hidden="true" />
                        {row.label} {formatSharePercent(row.value, tokenCompositionTotal)}
                      </span>
                    ))}
                  </div>
                </div>
                <details className="context-panel__breakdown-details">
                  <summary>{t("context.sourceDetails")}</summary>
                  <div className="context-panel__breakdown">
                    {tokenTypeRows.map((row) => (
                      <TokenLegend key={row.key} label={row.label} value={row.value} color={row.key} />
                    ))}
                  </div>
                </details>
              </div>
            )}
          </section>
        </section>
      </div>

    </div>
  );
}

function SectionHeading({ title, meta, children }: { title: string; meta?: string; children?: ReactNode }) {
  return (
    <header className="context-panel__section-head">
      <h3>{title}</h3>
      {meta && <span>{meta}</span>}
      {children}
    </header>
  );
}

function TokenLegend({ label, value, color }: { label: string; value: number; color: string }) {
  return (
    <div className="context-panel__legend-row">
      <span className={`context-panel__legend-dot context-panel__legend-dot--${color}`} />
      <span>{label}</span>
      <strong>{value.toLocaleString()}</strong>
    </div>
  );
}

function MiniStat({ label, value, title, tone, wide }: { label: string; value: string; title?: string; tone?: MetricTone; wide?: boolean }) {
  const toneClass = tone ? ` context-panel__mini-stat--${tone}` : "";
  const wideClass = wide ? " context-panel__mini-stat--wide" : "";
  const exactTitle = title && title !== value ? title : undefined;
  return (
    <div className={`context-panel__mini-stat${toneClass}${wideClass}`} aria-label={exactTitle ? `${label}: ${exactTitle}` : undefined}>
      <span>{label}</span>
      <strong title={exactTitle}>{value}</strong>
    </div>
  );
}

function MetricCard({ label, value, valueTitle, tone, wide }: { label: string; value: string; valueTitle?: string; tone?: "accent" | "good" | "notice" | "warn"; wide?: boolean }) {
  const toneClass = tone ? ` context-panel__metric--${tone}` : "";
  const wideClass = wide ? " context-panel__metric--wide" : "";
  const exactTitle = valueTitle && valueTitle !== value ? valueTitle : undefined;
  return (
    <div className={`context-panel__metric${toneClass}${wideClass}`} aria-label={exactTitle ? `${label}: ${exactTitle}` : undefined}>
      <span>{label}</span>
      <strong title={exactTitle}>{value}</strong>
    </div>
  );
}

function SourceMetric({ label, value, title }: { label: string; value: string; title?: string }) {
  const exactTitle = title && title !== value ? title : undefined;
  return (
    <div className="context-panel__source-metric" aria-label={exactTitle ? `${label}: ${exactTitle}` : undefined}>
      <span>{label}</span>
      <strong title={exactTitle}>{value}</strong>
    </div>
  );
}

function SourceSplitBar({ label, segments, compact }: { label: string; segments: Array<{ label: string; value: number; tone: string }>; compact?: boolean }) {
  const total = segments.reduce((sum, segment) => sum + Math.max(0, segment.value), 0);
  const visible = segments.filter((segment) => segment.value > 0);
  const compactClass = compact ? " context-panel__source-bar--compact" : "";
  if (total <= 0 || visible.length === 0) {
    return (
      <div className="context-panel__source-bar-row">
        <span>{label}</span>
        <div className={`context-panel__source-bar context-panel__source-bar--empty${compactClass}`} aria-hidden="true" />
      </div>
    );
  }
  return (
    <div className="context-panel__source-bar-row">
      <span>{label}</span>
      <div className={`context-panel__source-bar${compactClass}`}>
        {visible.map((segment) => {
          const width = (segment.value / total) * 100;
          return (
            <span
              className={`context-panel__source-bar-segment context-panel__source-bar-segment--${segment.tone}`}
              key={segment.tone}
              style={{ width: `${width}%` }}
              title={`${segment.label}: ${segment.value.toLocaleString()}`}
            />
          );
        })}
      </div>
    </div>
  );
}
