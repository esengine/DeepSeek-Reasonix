import { useEffect, useRef, useState } from "react";
import { Tooltip } from "./Tooltip";
import { useI18n } from "../lib/i18n";
import { type BalanceInfo, type CollaborationMode, type ContextInfo, type JobView, type ToolApprovalMode, type WireUsage } from "../lib/types";

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

function formatTokenCount(tokens?: number): string {
  if (typeof tokens !== "number" || tokens <= 0) return "-";
  return tokens.toLocaleString();
}

export function StatusBar({
  context,
  usage,
  balance,
  jobs,
  running,
  collaborationMode,
  toolApprovalMode,
  sessionTokens,
  cost,
  currency,
  modelLabel,
}: {
  context: ContextInfo;
  usage?: WireUsage;
  balance?: BalanceInfo;
  jobs?: JobView[];
  running: boolean;
  collaborationMode: CollaborationMode;
  toolApprovalMode: ToolApprovalMode;
  sessionTokens?: number;
  cost?: number;
  currency?: string;
  modelLabel?: string;
}) {
  const { t } = useI18n();
  const pct = context.window ? Math.min(100, Math.round((context.used / context.window) * 100)) : null;
  const compactPct = context.compactRatio ? Math.round(context.compactRatio * 100) : null;
  const compactNear = pct !== null && compactPct !== null && pct >= Math.max(0, compactPct - 10);
  const compactReached = pct !== null && compactPct !== null && pct >= compactPct;
  const nowPct = nowRate(usage);
  const avgPct = avgRate(usage);
  const jobsList = jobs ?? [];
  const costLabel = formatMoney(cost, currency);
  const tokenLabel = formatTokenCount(sessionTokens);
  const balanceLabel = balance?.available && balance.display ? balance.display : "-";
  const planMode = collaborationMode === "plan";
  const goalMode = collaborationMode === "goal";

  return (
    <div className="statusbar">
      <div className="statusbar__group statusbar__group--model">
        <Tooltip label={t("status.modelTitle")}>
          <span className="stat stat--model">
            <span className={`statusbar__dot ${running ? "statusbar__dot--busy" : ""}`} />
            {modelLabel && <span className="statusbar__model">{modelLabel}</span>}
          </span>
        </Tooltip>
      </div>
      <div className="statusbar__group statusbar__group--primary">
        <Tooltip label={t("status.cacheTitle")}>
          <span className="stat statusbar__cache">
            <span className="stat__label">{t("status.cacheLabel")}</span>
            <b className={rateValueClass(nowPct) || undefined}>{nowPct !== null ? `${nowPct}%` : "-"}</b>
          </span>
        </Tooltip>
        <Tooltip label={t("status.cacheAvgTitle")}>
          <span className="stat statusbar__avg">
            <span className="stat__label">{t("status.cacheAvgLabel")}</span>
            <b className={rateValueClass(avgPct) || undefined}>{avgPct !== null ? `${avgPct}%` : "-"}</b>
          </span>
        </Tooltip>
        <Tooltip label={t("status.sessionTokensTitle")}>
          <span className="stat statusbar__tokens">
            <span className="stat__label">{t("status.sessionTokensLabel")}</span>
            <b className={tokenLabel === "-" ? "stat__value--empty" : undefined}>{tokenLabel}</b>
          </span>
        </Tooltip>
      </div>
      <div className="statusbar__group statusbar__group--context">
        <Tooltip label={t("status.ctxTitle")}>
          <span className="stat statusbar__ctx">
            <span className="stat__label">{t("status.ctxLabel")}</span>
            <b className={pct === null ? "stat__value--empty" : undefined}>{pct !== null ? `${pct}%` : "-"}</b>
          </span>
        </Tooltip>
        <Tooltip label={t("status.compactTitle")}>
          <span className="stat statusbar__compact">
            <span className="stat__label">{t("status.compactLabel")}</span>
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
      </div>
      <div className="statusbar__group statusbar__group--account">
        <Tooltip label={t("status.spendTitle")}>
          <span className="stat statusbar__cost">
            <span className="stat__label">{t("status.costLabel")}</span>
            <b>{costLabel}</b>
          </span>
        </Tooltip>
        <Tooltip label={t("status.balanceTitle")}>
          <span className="stat stat--balance statusbar__balance">
            <span className="stat__label">{t("status.balanceLabel")}</span>
            <b className={balanceLabel === "-" ? "stat__value--empty" : undefined}>{balanceLabel}</b>
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
      {jobsList.length > 0 && (
        <div className="statusbar__group statusbar__group--jobs">
          <JobsChip jobs={jobsList} />
        </div>
      )}
    </div>
  );
}
