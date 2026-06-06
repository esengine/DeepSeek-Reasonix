// ContextPanel shows the active tab's context gauge, token usage, read files,
// and workspace changes. All visible text is routed through the i18n dictionary.
import { useCallback, useEffect, useState } from "react";
import { ArrowLeft, FileText, Search } from "lucide-react";
import { asArray } from "../lib/array";
import { app } from "../lib/bridge";
import { useT, type Translator } from "../lib/i18n";
import type { DictKey } from "../locales/en";
import type { ContextInfo, ContextPanelInfo, WireUsage } from "../lib/types";

interface ContextPanelProps {
  tabId?: string;
  context?: ContextInfo;
  usage?: WireUsage;
  sessionCost?: number;
  sessionCurrency?: string;
  scopeLabel?: string;
  refreshKey?: number;
}

type ContextDetail = "read" | "changed";

function fmtTokens(n: number): string {
  if (n >= 1000) return `${Math.round(n / 1000)}k`;
  return String(n);
}

function fmtTime(ms?: number): string {
  if (!ms) return "";
  return new Date(ms).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function fmtDuration(ms: number, t: Translator): string {
  if (ms <= 0) return "-";
  const totalSeconds = Math.max(1, Math.round(ms / 1000));
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes <= 0) return t("context.durationSeconds", { seconds });
  return t("context.durationMinutesSeconds", { minutes, seconds });
}

function currencySymbol(currency?: string): string {
  const value = (currency || "¥").trim();
  if (/^(cny|rmb|yuan)$/i.test(value)) return "¥";
  if (/^(usd|dollar)$/i.test(value)) return "$";
  return value || "¥";
}

function fmtMoney(amount: number, currency?: string): string {
  if (amount <= 0) return "-";
  const symbol = currencySymbol(currency);
  return `${symbol}${amount < 1 ? amount.toFixed(4) : amount.toFixed(2)}`;
}

interface HealthResult {
  tone: "good" | "notice" | "warn";
  labelKey: DictKey;
  bodyKey: DictKey;
  vars: Record<string, string | number>;
}

function contextHealth(usagePct: number, cachePct: number, readCount: number): HealthResult {
  if (usagePct >= 85) {
    return {
      tone: "warn",
      labelKey: "context.healthNearLimit",
      bodyKey: "context.healthNearLimitBody",
      vars: { pct: usagePct },
    };
  }
  if (readCount >= 8) {
    return {
      tone: "notice",
      labelKey: "context.healthManyFiles",
      bodyKey: "context.healthManyFilesBody",
      vars: { count: readCount },
    };
  }
  if (cachePct > 0 && cachePct < 50) {
    return {
      tone: "notice",
      labelKey: "context.healthLowCache",
      bodyKey: "context.healthLowCacheBody",
      vars: { pct: cachePct },
    };
  }
  return {
    tone: "good",
    labelKey: "context.healthGood",
    bodyKey: "context.healthGoodBody",
    vars: {},
  };
}

export function ContextPanel({ tabId, context, usage, sessionCost, sessionCurrency, scopeLabel, refreshKey }: ContextPanelProps) {
  const t = useT();
  const [info, setInfo] = useState<ContextPanelInfo | null>(null);
  const [detailView, setDetailView] = useState<ContextDetail | null>(null);
  const [query, setQuery] = useState("");

  const refresh = useCallback(async () => {
    if (!tabId) return;
    try {
      setInfo(await app.ContextPanel(tabId));
    } catch {
      /* bridge unavailable */
    }
  }, [tabId]);

  useEffect(() => {
    const id = window.setInterval(() => void refresh(), 2000);
    return () => window.clearInterval(id);
  }, [refresh]);

  useEffect(() => {
    void refresh();
  }, [refresh, refreshKey]);

  const usedTokens = context?.used && context.used > 0 ? context.used : info?.usedTokens ?? 0;
  const windowTokens = context?.window && context.window > 0 ? context.window : info?.windowTokens ?? 0;
  const promptTokens = usage?.promptTokens && usage.promptTokens > 0 ? usage.promptTokens : info?.promptTokens ?? 0;
  const completionTokens = usage?.completionTokens && usage.completionTokens > 0 ? usage.completionTokens : info?.completionTokens ?? 0;
  const reasoningTokens = usage?.reasoningTokens && usage.reasoningTokens > 0 ? usage.reasoningTokens : info?.reasoningTokens ?? 0;
  const cacheHitTokens = usage?.cacheHitTokens && usage.cacheHitTokens > 0 ? usage.cacheHitTokens : info?.cacheHitTokens ?? 0;
  const cacheMissTokens = usage?.cacheMissTokens && usage.cacheMissTokens > 0 ? usage.cacheMissTokens : info?.cacheMissTokens ?? 0;
  const cost = sessionCost && sessionCost > 0 ? sessionCost : info?.sessionCost ?? info?.sessionCostUsd ?? 0;
  const currency = sessionCurrency || info?.sessionCurrency || usage?.currency || "¥";
  const readFiles = asArray(info?.readFiles);
  const changedFiles = asArray(info?.changedFiles);

  const usagePct = windowTokens > 0 ? Math.round((usedTokens / windowTokens) * 100) : 0;
  const cachePct = cacheHitTokens + cacheMissTokens > 0
    ? Math.round((cacheHitTokens / (cacheHitTokens + cacheMissTokens)) * 100)
    : 0;
  const otherTokens = Math.max(0, usedTokens - promptTokens - completionTokens - reasoningTokens);
  const safeUsed = Math.max(usedTokens, 1);
  const promptPct = Math.min(100, (promptTokens / safeUsed) * usagePct);
  const completionPct = Math.min(100, promptPct + (completionTokens / safeUsed) * usagePct);
  const reasoningPct = Math.min(100, completionPct + (reasoningTokens / safeUsed) * usagePct);
  const otherPct = Math.min(100, reasoningPct + (otherTokens / safeUsed) * usagePct);
  const donutStyle = {
    background: `conic-gradient(#13a7a5 0 ${promptPct}%, #2f6df6 ${promptPct}% ${completionPct}%, #f97316 ${completionPct}% ${reasoningPct}%, var(--border) ${reasoningPct}% ${otherPct}%, var(--border-soft) ${otherPct}% 100%)`,
  };
  const eventTimes = [
    ...readFiles.map((file) => file.time),
    ...changedFiles.map((file) => file.latestTime ?? 0),
  ].filter((time) => time > 0);
  const elapsed = eventTimes.length > 1 ? Math.max(...eventTimes) - Math.min(...eventTimes) : 0;
  const requestCount = Math.max(readFiles.length + changedFiles.length, 0);
  const readRows = readFiles.map((f, i) => ({
    key: `${f.path}-${i}`,
    path: f.path,
    meta: `#${f.turn}`,
    time: fmtTime(f.time),
    detail: f.limit ? `${f.offset ?? 0}-${(f.offset ?? 0) + f.limit}${f.truncated ? " truncated" : ""}` : "",
  }));
  const changedRows = changedFiles.map((f, i) => ({
    key: `${f.path}-${i}`,
    path: f.path,
    meta: f.gitStatus || asArray(f.sources).join(", ") || "changed",
    time: fmtTime(f.latestTime),
    detail: asArray(f.turns).length > 0 ? `T${asArray(f.turns).join(",")}` : "",
  }));
  const normalizedQuery = query.trim().toLowerCase();
  const filterRows = (rows: typeof readRows) => {
    if (!normalizedQuery) return rows;
    return rows.filter((row) =>
      `${row.path} ${row.meta} ${row.time} ${row.detail}`.toLowerCase().includes(normalizedQuery)
    );
  };
  const filteredReadRows = filterRows(readRows);
  const filteredChangedRows = filterRows(changedRows);
  const health = contextHealth(usagePct, cachePct, readRows.length);
  const detailRows = detailView === "changed" ? filteredChangedRows : filteredReadRows;
  const detailTitle = detailView === "changed" ? t("context.sessionChanges") : t("context.referencedFiles");
  const detailCount = detailView === "changed" ? changedRows.length : readRows.length;
  const detailEmpty = detailView === "changed" ? t("context.noChanges") : t("context.noReads");
  const detailPlaceholder = detailView === "changed" ? t("context.filterChanges") : t("context.filterReads");
  const detailNote = detailView === "changed"
    ? t("context.changedNote", { count: detailCount })
    : t("context.readNote", { count: detailCount });

  const openDetail = (next: ContextDetail) => {
    setDetailView(next);
    setQuery("");
  };

  const closeDetail = () => {
    setDetailView(null);
    setQuery("");
  };

  return (
    <div className="context-panel">
      <div className="context-panel__summary-head">
        <div className="context-panel__heading-main">
          <span>{detailView ? detailTitle : t("context.overview")}</span>
          <strong>{scopeLabel || t("context.scopeGlobal")}</strong>
        </div>
        {detailView && (
          <button className="context-panel__back" type="button" onClick={closeDetail}>
            <ArrowLeft size={13} />
            {t("rightDock.overview")}
          </button>
        )}
      </div>

      {detailView && (
        <label className="context-panel__search">
          <Search size={14} />
          <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={detailPlaceholder} />
        </label>
      )}

      <div className="context-panel__body">
        {detailView ? (
          <section className="context-panel__detail">
            <div className="context-panel__detail-note">{detailNote}</div>
            <FileTable
              empty={detailEmpty}
              rows={detailRows}
            />
          </section>
        ) : (
          <section className="context-panel__overview">
            <section className="context-panel__usage">
              <div className="context-panel__donut" style={donutStyle}>
                <div className="context-panel__donut-core">
                  <strong>{fmtTokens(usedTokens)}</strong>
                  <span>/ {fmtTokens(windowTokens)} tokens</span>
                </div>
              </div>
              <div className="context-panel__percent">{usagePct}%</div>
              <div className="context-panel__breakdown">
                <TokenLegend label={t("context.prompt")} value={promptTokens} color="prompt" />
                <TokenLegend label={t("context.completion")} value={completionTokens} color="completion" />
                <TokenLegend label={t("context.reasoning")} value={reasoningTokens} color="reasoning" />
                <TokenLegend label={t("context.other")} value={otherTokens} color="other" />
                <div className="context-panel__total">
                  <span>{t("context.total")}</span>
                  <strong>{usedTokens.toLocaleString()} / {windowTokens.toLocaleString()}</strong>
                </div>
              </div>
              <div className="context-panel__stats">
                <MetricCard label={t("context.cacheHit")} value={cachePct > 0 ? `${cachePct}%` : "-"} tone="accent" />
                <MetricCard label={t("context.sessionCost")} value={fmtMoney(cost, currency)} />
                <MetricCard label={t("context.requests")} value={requestCount > 0 ? String(requestCount) : "-"} />
                <MetricCard label={t("context.time")} value={fmtDuration(elapsed, t)} />
              </div>
            </section>
            <div className={`context-panel__health context-panel__health--${health.tone}`}>
              <span>{t("context.health")}</span>
              <strong>{t(health.labelKey, health.vars)}</strong>
              <small>{t(health.bodyKey, health.vars)}</small>
            </div>
            <PreviewSection
              title={t("context.referencedFiles")}
              meta={t("context.readMeta", { count: readRows.length })}
              action={t("context.viewAll")}
              onAction={() => openDetail("read")}
              rows={readRows.slice(0, 3)}
              empty={t("context.noReads")}
            />
            <PreviewSection
              title={t("context.sessionChanges")}
              meta={t("context.changedMeta", { count: changedRows.length })}
              action={t("context.viewAll")}
              onAction={() => openDetail("changed")}
              rows={changedRows.slice(0, 3)}
              empty={t("context.noChanges")}
            />
          </section>
        )}
      </div>

      <footer className="context-panel__scope">
        <FileText size={14} />
        <span>{scopeLabel || t("context.scopeGlobal")}</span>
      </footer>
    </div>
  );
}

function PreviewSection({
  title,
  meta,
  action,
  onAction,
  rows,
  empty,
}: {
  title: string;
  meta?: string;
  action: string;
  onAction: () => void;
  rows: Array<{ key: string; path: string; meta: string; time: string; detail: string }>;
  empty: string;
}) {
  return (
    <section className="context-panel__preview">
      <header className="context-panel__preview-head">
        <h3>{title}</h3>
        {meta && <span>{meta}</span>}
        {rows.length > 0 && <button type="button" onClick={onAction}>{action}</button>}
      </header>
      <FileTable rows={rows} empty={empty} compact />
    </section>
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

function MetricCard({ label, value, tone }: { label: string; value: string; tone?: "accent" }) {
  return (
    <div className="context-panel__metric">
      <span>{label}</span>
      <strong className={tone === "accent" ? "context-panel__metric-accent" : ""}>{value}</strong>
    </div>
  );
}

function FileTable({
  rows,
  empty,
  compact = false,
}: {
  rows: Array<{ key: string; path: string; meta: string; time: string; detail: string }>;
  empty: string;
  compact?: boolean;
}) {
  if (rows.length === 0) return <div className="context-panel__empty">{empty}</div>;
  return (
    <div className={`context-panel__file-list${compact ? " context-panel__file-list--compact" : ""}`}>
      {rows.map((row) => (
        <div className="context-panel__file-row" key={row.key}>
          <span className="context-panel__file-main">
            <FileText size={14} />
            <span className="context-panel__file-copy">
              <span>{row.path}</span>
              {row.detail && <small>{row.detail}</small>}
            </span>
          </span>
          <span className="context-panel__file-meta">
            <span className="context-panel__file-turn">{row.meta}</span>
            {row.time && <span>{row.time}</span>}
          </span>
        </div>
      ))}
    </div>
  );
}
