import { useEffect, useRef, useState } from "react";
import { Tooltip } from "./Tooltip";
import { useI18n } from "../lib/i18n";
import { type BalanceInfo, type CollaborationMode, type ContextInfo, type JobView, type ToolApprovalMode, type WireUsage } from "../lib/types";

// JobsChip is the status-bar background-jobs indicator: a count that opens an
// upward popover listing the running jobs (id · label · status), mirroring the
// ModelSwitcher's click-to-open pattern. With no jobs it still reserves a stable
// "任务 0" slot so the IDE-style status order does not jump.
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
    return (
      <span className="stat stat--jobs">
        <span className="stat__label">{t("status.jobsLabel")}</span>
        <b>-</b>
      </span>
    );
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

function currencySymbol(currency?: string): string {
  const value = (currency || "¥").trim();
  if (/^(cny|rmb|yuan)$/i.test(value)) return "¥";
  if (/^(usd|dollar)$/i.test(value)) return "$";
  return value || "¥";
}

function formatMoney(amount?: number, currency?: string): string {
  const symbol = currencySymbol(currency);
  if (typeof amount !== "number" || amount <= 0) return `${symbol}0.0000`;
  return `${symbol}${amount < 1 ? amount.toFixed(4) : amount.toFixed(2)}`;
}

export function StatusBar({
  context,
  usage,
  balance,
  jobs,
  running,
  collaborationMode,
  toolApprovalMode,
  cost,
  currency,
  modelLabel,
  currentTurnCount,
}: {
  context: ContextInfo;
  usage?: WireUsage;
  balance?: BalanceInfo;
  jobs?: JobView[];
  running: boolean;
  collaborationMode: CollaborationMode;
  toolApprovalMode: ToolApprovalMode;
  cost?: number;
  currency?: string;
  modelLabel?: string;
  currentTurnCount?: number;
}) {
  const { t } = useI18n();
  const pct = context.window ? Math.min(100, Math.round((context.used / context.window) * 100)) : null;
  const compactPct = context.compactRatio ? Math.round(context.compactRatio * 100) : null;
  const nowPct = nowRate(usage);
  const avgPct = avgRate(usage);
  const jobsList = jobs ?? [];
  const costLabel = formatMoney(cost, currency);
  const balanceLabel = balance?.available && balance.display ? balance.display : "-";
  const planMode = collaborationMode === "plan";
  const goalMode = collaborationMode === "goal";

  return (
    <div className="statusbar">
      <span className="stat stat--model">
        <span className={`statusbar__dot ${running ? "statusbar__dot--busy" : ""}`} />
        {modelLabel && <span className="statusbar__model">{modelLabel}</span>}
      </span>
      {typeof currentTurnCount === "number" && currentTurnCount > 0 && (
        <span className="stat statusbar__turns" title={t("status.sessionTurnsTitle")}>
          <span className="stat__label">{t("status.sessionTurnsLabel")}</span>
          <b>{t(currentTurnCount === 1 ? "history.turnOne" : "history.turnOther", { n: currentTurnCount })}</b>
        </span>
      )}
      <span className="stat statusbar__ctx">
        <span className="stat__label">{t("status.ctxLabel")}</span>
        <b>{pct !== null ? `${pct}%` : "-"}</b>
      </span>
      <span className="stat statusbar__compact">
        <span className="stat__label">{t("status.compactLabel")}</span>
        <b>{compactPct !== null ? `${compactPct}%` : "-"}</b>
      </span>
      <span className="stat statusbar__cache">
        <span className="stat__label">{t("status.cacheLabel")}</span>
        <b>{nowPct !== null ? `${nowPct}%` : "-"}</b>
      </span>
      <span className="stat statusbar__avg">
        <span className="stat__label">{t("status.cacheAvgLabel")}</span>
        <b>{avgPct !== null ? `${avgPct}%` : "-"}</b>
      </span>
      <span className="statusbar__spacer" />
      <JobsChip jobs={jobsList} />
      <Tooltip label={t("status.spendTitle")}>
        <span className="stat statusbar__cost">
          <span className="stat__label">{t("status.costLabel")}</span>
          <b>{costLabel}</b>
        </span>
      </Tooltip>
      <Tooltip label={t("status.balanceTitle")}>
        <span className="stat stat--balance statusbar__balance">
          <span className="stat__label">{t("status.balanceLabel")}</span>
          <b>{balanceLabel}</b>
        </span>
      </Tooltip>
      {planMode && <span className="statusbar__plan">{t("status.plan")}</span>}
      {goalMode && <span className="statusbar__plan">{t("composer.goalMode")}</span>}
      {toolApprovalMode === "auto" && (
        <Tooltip label={t("composer.accessAutoTitle")}>
          <span className="statusbar__yolo">{t("composer.accessAuto")}</span>
        </Tooltip>
      )}
      {toolApprovalMode === "yolo" && (
        <Tooltip label={t("status.yoloTitle")}>
          <span className="statusbar__yolo">{t("composer.accessYolo")}</span>
        </Tooltip>
      )}
    </div>
  );
}
