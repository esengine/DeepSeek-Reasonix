import { useEffect, useRef, useState, type ReactNode } from "react";
import { Activity, CircleDollarSign, CircleGauge, Database, FileOutput, Folder, Gauge, GitBranch, HardDrive, Layers, Percent, Puzzle, RefreshCw, Server, Settings, Square, Unplug, Wallet, Zap } from "lucide-react";
import { AnchoredPopover } from "./AnchoredPopover";
import { RemoteConnectionErrorDialog } from "./RemoteConnectionErrorDialog";
import { Tooltip } from "./Tooltip";
import { useI18n, type Translator } from "../lib/i18n";
import { formatMoneyLocalized } from "../lib/money";
import { normalizeStatusBarItems, type StatusBarItemId } from "../lib/statusBarItems";
import { isRemoteDegradedWarning, isRemoteHostKeyMismatch, isRemoteTerminalFailure, remoteConnectionErrorSummaryKey } from "../lib/remoteErrors";
import type { ExtensionStatusEntry } from "../lib/useController";
import { type BackgroundRuntimeView, type BalanceInfo, type ContextInfo, type JobView, type RemoteConnectionStatus, type RemoteHostView, type UsageSourceStats, type WireUsage } from "../lib/types";
import { useRemoteStore } from "../store/remote";

type StatusBarLabelStyle = "icon" | "text";

function formatRate(hit: number, denom: number): string | null {
  if (denom <= 0) return null;
  return ((hit / denom) * 100).toFixed(2);
}

// nowRate is the SINGLE-TURN prompt cache-hit % (latest turn) — the higher,
// steeper number on a non-compacting DeepSeek session. null when nothing yet.
function nowRate(u?: WireUsage): string | null {
  if (!u) return null;
  const denom = u.cacheHitTokens + u.cacheMissTokens;
  return formatRate(u.cacheHitTokens, denom);
}

// avgRate is the SESSION-AGGREGATE cache-hit % — Σhit/Σ(hit+miss) across every
// turn — but scoped to the EXECUTOR agent only: the wire session counters come
// from the main agent and exclude subagent/planner/auxiliary requests. It is
// only the pre-first-refresh fallback; the authoritative all-sources number is
// contextAvgRate below, so the "session average" label reports one scope.
function avgRate(u?: WireUsage): string | null {
  if (!u) return null;
  const denom = u.sessionCacheHitTokens + u.sessionCacheMissTokens;
  return formatRate(u.sessionCacheHitTokens, denom);
}

// contextAvgRate computes the session-aggregate cache-hit % from ContextInfo
// cache tokens — the tab telemetry that accumulates ALL request sources
// (executor, subagents, planner, auxiliary calls), refreshed at turn
// boundaries. Preferred over avgRate: it matches the 会话费用 tooltip's
// "includes main model, subagents and auxiliary calls" scope.
function contextAvgRate(ctx: ContextInfo): string | null {
  const hit = ctx.cacheHitTokens ?? 0;
  const miss = ctx.cacheMissTokens ?? 0;
  return formatRate(hit, hit + miss);
}

function rateValueClass(rate: string | null): string {
  if (rate === null) return "stat__value--empty";
  const pct = Number.parseFloat(rate);
  if (!Number.isFinite(pct)) return "";
  if (pct >= 80) return "statusbar__rate-value--good";
  if (pct >= 50) return "statusbar__rate-value--notice";
  return "statusbar__rate-value--critical";
}

function formatTokenCount(tokens?: number): string {
  if (typeof tokens !== "number" || tokens <= 0) return "-";
  return tokens.toLocaleString();
}

function formatTurnCount(turns: number | undefined, t: Translator): string {
  if (typeof turns !== "number" || turns < 0) return "-";
  return t(turns === 1 ? "history.turnOne" : "history.turnOther", { n: turns });
}
function formatTps(tps?: number | null, estimated = false): string | null {
  if (!tps || tps <= 0) return null;
  const prefix = estimated ? "≈" : "";
  if (tps < 1) return `${prefix}<1 t/s`;
  return `${prefix}${Math.round(tps)} t/s`;
}

const STATUS_SOURCE_ORDER = ["executor", "planner", "subagent", "compaction", "classifier", "title"];

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

function sourceRows(sources?: Record<string, UsageSourceStats>): Array<{ source: string; stats: UsageSourceStats }> {
  return Object.entries(sources ?? {})
    .filter(([, stats]) =>
      (stats.requestCount ?? 0) > 0 ||
      (stats.promptTokens ?? 0) > 0 ||
      (stats.completionTokens ?? 0) > 0 ||
      (stats.cacheHitTokens ?? 0) > 0 ||
      (stats.cacheMissTokens ?? 0) > 0
    )
    .sort(([a], [b]) => {
      const ia = STATUS_SOURCE_ORDER.indexOf(a);
      const ib = STATUS_SOURCE_ORDER.indexOf(b);
      if (ia >= 0 || ib >= 0) return (ia >= 0 ? ia : STATUS_SOURCE_ORDER.length) - (ib >= 0 ? ib : STATUS_SOURCE_ORDER.length);
      return a.localeCompare(b);
    })
    .map(([source, stats]) => ({ source, stats }));
}

function sourceCacheTooltip(t: Translator, title: string, context: ContextInfo): ReactNode {
  const rows = sourceRows(context.sources);
  if (rows.length === 0) return title;
  return (
    <span className="statusbar__tooltip-stack">
      <span>{title}</span>
      {rows.map(({ source, stats }) => {
        const denom = stats.cacheHitTokens + stats.cacheMissTokens;
        const rate = denom > 0 ? `${formatRate(stats.cacheHitTokens, denom)}%` : t("context.cacheNotReported");
        return (
          <span key={source}>
            {sourceLabel(source, t)}: {rate} · {t("context.sourceInput")} {formatTokenCount(stats.promptTokens)}
            {" · "}{t("context.sourceOutput")} {formatTokenCount(stats.completionTokens)}
            {" · "}{t("context.sourceRequests", { count: stats.requestCount ?? 0 })}
          </span>
        );
      })}
    </span>
  );
}

function MetricLabel({ style, icon, label }: { style: StatusBarLabelStyle; icon: ReactNode; label: string }) {
  return (
    <span className={`stat__label stat__label--${style}`} aria-hidden={style === "icon" ? "true" : undefined}>
      {style === "icon" ? icon : label}
    </span>
  );
}

function compactPath(path?: string, fallback?: string): string {
  const value = (path || fallback || "").trim();
  if (!value) return "";
  const normalized = value.replace(/\\/g, "/");
  const homeMatch = normalized.match(/^~\/?(.+)?$/);
  const parts = (homeMatch ? homeMatch[1] ?? "" : normalized).split("/").filter(Boolean);
  if (parts.length === 0) return normalized;
  if (parts.length === 1) return parts[0];
  return `…/${parts.slice(-2).join("/")}`;
}

function workspaceTooltip(t: Translator, displayPath: string, workspacePath?: string, gitBranch?: string) {
  const workspace = (workspacePath || displayPath).trim();
  const branch = (gitBranch || "").trim();
  if (branch) {
    return (
      <span className="statusbar__tooltip-stack">
        {workspace && <span>{t("status.workspaceTitle")}: {workspace}</span>}
        {branch && <span>{t("status.gitBranchTitle")}: {branch}</span>}
      </span>
    );
  }
  return `${t("status.workspaceTitle")}: ${workspace}`;
}

export function StatusBar({
  context,
  usage,
  balance,
  running,
  sessionTurns,
  sessionTokens,
  turnTokens,
  lastTurnOutputTokens,
  lastTurnModelMs,
  lastTurnOutputEstimated = false,
  lastRequestTps,
  turnCost,
  cost,
  currency,
  modelLabel,
  labelStyle = "text",
  items,
  workspacePath,
  workspaceName,
  gitBranch,
  onConnectRemote,
  onDisconnectRemote,
  onManageRemote,
  onOpenRemote,
  onOpenRemoteWorkspace,
  remoteHosts = [],
  remoteStatuses = {},
  jobs = [],
  onCancelJob,
  backgroundRuntimes = [],
  onCancelRuntimeJob,
  onRevealRuntime,
  extensionStatuses = [],
}: {
  context: ContextInfo;
  usage?: WireUsage;
  balance?: BalanceInfo;
  running: boolean;
  sessionTurns?: number;
  sessionTokens?: number;
  turnTokens?: number;
  lastTurnOutputTokens?: number;
  lastTurnModelMs?: number;
  lastTurnOutputEstimated?: boolean;
  lastRequestTps?: number | null; // Null means the latest request was not measurable.
  turnCost?: number;
  cost?: number;
  currency?: string;
  modelLabel?: string;
  labelStyle?: StatusBarLabelStyle;
  items?: readonly string[];
  workspacePath?: string;
  workspaceName?: string;
  gitBranch?: string;
  onConnectRemote?: (host: RemoteHostView) => void;
  onDisconnectRemote?: (hostId: string) => void;
  onManageRemote?: () => void;
  onOpenRemote?: (hostId: string) => void;
  onOpenRemoteWorkspace?: (host: RemoteHostView) => void;
  remoteHosts?: RemoteHostView[];
  remoteStatuses?: Record<string, RemoteConnectionStatus>;
  jobs?: JobView[];
  onCancelJob?: (jobID: string) => Promise<boolean>;
  backgroundRuntimes?: BackgroundRuntimeView[];
  onCancelRuntimeJob?: (tabID: string, jobID: string) => Promise<boolean>;
  onRevealRuntime?: (tabID: string) => Promise<void>;
  // Extension-published status surfaces (stage 8b2), one per surface key.
  extensionStatuses?: ExtensionStatusEntry[];
}) {
  const { locale, t } = useI18n();
  const pct = context.window ? Math.min(100, Math.round((context.used / context.window) * 100)) : null;
  const compactPct = context.compactRatio ? Math.round(context.compactRatio * 100) : null;
  const compactNear = pct !== null && compactPct !== null && pct >= Math.max(0, compactPct - 10);
  const compactReached = pct !== null && compactPct !== null && pct >= compactPct;
  const nowPct = nowRate(usage);
  // All-sources telemetry first; the executor-only live counters only bridge
  // the gap before the first ContextInfo refresh of a fresh session.
  const avgPct = contextAvgRate(context) ?? avgRate(usage);
  const turnEstimated = usage?.estimated === true;
  const sessionEstimated = context.estimated === true;
  const markEstimated = (value: string, estimated: boolean) => estimated && value !== "-" ? `≈${value}` : value;
  const turnCostLabel = markEstimated(formatMoneyLocalized(turnCost, currency, { locale }), turnEstimated);
  const costLabel = markEstimated(formatMoneyLocalized(cost, currency, { locale }), sessionEstimated);
  const displayWorkspacePath = (workspacePath || workspaceName || "").trim();
  const workspaceLabel = compactPath(displayWorkspacePath, workspaceName);
  const branchLabel = (gitBranch || "").trim();
  const workspaceTitle = displayWorkspacePath ? workspaceTooltip(t, displayWorkspacePath, workspacePath, branchLabel) : "";
  const turnLabel = formatTurnCount(sessionTurns, t);
  const tokenLabel = markEstimated(formatTokenCount(sessionTokens), sessionEstimated);
  const turnTokenLabel = markEstimated(formatTokenCount(turnTokens), turnEstimated);
  const balanceLabel = balance?.available && balance.display ? balance.display : "-";
  const tpsLabel = lastRequestTps === undefined
    ? formatTps(lastTurnOutputTokens && lastTurnModelMs ? lastTurnOutputTokens / (lastTurnModelMs / 1_000) : null, lastTurnOutputEstimated)
    : formatTps(lastRequestTps);
  const formatUsageToken = (value: number) => `${usage?.estimated ? "≈" : ""}${value.toLocaleString()}`;
  const outputTokensLabel = usage && typeof usage.completionTokens === "number"
    ? formatUsageToken(usage.completionTokens)
    : "-";
  const cacheHit = usage?.cacheHitTokens;
  const cacheMiss = usage?.cacheMissTokens;
  const hasCacheTokens = typeof cacheHit === "number" || typeof cacheMiss === "number";
  const metricLabelStyle = labelStyle === "text" ? "text" : "icon";
  const visibleItems = normalizeStatusBarItems(items);
  const cacheTooltip = sourceCacheTooltip(t, t("status.cacheTitle"), context);
  const avgCacheTooltip = sourceCacheTooltip(t, t("status.cacheAvgTitle"), context);
  const itemRenderers: Record<StatusBarItemId, ReactNode> = {
    model: (
      <Tooltip label={t("status.modelTitle")}>
        <span className="stat stat--model">
          <span className={`statusbar__dot ${running ? "statusbar__dot--busy" : ""}`} />
          {modelLabel && <span className="statusbar__model">{modelLabel}</span>}
        </span>
      </Tooltip>
    ),
    workspace: workspaceLabel ? (
      <Tooltip label={workspaceTitle} className="statusbar__metric statusbar__metric--workspace">
        <span className="stat statusbar__workspace">
          <span className="stat__label stat__label--icon" aria-hidden="true"><Folder size={12} /></span>
          <b>{workspaceLabel}</b>
        </span>
      </Tooltip>
    ) : null,
    git_branch: branchLabel ? (
      <Tooltip label={`${t("status.gitBranchTitle")}: ${branchLabel}`} className="statusbar__metric statusbar__metric--branch">
        <span className="stat statusbar__branch">
          <span className="stat__label stat__label--icon" aria-hidden="true"><GitBranch size={12} /></span>
          <b>{branchLabel}</b>
        </span>
      </Tooltip>
    ) : null,
    cache: (
      <Tooltip label={cacheTooltip} className="statusbar__metric statusbar__metric--cache">
        <span className="stat statusbar__cache">
          <MetricLabel style={metricLabelStyle} icon={<Percent size={12} />} label={t("status.cacheLabel")} />
          <b className={rateValueClass(nowPct) || undefined}>{nowPct !== null ? `${nowPct}%` : "-"}</b>
        </span>
      </Tooltip>
    ),
    cache_avg: (
      <Tooltip label={avgCacheTooltip} className="statusbar__metric statusbar__metric--avg">
        <span className="stat statusbar__avg">
          <MetricLabel style={metricLabelStyle} icon={<Activity size={12} />} label={t("status.cacheAvgLabel")} />
          <b className={rateValueClass(avgPct) || undefined}>{avgPct !== null ? `${avgPct}%` : "-"}</b>
        </span>
      </Tooltip>
    ),
    session_tokens: (
      <Tooltip label={t("status.sessionTokensTitle")} className="statusbar__metric statusbar__metric--tokens">
        <span className="stat statusbar__tokens">
          <MetricLabel style={metricLabelStyle} icon={<Database size={12} />} label={t("status.sessionTokensLabel")} />
          <b className={tokenLabel === "-" ? "stat__value--empty" : undefined}>{tokenLabel}</b>
        </span>
      </Tooltip>
    ),
    turn_tokens: (
      <Tooltip label={t("status.turnTokensTitle")} className="statusbar__metric statusbar__metric--turn-tokens">
        <span className="stat statusbar__turn-tokens">
          <MetricLabel style={metricLabelStyle} icon={<Zap size={12} />} label={t("status.turnTokensLabel")} />
          <b className={turnTokenLabel === "-" ? "stat__value--empty" : undefined}>{turnTokenLabel}</b>
        </span>
      </Tooltip>
    ),
    turn_cost: (
      <Tooltip label={t("status.turnCostTitle")} className="statusbar__metric statusbar__metric--turn-cost">
        <span className="stat statusbar__turn-cost">
          <MetricLabel style={metricLabelStyle} icon={<CircleDollarSign size={12} />} label={t("status.turnCostLabel")} />
          <b>{turnCostLabel}</b>
        </span>
      </Tooltip>
    ),
    session_turns: (
      <Tooltip label={t("status.sessionTurnsTitle")} className="statusbar__metric statusbar__metric--turns">
        <span className="stat statusbar__turns">
          <MetricLabel style={metricLabelStyle} icon={<RefreshCw size={12} />} label={t("status.sessionTurnsLabel")} />
          <b className={turnLabel === "-" ? "stat__value--empty" : undefined}>{turnLabel}</b>
        </span>
      </Tooltip>
    ),
    turn_tps: (
      <Tooltip label={t("status.tpsTitle")} className="statusbar__metric statusbar__metric--tps">
        <span className="stat statusbar__tps">
          <MetricLabel style={metricLabelStyle} icon={<Gauge size={12} />} label={t("status.tpsLabel")} />
          <b className={tpsLabel === null ? "stat__value--empty" : undefined}>{tpsLabel ?? "-"}</b>
        </span>
      </Tooltip>
    ),
    turn_output_tokens: (
      <Tooltip label={t("status.outputTokensTitle")} className="statusbar__metric statusbar__metric--output-tokens">
        <span className="stat statusbar__output-tokens">
          <MetricLabel style={metricLabelStyle} icon={<FileOutput size={12} />} label={t("status.outputTokensLabel")} />
          <b className={outputTokensLabel === "-" ? "stat__value--empty" : undefined}>{outputTokensLabel}</b>
        </span>
      </Tooltip>
    ),
    turn_cache_tokens: (
      <Tooltip label={t("status.cacheTokensTitle")} className="statusbar__metric statusbar__metric--cache-tokens">
        <span className="stat statusbar__cache-tokens">
          <MetricLabel style={metricLabelStyle} icon={<HardDrive size={12} />} label={t("status.cacheTokensLabel")} />
          {hasCacheTokens ? (
            <>
              <span>{t("status.cacheHitShort")} </span><b>{formatUsageToken(typeof cacheHit === "number" && cacheHit > 0 ? cacheHit : 0)}</b>
              <span className="statusbar__cache-sep">|</span>
              <span>{t("status.cacheMissShort")} </span><b>{formatUsageToken(typeof cacheMiss === "number" && cacheMiss > 0 ? cacheMiss : 0)}</b>
            </>
          ) : (
            <b className="stat__value--empty">-</b>
          )}
        </span>
      </Tooltip>
    ),
    context: (
      <Tooltip label={t("status.ctxTitle")} className="statusbar__metric statusbar__metric--ctx">
        <span className="stat statusbar__ctx">
          <MetricLabel style={metricLabelStyle} icon={<CircleGauge size={12} />} label={t("status.ctxLabel")} />
          <b className={pct === null ? "stat__value--empty" : undefined}>{pct !== null ? `${pct}%` : "-"}</b>
        </span>
      </Tooltip>
    ),
    compact: (
      <Tooltip label={t("status.compactTitle")} className="statusbar__metric statusbar__metric--compact">
        <span className="stat statusbar__compact">
          <MetricLabel style={metricLabelStyle} icon={<Layers size={12} />} label={t("status.compactLabel")} />
          <b
            className={[
              compactPct === null ? "stat__value--empty" : undefined,
              compactReached ? "statusbar__compact-value--critical" : compactNear ? "statusbar__compact-value--warn" : undefined,
            ].filter(Boolean).join(" ") || undefined}
          >
            {compactPct !== null ? `${compactPct}%` : "-"}
          </b>
        </span>
      </Tooltip>
    ),
    cost: (
      <Tooltip label={t("status.spendTitle")} className="statusbar__metric statusbar__metric--cost">
        <span className="stat statusbar__cost">
          <MetricLabel style={metricLabelStyle} icon={<CircleDollarSign size={12} />} label={t("status.costLabel")} />
          <b>{costLabel}</b>
        </span>
      </Tooltip>
    ),
    balance: (
      <Tooltip label={t("status.balanceTitle")} className="statusbar__metric statusbar__metric--balance">
        <span className="stat stat--balance statusbar__balance">
          <MetricLabel style={metricLabelStyle} icon={<Wallet size={12} />} label={t("status.balanceLabel")} />
          <b className={balanceLabel === "-" ? "stat__value--empty" : undefined}>{balanceLabel}</b>
        </span>
      </Tooltip>
    ),
  };
  const renderedItems = visibleItems
    .map((id) => ({ id, node: itemRenderers[id] }))
    .filter(({ node }) => node !== null && node !== undefined && node !== false);
  const statusbarRef = useRef<HTMLDivElement>(null);
  // React's onWheel is a passive listener, so preventDefault() there would be
  // a no-op (plus a console warning). Register a native non-passive listener
  // instead so wheel-panning the status bar also stops page scroll.
  useEffect(() => {
    const el = statusbarRef.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      if (el.scrollWidth <= el.clientWidth) return;
      e.preventDefault();
      el.scrollLeft += e.deltaY;
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, []);
  return (
    <div
      className={`statusbar statusbar--${metricLabelStyle}`}
      ref={statusbarRef}
    >
      <div className="statusbar__group statusbar__group--items">
        <RemoteStatusBarChip
          hosts={remoteHosts}
          statuses={remoteStatuses}
          onOpen={onOpenRemote}
          onOpenWorkspace={onOpenRemoteWorkspace}
          onConnect={onConnectRemote}
          onDisconnect={onDisconnectRemote}
          onManage={onManageRemote}
        />
        <JobsStatusBarChip
          jobs={jobs}
          activeJobsRemote={false}
          onCancelJob={onCancelJob}
          runtimes={backgroundRuntimes}
          onCancelRuntimeJob={onCancelRuntimeJob}
          onRevealRuntime={onRevealRuntime}
        />
        <ExtensionStatusBarChips statuses={extensionStatuses} />
        {renderedItems.map(({ id, node }) => (
          <span className="statusbar__item" data-statusbar-item={id} key={id}>
            {node}
          </span>
        ))}
      </div>
    </div>
  );
}

// ExtensionStatusBarChips renders extension-published status surfaces next to
// the built-in chips. A surface persists until the owning sidecar replaces it
// (same surface key) or the runtime rebuilds; severity drives the accent color.
function ExtensionStatusBarChips({ statuses }: { statuses: ExtensionStatusEntry[] }) {
  const { t } = useI18n();
  if (statuses.length === 0) return null;
  return (
    <>
      {statuses.map((status) => {
        const severity = status.severity === "error" ? "error" : status.severity === "warn" ? "warn" : "info";
        const pct = typeof status.progress === "number" ? Math.round(Math.max(0, Math.min(1, status.progress)) * 100) : undefined;
        return (
          <span className="statusbar__item" data-statusbar-item="extension" key={`${status.pluginId}:${status.surfaceId}`}>
            <Tooltip
              label={
                <span className="statusbar__tooltip-stack">
                  <span>{t("status.extensionTitle")}: {status.pluginId}</span>
                  {status.detail ? <span>{status.detail}</span> : null}
                  {pct !== undefined ? <span>{t("ext.card.progress")}: {pct}%</span> : null}
                </span>
              }
            >
              <span className={`stat statusbar__extension statusbar__extension--${severity}`}>
                <Puzzle size={12} aria-hidden="true" />
                <span className="statusbar__extension-label">{status.label}</span>
                {pct !== undefined ? <b>{pct}%</b> : null}
              </span>
            </Tooltip>
          </span>
        );
      })}
    </>
  );
}

function JobsStatusBarChip({
  jobs,
  activeJobsRemote,
  onCancelJob,
  runtimes,
  onCancelRuntimeJob,
  onRevealRuntime,
}: {
  jobs: JobView[];
  activeJobsRemote: boolean;
  onCancelJob?: (jobID: string) => Promise<boolean>;
  runtimes: BackgroundRuntimeView[];
  onCancelRuntimeJob?: (tabID: string, jobID: string) => Promise<boolean>;
  onRevealRuntime?: (tabID: string) => Promise<void>;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [stopping, setStopping] = useState<Set<string>>(() => new Set());
  const triggerRef = useRef<HTMLButtonElement>(null);
  const groups = runtimes.filter((runtime) => runtime.running || runtime.pendingPrompt || runtime.jobs.length > 0);
  // BackgroundRuntimes is process-local, while jobs from the active controller
  // snapshot may come from another runtime. Keep both sources visible.
  if (jobs.length > 0 && (activeJobsRemote || !groups.some((runtime) => runtime.jobs.length > 0))) {
    groups.push({ tabId: "", title: "", detached: false, running: false, pendingPrompt: false, jobs });
  }
  const totalActivity = groups.reduce(
    (total, runtime) => total + Math.max(1, runtime.jobs.length),
    0,
  );

  useEffect(() => {
    if (totalActivity === 0) setOpen(false);
  }, [totalActivity]);
  if (totalActivity === 0) return null;

  const stop = async (tabID: string, jobID: string) => {
    const key = `${tabID}:${jobID}`;
    const handler = tabID ? onCancelRuntimeJob : onCancelJob;
    if (!handler || stopping.has(key)) return;
    setStopping((current) => new Set(current).add(key));
    try {
      if (tabID && onCancelRuntimeJob) await onCancelRuntimeJob(tabID, jobID);
      else if (onCancelJob) await onCancelJob(jobID);
    } finally {
      setStopping((current) => {
        const next = new Set(current);
        next.delete(key);
        return next;
      });
    }
  };

  return (
    <span className="statusbar__jobs">
      <button
        ref={triggerRef}
        type="button"
        className="statusbar__jobs-trigger"
        aria-label={`${t("status.jobsTitle")}: ${t("status.jobs", { n: totalActivity })}`}
        aria-expanded={open}
        aria-haspopup="dialog"
        title={t("status.jobsTitle")}
        onClick={() => setOpen((value) => !value)}
      >
        <Activity size={12} aria-hidden="true" />
        <b>{totalActivity}</b>
      </button>
      <AnchoredPopover open={open} anchorRef={triggerRef} onClose={() => setOpen(false)} className="jobs-popover" align="start">
        <section role="dialog" aria-label={t("status.jobsTitle")}>
          <header className="jobs-popover__header">{t("status.jobsTitle")}</header>
          <div className="jobs-popover__list">
            {groups.map((runtime) => (
              <div className="jobs-popover__runtime" key={runtime.tabId || "active"}>
                {runtime.tabId && (
                  <div className="jobs-popover__runtime-header">
                    <strong>{runtime.title || t("runtime.unknownTask")}</strong>
                    {onRevealRuntime && (
                      <button type="button" className="btn btn--small" onClick={() => void onRevealRuntime(runtime.tabId)}>
                        {t("status.jobOpenTask")}
                      </button>
                    )}
                  </div>
                )}
                {runtime.jobs.length === 0 && (
                  <div className="jobs-popover__job">
                    <span className="jobs-popover__copy">
                      <strong>{runtime.pendingPrompt ? t("status.runtimePendingPrompt") : t("status.runtimeRunning")}</strong>
                    </span>
                  </div>
                )}
                {runtime.jobs.map((job) => {
                  const pending = stopping.has(`${runtime.tabId}:${job.id}`);
                  const canStop = runtime.tabId ? Boolean(onCancelRuntimeJob) : Boolean(onCancelJob);
                  return (
                    <div className="jobs-popover__job" key={`${runtime.tabId}:${job.id}`}>
                      <span className="jobs-popover__copy">
                        <strong>{job.label || job.kind}</strong>
                        <small>{job.kind} · {job.status}</small>
                      </span>
                      <button
                        type="button"
                        className="btn btn--small jobs-popover__stop"
                        disabled={pending || !canStop}
                        onClick={() => void stop(runtime.tabId, job.id)}
                      >
                        <Square size={11} aria-hidden="true" />
                        {pending ? t("status.jobStopping") : t("status.jobStop")}
                      </button>
                    </div>
                  );
                })}
              </div>
            ))}
          </div>
        </section>
      </AnchoredPopover>
    </span>
  );
}

// This entry remains visible whenever an SSH host is configured. The popover
// owns quick connection actions; remote files and services live in the dock.
const REMOTE_STATE_SEVERITY: Record<string, number> = {
  error: 5,
  reconnecting: 4,
  pending_hostkey: 4,
  pending_secret: 4,
  connecting: 3,
  degraded: 2,
  connected: 1,
  stopped: 0,
};

function RemoteStatusBarChip({
  hosts,
  statuses,
  onOpen,
  onOpenWorkspace,
  onConnect,
  onDisconnect,
  onManage,
}: {
  hosts: RemoteHostView[];
  statuses: Record<string, RemoteConnectionStatus>;
  onOpen?: (hostId: string) => void;
  onOpenWorkspace?: (host: RemoteHostView) => void;
  onConnect?: (host: RemoteHostView) => void;
  onDisconnect?: (hostId: string) => void;
  onManage?: () => void;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [detailHostId, setDetailHostId] = useState<string | null>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const revealRequest = useRemoteStore((state) => state.statusPopoverRequest);
  const clearRevealRequest = useRemoteStore((state) => state.clearStatusPopoverRequest);

  useEffect(() => {
    if (!revealRequest || !hosts.some((host) => host.id === revealRequest.hostId)) return;
    setOpen(true);
    clearRevealRequest(revealRequest);
  }, [clearRevealRequest, hosts, revealRequest]);

  if (hosts.length === 0) return null;

  const entries = hosts.map((host) => statuses[host.id] ?? { hostId: host.id, state: "stopped" as const });
  const worst = entries.reduce((a, b) => {
    const aSeverity = isRemoteTerminalFailure(a) ? 6 : REMOTE_STATE_SEVERITY[a.state] ?? 0;
    const bSeverity = isRemoteTerminalFailure(b) ? 6 : REMOTE_STATE_SEVERITY[b.state] ?? 0;
    return bSeverity > aSeverity ? b : a;
  });
  const worstHost = hosts.find((host) => host.id === worst.hostId) ?? hosts[0];
  const triggerState = isRemoteTerminalFailure(worst) ? "error" : worst.state;
  const triggerStatus = isRemoteTerminalFailure(worst) ? t("remote.status.failed") : t(`remote.status.${worst.state}`);
  const idleDisconnected = worst.state === "stopped" && !worst.error;
  const triggerLabel = idleDisconnected ? t("remote.statusBar.disconnected") : t("remote.statusBar.summary", { host: worstHost.label, status: triggerStatus });
  const triggerText = idleDisconnected ? "SSH" : triggerState === "connected" ? worstHost.label : triggerLabel;

  return (
    <span className="statusbar__remote-wrap">
      <button
        ref={triggerRef}
        type="button"
        className={`statusbar__remote remote-chip remote-chip--${triggerState}${idleDisconnected ? " statusbar__remote--idle" : ""}`}
        onClick={() => setOpen((value) => !value)}
        aria-label={triggerLabel}
        aria-haspopup="dialog"
        aria-expanded={open}
        title={triggerLabel}
      >
        {triggerState === "connected" ? <span className="statusbar__remote-state-dot" aria-hidden="true" /> : <Server size={11} aria-hidden="true" />}
        <span className="statusbar__remote-label">{triggerText}</span>
      </button>
      <AnchoredPopover
        open={open}
        anchorRef={triggerRef}
        onClose={() => setOpen(false)}
        className="remote-switcher"
        align="start"
      >
        <section role="dialog" aria-label={t("remote.switcher.title")}>
          <header className="remote-switcher__header">{t("remote.switcher.title")}</header>
          <div className="remote-switcher__section-label">{t("remote.switcher.hosts")}</div>
          <div className="remote-switcher__hosts">
            {hosts.map((host) => {
              const status = statuses[host.id] ?? { hostId: host.id, state: "stopped" as const };
              const connected = status.state === "connected" || status.state === "degraded";
              const busy = status.state === "connecting" || status.state === "reconnecting" || status.state === "pending_hostkey" || status.state === "pending_secret";
              const terminalFailure = isRemoteTerminalFailure(status);
              const degradedWarning = isRemoteDegradedWarning(status);
              const stateClass = terminalFailure ? "error" : status.state;
              const stateLabel = terminalFailure ? t("remote.status.failed") : t(`remote.status.${status.state}`);
              const errorSummary = status.error ? t(remoteConnectionErrorSummaryKey(status), { host: host.label }) : "";
              const target = `${host.user ? `${host.user}@` : ""}${host.host}${host.port && host.port !== 22 ? `:${host.port}` : ""}`;
              return (
                <div className={`remote-switcher__host remote-switcher__host--${stateClass}`} key={host.id}>
                  <button
                    type="button"
                    className="remote-switcher__host-main"
                    onClick={() => {
                      setOpen(false);
                      onOpen?.(host.id);
                    }}
                  >
                    <span className={`remote-switcher__state remote-switcher__state--${stateClass}`} aria-hidden="true" />
                    <span className="remote-switcher__copy">
                      <strong>{host.label}</strong>
                      <small>{stateLabel} · {host.defaultWorkspace || target}</small>
                    </span>
                  </button>
                    <span className="remote-switcher__actions">
                    <button
                      type="button"
                      className="btn btn--small btn--primary"
                      disabled={busy}
                      onClick={() => {
                        if (connected) {
                          setOpen(false);
                          onOpenWorkspace?.(host);
                        } else {
                          setOpen(false);
                          onConnect?.(host);
                        }
                      }}
                    >
                      {connected ? t("remote.openWorkspace") : busy ? stateLabel : terminalFailure ? t("remote.error.retry") : t("remote.connectAndOpen")}
                    </button>
                    {connected && (
                      <button
                        type="button"
                        className="remote-switcher__disconnect"
                        onClick={() => onDisconnect?.(host.id)}
                        aria-label={t("remote.disconnectHost", { host: host.label })}
                        title={t("remote.disconnect")}
                      >
                        <Unplug size={13} aria-hidden="true" />
                      </button>
                    )}
                  </span>
                  {(terminalFailure || degradedWarning) && (
                    <div className={`remote-switcher__error-card ${degradedWarning ? "remote-switcher__error-card--warning" : ""}`} role="alert">
                      <strong>{t(degradedWarning ? "remote.status.degraded" : "remote.status.failed")}</strong>
                      <span>{errorSummary}</span>
                      <div className="remote-switcher__error-actions">
                        <button
                          type="button"
                          className="btn btn--small"
                          onClick={() => {
                            setOpen(false);
                            setDetailHostId(host.id);
                          }}
                        >
                          {t(isRemoteHostKeyMismatch(status) ? "remote.error.hostKeyDetails" : "remote.error.details")}
                        </button>
                        <button
                          type="button"
                          className="btn btn--small"
                          onClick={() => {
                            setOpen(false);
                            onManage?.();
                          }}
                        >
                          {t("remote.error.manage")}
                        </button>
                      </div>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
          <button
            type="button"
            className="remote-switcher__manage"
            onClick={() => {
              setOpen(false);
              onManage?.();
            }}
          >
            <Settings size={13} aria-hidden="true" />
            {t("remote.switcher.manage")}
          </button>
        </section>
      </AnchoredPopover>
      {detailHostId && (() => {
        const host = hosts.find((item) => item.id === detailHostId);
        const status = statuses[detailHostId];
        if (!host || !status?.error) return null;
        return (
          <RemoteConnectionErrorDialog
            host={host}
            status={status}
            onClose={() => setDetailHostId(null)}
            onManage={onManage}
            onRetry={() => onConnect?.(host)}
          />
        );
      })()}
    </span>
  );
}
