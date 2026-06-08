import { useState } from "react";
import { Tooltip } from "./Tooltip";
import { useI18n } from "../lib/i18n";
import type { BalanceInfo, ContextInfo, JobView, Mode, WireUsage } from "../lib/types";

// JobsChip is the status-bar background-jobs indicator: a count that opens an
// upward popover listing the running jobs (id · label · status), mirroring the
// ModelSwitcher's click-to-open pattern. With no jobs it still reserves a stable
// "jobs 0" slot so the IDE-style status order does not jump.
function JobsChip({ jobs }: { jobs: JobView[] }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  if (jobs.length === 0) {
    return (
      <span className="statusbar__item">
        {t("status.jobsCount", { n: 0 })}
      </span>
    );
  }
  return (
    <div className="statusbar__jobswrap">
      <Tooltip label={t("status.jobsTitle")}>
        <button className="statusbar__item statusbar__jobs" onClick={() => setOpen((v) => !v)}>
          {t("status.jobsCount", { n: jobs.length })}
        </button>
      </Tooltip>
      {open && (
        <>
          <div className="modelsw__backdrop" onClick={() => setOpen(false)} />
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
        </>
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
  mode,
  cost,
  currency,
}: {
  context: ContextInfo;
  usage?: WireUsage;
  balance?: BalanceInfo;
  jobs?: JobView[];
  running: boolean;
  mode: Mode;
  cost?: number;
  currency?: string;
}) {
  const { t } = useI18n();
  const pct = context.window ? Math.min(100, Math.round((context.used / context.window) * 100)) : null;
  const compactPct = context.compactRatio ? Math.round(context.compactRatio * 100) : null;
  const nowPct = nowRate(usage);
  const avgPct = avgRate(usage);
  const jobsList = jobs ?? [];
  const costLabel = formatMoney(cost, currency);

  return (
    <div className="statusbar">
      <span className={`statusbar__dot ${running ? "statusbar__dot--busy" : ""}`} />
      <span className="statusbar__item statusbar__ctx">{pct !== null ? t("status.ctx", { pct }) : t("status.ctxUnknown")}</span>
      <span className="statusbar__sep">·</span>
      <span className="statusbar__item statusbar__compact">{compactPct !== null ? t("status.compact", { pct: compactPct }) : t("status.compactUnknown")}</span>
      <span className="statusbar__sep">·</span>
      <span className="statusbar__item statusbar__cache">{t("status.cache", { pct: nowPct ?? "-" })}</span>
      <span className="statusbar__sep">·</span>
      <span className="statusbar__item statusbar__avg">{t("status.cacheAvg", { pct: avgPct ?? "-" })}</span>
      <span className="statusbar__sep">·</span>
      <Tooltip label={t("status.spendTitle")}>
        <span className="statusbar__item statusbar__cost">
          {t("status.cost", { amount: costLabel })}
        </span>
      </Tooltip>
      <span className="statusbar__sep">·</span>
      <JobsChip jobs={jobsList} />
      <span className="statusbar__sep">·</span>
      <Tooltip label={t("status.balanceTitle")}>
        <span className="statusbar__item statusbar__balance">
          {t("status.balance", { amount: balance?.available && balance.display ? balance.display : "-" })}
        </span>
      </Tooltip>
      <span className="statusbar__spacer" />
      {mode === "plan" && <span className="statusbar__plan">{t("status.plan")}</span>}
    </div>
  );
}
