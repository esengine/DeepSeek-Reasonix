// ContextPanel shows the active tab's context gauge, token usage, read files,
// and workspace changes. All visible text is routed through the i18n dictionary.
import { useCallback, useEffect, useState } from "react";
import { FileText } from "lucide-react";
import { asArray } from "../lib/array";
import { app } from "../lib/bridge";
import { useT, type Translator } from "../lib/i18n";
import { formatMoney } from "../lib/money";
import type { DictKey } from "../locales/en";
import type { ContextInfo, ContextPanelInfo, WireUsage } from "../lib/types";

interface ContextPanelProps {
  tabId?: string;
  context?: ContextInfo;
  usage?: WireUsage;
  sessionTokens?: number;
  sessionCost?: number;
  sessionCurrency?: string;
  refreshKey?: number;
  onOpenWorkspaceMode?: (mode: "files" | "changed") => void;
  onOpenWorkspaceFile?: (path: string) => void;
  onOpenWorkspaceFileList?: (paths: string[]) => void;
  onOpenWorkspaceChangeList?: (changes: ContextFileRow[]) => void;
  onOpenWorkspaceChangeFile?: (path: string) => void;
}

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

interface HealthResult {
  tone: "good" | "notice" | "warn";
  shortKey: DictKey;
  vars: Record<string, string | number>;
}

type ContextFileRow = { key: string; path: string; meta: string; time: string; detail: string };

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
  if (usage?.cost && usage.cost > 0) {
    return { amount: usage.cost, currency: usage.currency || sessionCurrency || info?.sessionCurrency };
  }
  if (info?.sessionCostUsd && info.sessionCostUsd > 0) {
    return { amount: info.sessionCostUsd, currency: info.sessionCurrency || sessionCurrency || usage?.currency };
  }
  if (usage?.costUsd && usage.costUsd > 0) {
    return { amount: usage.costUsd, currency: usage.currency || sessionCurrency || info?.sessionCurrency };
  }
  return { amount: 0, currency: info?.sessionCurrency || sessionCurrency || usage?.currency };
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

function contextHealth(usagePct: number, cachePct: number, readCount: number): HealthResult {
  if (usagePct >= 85) {
    return {
      tone: "warn",
      shortKey: "context.healthNearLimitShort",
      vars: { pct: usagePct },
    };
  }
  if (readCount >= 8) {
    return {
      tone: "notice",
      shortKey: "context.healthManyFilesShort",
      vars: { count: readCount },
    };
  }
  if (cachePct > 0 && cachePct < 50) {
    return {
      tone: "notice",
      shortKey: "context.healthLowCacheShort",
      vars: { pct: cachePct },
    };
  }
  return {
    tone: "good",
    shortKey: "context.healthGoodShort",
    vars: {},
  };
}

export function ContextPanel({
  tabId,
  context,
  usage,
  sessionTokens,
  sessionCost,
  sessionCurrency,
  refreshKey,
  onOpenWorkspaceMode,
  onOpenWorkspaceFile,
  onOpenWorkspaceFileList,
  onOpenWorkspaceChangeList,
  onOpenWorkspaceChangeFile,
}: ContextPanelProps) {
  const t = useT();
  const [info, setInfo] = useState<ContextPanelInfo | null>(null);

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

  const hasPanelUsage = Boolean(
    (info?.requestCount ?? 0) > 0 ||
    (info?.promptTokens ?? 0) > 0 ||
    (info?.completionTokens ?? 0) > 0 ||
    (info?.totalTokens ?? 0) > 0 ||
    (info?.reasoningTokens ?? 0) > 0 ||
    (info?.cacheHitTokens ?? 0) > 0 ||
    (info?.cacheMissTokens ?? 0) > 0
  );
  const usedTokens = context?.used && context.used > 0 ? context.used : info?.usedTokens ?? 0;
  const windowTokens = context?.window && context.window > 0 ? context.window : info?.windowTokens ?? 0;
  const promptTokens = hasPanelUsage ? info?.promptTokens ?? 0 : usage?.promptTokens ?? 0;
  const completionTokens = hasPanelUsage ? info?.completionTokens ?? 0 : usage?.completionTokens ?? 0;
  const totalTokens = info?.totalTokens && info.totalTokens > 0
    ? info.totalTokens
    : sessionTokens && sessionTokens > 0
      ? sessionTokens
      : usage?.totalTokens && usage.totalTokens > 0
        ? usage.totalTokens
        : promptTokens + completionTokens;
  const reasoningTokens = hasPanelUsage ? info?.reasoningTokens ?? 0 : usage?.reasoningTokens ?? 0;
  const cacheHitTokens = hasPanelUsage ? info?.cacheHitTokens ?? 0 : usage?.cacheHitTokens ?? 0;
  const cacheMissTokens = hasPanelUsage ? info?.cacheMissTokens ?? 0 : usage?.cacheMissTokens ?? 0;
  const cost = contextCostDisplay({ info, sessionCost, sessionCurrency, usage });
  const readFiles = asArray(info?.readFiles);
  const changedFiles = asArray(info?.changedFiles);

  const usagePct = windowTokens > 0 ? Math.min(100, Math.round((usedTokens / windowTokens) * 100)) : 0;
  const compactPct = context?.compactRatio ? Math.round(context.compactRatio * 100) : 0;
  const cachePct = cacheHitTokens + cacheMissTokens > 0
    ? Math.round((cacheHitTokens / (cacheHitTokens + cacheMissTokens)) * 100)
    : 0;
  const breakdown = contextBreakdown(usedTokens, windowTokens, promptTokens, completionTokens, reasoningTokens);
  const donutStyle = {
    background: `conic-gradient(#13a7a5 0 ${breakdown.promptPct}%, #2f6df6 ${breakdown.promptPct}% ${breakdown.completionPct}%, #f97316 ${breakdown.completionPct}% ${breakdown.reasoningPct}%, var(--border) ${breakdown.reasoningPct}% ${breakdown.otherPct}%, var(--border-soft) ${breakdown.otherPct}% 100%)`,
  };
  const eventTimes = [
    ...readFiles.map((file) => file.time),
    ...changedFiles.map((file) => file.latestTime ?? 0),
  ].filter((time) => time > 0);
  const derivedElapsed = eventTimes.length > 1 ? Math.max(...eventTimes) - Math.min(...eventTimes) : 0;
  const elapsed = info?.elapsedMs && info.elapsedMs > 0 ? info.elapsedMs : derivedElapsed;
  const derivedRequestCount = Math.max(readFiles.length + changedFiles.length, 0);
  const requestCount = info?.requestCount && info.requestCount > 0 ? info.requestCount : derivedRequestCount;
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
  const health = contextHealth(usagePct, cachePct, readRows.length);

  return (
    <div className="context-panel">
      <div className="context-panel__body">
        <section className="context-panel__overview">
          <section className="context-panel__usage">
            <SectionHeading title={t("context.windowTitle")} meta={t("context.windowSubtitle")} />
            <div className="context-panel__usage-visual">
              <div className="context-panel__donut" style={donutStyle}>
                <div className="context-panel__donut-core">
                  <strong>{fmtTokens(usedTokens)}</strong>
                  <span>/ {fmtTokens(windowTokens)} tokens</span>
                </div>
              </div>
              <div className="context-panel__percent">{usagePct}%</div>
            </div>
            <div className="context-panel__breakdown">
              <TokenLegend label={t("context.prompt")} value={breakdown.promptTokens} color="prompt" />
              <TokenLegend label={t("context.completion")} value={breakdown.completionTokens} color="completion" />
              <TokenLegend label={t("context.reasoning")} value={breakdown.reasoningTokens} color="reasoning" />
              <TokenLegend label={t("context.other")} value={breakdown.otherTokens} color="other" />
              <div className="context-panel__total">
                <span>{t("context.total")}</span>
                <strong>{usedTokens.toLocaleString()} / {windowTokens.toLocaleString()}</strong>
              </div>
            </div>
          </section>
          <section className="context-panel__section">
            <SectionHeading title={t("context.runtimeMetrics")} />
            <div className="context-panel__stats">
              <MetricCard label={t("context.time")} value={fmtDuration(elapsed, t)} />
              <MetricCard label={t("context.requests")} value={requestCount > 0 ? String(requestCount) : "-"} />
              <MetricCard label={t("context.sessionTokens")} value={totalTokens > 0 ? totalTokens.toLocaleString() : "-"} wide />
            </div>
          </section>
          <section className="context-panel__section">
            <SectionHeading title={t("context.costMetrics")} />
            <div className="context-panel__stats">
              <MetricCard label={t("context.cacheHit")} value={cachePct > 0 ? `${cachePct}%` : "-"} tone="accent" />
              <MetricCard label={t("context.sessionCost")} value={formatMoney(cost.amount, cost.currency, "dash")} />
            </div>
          </section>
          <section className="context-panel__section context-panel__section--status">
            <SectionHeading title={t("context.sessionStatus")} />
            <div className="context-panel__stats">
              <MetricCard label={t("context.health")} value={t(health.shortKey, health.vars)} tone={health.tone} />
              <MetricCard label={t("context.compaction")} value={compactPct > 0 ? `${compactPct}%` : "-"} />
            </div>
          </section>
          <PreviewSection
            title={t("context.referencedFiles")}
            meta={t("context.readMeta", { count: readRows.length })}
            action={t("context.viewAll")}
            onAction={() => {
              if (onOpenWorkspaceFileList) {
                onOpenWorkspaceFileList(readRows.map((row) => row.path));
                return;
              }
              onOpenWorkspaceMode?.("files");
            }}
            onRowAction={onOpenWorkspaceFile}
            rows={readRows.slice(0, 3)}
            empty={t("context.noReads")}
          />
          <PreviewSection
            title={t("context.sessionChanges")}
            meta={t("context.changedMeta", { count: changedRows.length })}
            action={t("context.viewAll")}
            onAction={() => {
              if (onOpenWorkspaceChangeList) {
                onOpenWorkspaceChangeList(changedRows);
                return;
              }
              onOpenWorkspaceMode?.("changed");
            }}
            onRowAction={onOpenWorkspaceChangeFile}
            rows={changedRows.slice(0, 3)}
            empty={t("context.noChanges")}
          />
        </section>
      </div>

    </div>
  );
}

function SectionHeading({ title, meta }: { title: string; meta?: string }) {
  return (
    <header className="context-panel__section-head">
      <h3>{title}</h3>
      {meta && <span>{meta}</span>}
    </header>
  );
}

function PreviewSection({
  title,
  meta,
  action,
  onAction,
  onRowAction,
  rows,
  empty,
}: {
  title: string;
  meta?: string;
  action: string;
  onAction: () => void;
  onRowAction?: (path: string) => void;
  rows: ContextFileRow[];
  empty: string;
}) {
  return (
    <section className="context-panel__preview">
      <header className="context-panel__preview-head">
        <h3>{title}</h3>
        {meta && <span>{meta}</span>}
        {rows.length > 0 && <button type="button" onClick={onAction}>{action}</button>}
      </header>
      <FileTable rows={rows} empty={empty} compact onRowAction={onRowAction} />
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

function MetricCard({ label, value, tone, wide }: { label: string; value: string; tone?: "accent" | "good" | "notice" | "warn"; wide?: boolean }) {
  const toneClass = tone ? ` context-panel__metric--${tone}` : "";
  const wideClass = wide ? " context-panel__metric--wide" : "";
  return (
    <div className={`context-panel__metric${toneClass}${wideClass}`}>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function FileTable({
  rows,
  empty,
  compact = false,
  onRowAction,
}: {
  rows: ContextFileRow[];
  empty: string;
  compact?: boolean;
  onRowAction?: (path: string) => void;
}) {
  if (rows.length === 0) return <div className="context-panel__empty">{empty}</div>;
  return (
    <div className={`context-panel__file-list${compact ? " context-panel__file-list--compact" : ""}`}>
      {rows.map((row) => {
        const content = (
          <>
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
          </>
        );
        if (onRowAction) {
          return (
            <button
              className="context-panel__file-row context-panel__file-row--button"
              key={row.key}
              type="button"
              title={row.path}
              onClick={() => onRowAction(row.path)}
            >
              {content}
            </button>
          );
        }
        return (
          <div className="context-panel__file-row" key={row.key} title={row.path}>
            {content}
          </div>
        );
      })}
    </div>
  );
}
