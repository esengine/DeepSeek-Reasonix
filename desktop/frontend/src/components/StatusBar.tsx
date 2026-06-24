import { useEffect, useRef, useState, type ReactNode } from "react";
import { Activity, CircleDollarSign, CircleGauge, Database, Folder, GitBranch, Layers, Percent, RefreshCw, Wallet, Zap } from "lucide-react";
import { Tooltip } from "./Tooltip";
import { useI18n, type Translator } from "../lib/i18n";
import { formatMoneyLocalized } from "../lib/money";
import { normalizeStatusBarItems, type StatusBarItemId } from "../lib/statusBarItems";
import { type BalanceInfo, type CollaborationMode, type ContextInfo, type JobView, type ToolApprovalMode, type WireUsage } from "../lib/types";

type StatusBarLabelStyle = "icon" | "text";

// JobsChip is the status-bar background-jobs indicator: a count that opens an
// upward popover listing the running jobs (id · label · status), mirroring the
// ModelSwitcher's click-to-open pattern. With no jobs it stays hidden so the
// high-priority status metrics keep the compact left-to-right scan.
function JobsChip({ jobs }: { jobs: JobView[] }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const closeOnOutsideClick = (event: MouseEvent) => {
      const target = event.target;
      if (!(target instanceof Node)) return;
      if (wrapRef.current?.contains(target)) return;
      setOpen(false);
    };
    document.addEventListener("click", closeOnOutsideClick);
    return () => document.removeEventListener("click", closeOnOutsideClick);
  }, [open]);
  if (jobs.length === 0) {
    return null;
  }
  return (
    <div className="statusbar__jobswrap" ref={wrapRef}>
      <Tooltip label={t("status.jobsTitle")}>
        <button className="stat stat--jobs statusbar__jobs" onClick={() => setOpen((v) => !v)}>
          <span className="stat__label">{t("status.jobsLabel")}</span>
          <b>{jobs.length}</b>
        </button>
      </Tooltip>
      {open && (
        <div className="modelsw__menu jobsmenu" role="listbox">
          <div className="jobsmenu__head">{t("status.jobsTitle")}</div>
          {jobs.map((j) => (
            <div className="jobsmenu__item" key={j.id} role="option">
              <span className="jobsmenu__id">{j.id}</span>
              <span className="jobsmenu__label">{j.label || j.kind}</span>
              <span className="jobsmenu__status">{j.status}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function formatRate(hit: number, denom: number): string | null {
  if (denom <= 0) return null;
  return ((hit / denom) * 100).toFixed(2);
}

// nowRate is the SINGLE-TURN prompt cache-hit % (latest turn) — the higher,
// steeper number on a non-compacting DeepSeek session. null when nothing yet.
function nowRate(u?: WireUsage): string | null {
  if (!u) return null;
  let denom = u.cacheHitTokens + u.cacheMissTokens;
  if (denom === 0) denom = u.promptTokens;
  return formatRate(u.cacheHitTokens, denom);
}

// avgRate is the SESSION-AGGREGATE cache-hit % — Σhit/Σ(hit+miss) across every
// turn — the steadier, cost-oriented number that matches the legacy dashboard.
// On a non-compacting DeepSeek session it trails nowRate (early cold-start turns
// drag the average down); it overtakes only when compaction craters single turns.
function avgRate(u?: WireUsage): string | null {
  if (!u) return null;
  const denom = u.sessionCacheHitTokens + u.sessionCacheMissTokens;
  return formatRate(u.sessionCacheHitTokens, denom);
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
  jobs,
  running,
  collaborationMode,
  toolApprovalMode,
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
  hydrationLabel,
}: {
  context: ContextInfo;
  usage?: WireUsage;
  balance?: BalanceInfo;
  jobs?: JobView[];
  running: boolean;
  collaborationMode: CollaborationMode;
  toolApprovalMode: ToolApprovalMode;
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
  hydrationLabel?: string;
}) {
  const { locale, t } = useI18n();
  const pct = context.window ? Math.min(100, Math.round((context.used / context.window) * 100)) : null;
  const compactPct = context.compactRatio ? Math.round(context.compactRatio * 100) : null;
  const compactNear = pct !== null && compactPct !== null && pct >= Math.max(0, compactPct - 10);
  const compactReached = pct !== null && compactPct !== null && pct >= compactPct;
  const nowPct = nowRate(usage);
  const avgPct = avgRate(usage);
  const jobsList = jobs ?? [];
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
  const planMode = collaborationMode === "plan";
  const goalMode = collaborationMode === "goal";
  const metricLabelStyle = labelStyle === "text" ? "text" : "icon";
  const visibleItems = normalizeStatusBarItems(items);
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
      <Tooltip label={t("status.cacheTitle")} className="statusbar__metric statusbar__metric--cache">
        <span className="stat statusbar__cache">
          <MetricLabel style={metricLabelStyle} icon={<Percent size={12} />} label={t("status.cacheLabel")} />
          <b className={rateValueClass(nowPct) || undefined}>{nowPct !== null ? `${nowPct}%` : "-"}</b>
        </span>
      </Tooltip>
    ),
    cache_avg: (
      <Tooltip label={t("status.cacheAvgTitle")} className="statusbar__metric statusbar__metric--avg">
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
  const modeIndicators = [
    planMode ? <span className="statusbar__plan" key="plan">{t("status.plan")}</span> : null,
    goalMode ? <span className="statusbar__plan" key="goal">{t("composer.goalMode")}</span> : null,
    toolApprovalMode === "auto" ? (
      <Tooltip label={t("composer.accessAutoTitle")} key="auto">
        <span className="statusbar__yolo">{t("composer.accessAuto")}</span>
      </Tooltip>
    ) : null,
    toolApprovalMode === "yolo" ? (
      <Tooltip label={t("status.yoloTitle")} key="yolo">
        <span className="statusbar__yolo">{t("composer.accessYolo")}</span>
      </Tooltip>
    ) : null,
  ].filter(Boolean);

  return (
    <div className={`statusbar statusbar--${metricLabelStyle}`}>
      {hydrationLabel && (
        <div className="statusbar__group statusbar__group--hydrate">
          <span className="stat statusbar__hydrate" role="status">
            <RefreshCw size={12} />
            <span>{hydrationLabel}</span>
          </span>
        </div>
      )}
      <div className="statusbar__group statusbar__group--items">
        {renderedItems.map(({ id, node }) => (
          <span className="statusbar__item" data-statusbar-item={id} key={id}>
            {node}
          </span>
        ))}
      </div>
      {modeIndicators.length > 0 && <div className="statusbar__group statusbar__group--modes">{modeIndicators}</div>}
      {jobsList.length > 0 && (
        <div className="statusbar__group statusbar__group--jobs">
          <JobsChip jobs={jobsList} />
        </div>
      )}
    </div>
  );
}
