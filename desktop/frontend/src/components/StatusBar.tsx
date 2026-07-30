import { useEffect, useRef, useState, type ReactNode } from "react";
import { Activity, Check, ChevronsUpDown, CircleDollarSign, CircleGauge, Database, Folder, GitBranch, Laptop, Layers, Percent, RefreshCw, Server, Settings, Unplug, Wallet, Zap } from "lucide-react";
import { AnchoredPopover } from "./AnchoredPopover";
import { RemoteConnectionErrorDialog } from "./RemoteConnectionErrorDialog";
import { Tooltip } from "./Tooltip";
import { useI18n, type Translator } from "../lib/i18n";
import { formatMoneyLocalized } from "../lib/money";
import { normalizeStatusBarItems, type StatusBarItemId } from "../lib/statusBarItems";
import { isRemoteDegradedWarning, isRemoteHostKeyMismatch, isRemoteTerminalFailure, remoteConnectionErrorSummaryKey } from "../lib/remoteErrors";
import { type BalanceInfo, type ContextInfo, type RemoteConnectionStatus, type RemoteHostView, type UsageSourceStats, type WireUsage } from "../lib/types";
import { useRemoteStore } from "../store/remote";
import type { WorkbenchActiveTarget } from "../lib/workbenchTarget";

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
  workbenchTarget,
  onSwitchLocal,
}: {
  context: ContextInfo;
  usage?: WireUsage;
  balance?: BalanceInfo;
  running: boolean;
  sessionTurns?: number;
  sessionTokens?: number;
  turnTokens?: number;
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
  workbenchTarget?: WorkbenchActiveTarget;
  onSwitchLocal?: () => void;
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
  const turnCostLabel = formatMoneyLocalized(turnCost, currency, { locale });
  const costLabel = formatMoneyLocalized(cost, currency, { locale });
  const displayWorkspacePath = (workspacePath || workspaceName || "").trim();
  const workspaceLabel = compactPath(displayWorkspacePath, workspaceName);
  const branchLabel = (gitBranch || "").trim();
  const workspaceTitle = displayWorkspacePath ? workspaceTooltip(t, displayWorkspacePath, workspacePath, branchLabel) : "";
  const turnLabel = formatTurnCount(sessionTurns, t);
  const tokenLabel = formatTokenCount(sessionTokens);
  const turnTokenLabel = formatTokenCount(turnTokens);
  const balanceLabel = balance?.available && balance.display ? balance.display : "-";
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
  return (
    <div className={`statusbar statusbar--${metricLabelStyle}`}>
      <div className="statusbar__group statusbar__group--items">
        <RemoteStatusBarChip
          hosts={remoteHosts}
          statuses={remoteStatuses}
          onOpen={onOpenRemote}
          onOpenWorkspace={onOpenRemoteWorkspace}
          onConnect={onConnectRemote}
          onDisconnect={onDisconnectRemote}
          onManage={onManageRemote}
          workbenchTarget={workbenchTarget}
          onSwitchLocal={onSwitchLocal}
        />
        {renderedItems.map(({ id, node }) => (
          <span className="statusbar__item" data-statusbar-item={id} key={id}>
            {node}
          </span>
        ))}
      </div>
    </div>
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
  workbenchTarget,
  onSwitchLocal,
}: {
  hosts: RemoteHostView[];
  statuses: Record<string, RemoteConnectionStatus>;
  onOpen?: (hostId: string) => void;
  onOpenWorkspace?: (host: RemoteHostView) => void;
  onConnect?: (host: RemoteHostView) => void;
  onDisconnect?: (hostId: string) => void;
  onManage?: () => void;
  workbenchTarget?: WorkbenchActiveTarget;
  onSwitchLocal?: () => void;
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
  const activeRemoteHost = workbenchTarget?.kind === "ssh"
    ? hosts.find((host) => host.id === workbenchTarget.hostId)
    : undefined;
  const triggerLabel = activeRemoteHost
    ? t("remote.statusBar.activeWorkspace", { host: activeRemoteHost.label, workspace: compactPath(workbenchTarget?.workspace) })
    : worst.state === "stopped" && !worst.error
    ? t("remote.statusBar.disconnected")
    : t("remote.statusBar.summary", { host: worstHost.label, status: triggerStatus });

  return (
    <span className="statusbar__remote-wrap">
      <button
        ref={triggerRef}
        type="button"
        className={`statusbar__remote remote-chip remote-chip--${triggerState}`}
        onClick={() => setOpen((value) => !value)}
        aria-label={triggerLabel}
        aria-haspopup="dialog"
        aria-expanded={open}
        title={triggerLabel}
      >
        <Server size={11} aria-hidden="true" />
        <span>{triggerLabel}</span>
        <ChevronsUpDown size={10} aria-hidden="true" />
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
          <button
            type="button"
            className="remote-switcher__local"
            aria-current={workbenchTarget?.kind !== "ssh" ? "true" : undefined}
            onClick={() => {
              if (workbenchTarget?.kind !== "ssh") return;
              setOpen(false);
              onSwitchLocal?.();
            }}
          >
            <span className="remote-switcher__icon"><Laptop size={14} aria-hidden="true" /></span>
            <span className="remote-switcher__copy">
              <strong>{t("remote.switcher.local")}</strong>
              <small>{t("remote.switcher.currentSession")}</small>
            </span>
            {workbenchTarget?.kind !== "ssh" && <Check size={14} className="remote-switcher__check" aria-hidden="true" />}
          </button>
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
                    {workbenchTarget?.kind === "ssh" && workbenchTarget.hostId === host.id && (
                      <Check size={14} className="remote-switcher__check" aria-label={t("remote.switcher.currentSession")} />
                    )}
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
