import { useCallback, useEffect, useRef, useState } from "react";
import { app } from "../lib/bridge";
import { contextWindowPercentages } from "../lib/contextWindow";
import { useI18n } from "../lib/i18n";
import { formatMoneyLocalized } from "../lib/money";
import type { BalanceInfo, ContextInfo, ContextPanelInfo } from "../lib/types";
import { AnchoredPopover } from "./AnchoredPopover";
import {
  contextWindowStatus,
  formatCacheHitRate,
} from "./ContextPanel";

interface ContextWindowRingProps {
  enabled?: boolean;
  context?: ContextInfo;
  tabId?: string;
  turnCost?: number;
  currency?: string;
  cacheHitTokens?: number;
  cacheMissTokens?: number;
  balance?: BalanceInfo;
}

const RING = 20;
const RING_R = (RING - 3) / 2;
const RING_C = 2 * Math.PI * RING_R;

function fmtCompact(n: number): string {
  if (n <= 0) return "0";
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1).replace(/\.0$/, "") + "M";
  if (n >= 1_000) return (n / 1_000).toFixed(1).replace(/\.0$/, "") + "k";
  return String(Math.round(n));
}

function fmtDuration(ms: number, t: ReturnType<typeof useI18n>['t']): string {
  if (ms <= 0) return "-";
  const totalSeconds = Math.max(1, Math.round(ms / 1000));
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes <= 0) return t("context.durationSeconds", { seconds });
  return t("context.durationMinutesSeconds", { minutes, seconds });
}

export function ContextWindowRing({ enabled = true, context, tabId, turnCost, currency, cacheHitTokens, cacheMissTokens, balance }: ContextWindowRingProps) {
  const { locale, t } = useI18n();
  const [open, setOpen] = useState(false);
  const [info, setInfo] = useState<ContextPanelInfo | null>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const loadingTabRef = useRef<string | null>(null);
  const requestSeq = useRef(0);
  const enterTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const leaveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const used = context?.used ?? 0;
  const windowTokens = context?.window ?? 0;
  const usagePercentages = contextWindowPercentages(used, windowTokens);
  const rawUsagePct = usagePercentages.raw;
  const usagePct = usagePercentages.display;
  const compactRatio = context?.compactRatio && context.compactRatio > 0 ? context.compactRatio : 0.85;
  const compactPct = Math.round(compactRatio * 100);
  const status = contextWindowStatus(rawUsagePct, compactPct);

  const loadInfo = useCallback(() => {
    if (!enabled || !tabId) return;
    if (loadingTabRef.current === tabId) return;
    const requestTab = tabId;
    const seq = requestSeq.current + 1;
    requestSeq.current = seq;
    loadingTabRef.current = requestTab;
    app.ContextPanel(requestTab).then((next) => {
      if (requestSeq.current === seq) setInfo(next);
    }).catch(() => {}).finally(() => {
      if (requestSeq.current === seq) loadingTabRef.current = null;
    });
  }, [enabled, tabId]);

  // Reset when tabId changes so an older panel request cannot paint a new session.
  useEffect(() => {
    requestSeq.current += 1;
    loadingTabRef.current = null;
    setInfo(null);
    if (!enabled) setOpen(false);
  }, [enabled, tabId]);

  useEffect(() => () => {
    requestSeq.current += 1;
    if (enterTimer.current != null) clearTimeout(enterTimer.current);
    if (leaveTimer.current != null) clearTimeout(leaveTimer.current);
  }, []);

  const onEnter = useCallback(() => {
    if (leaveTimer.current != null) clearTimeout(leaveTimer.current);
    loadInfo();
    enterTimer.current = setTimeout(() => setOpen(true), 200);
  }, [loadInfo]);

  const onLeave = useCallback(() => {
    if (enterTimer.current != null) clearTimeout(enterTimer.current);
    leaveTimer.current = setTimeout(() => setOpen(false), 120);
  }, []);

  const onPopoverEnter = useCallback(() => {
    if (leaveTimer.current != null) clearTimeout(leaveTimer.current);
  }, []);

  const onPopoverLeave = useCallback(() => {
    setOpen(false);
  }, []);

  if (!enabled) return null;

  const turnCacheHit = cacheHitTokens ?? info?.cacheHitTokens ?? 0;
  const turnCacheMiss = cacheMissTokens ?? info?.cacheMissTokens ?? 0;
  const turnCacheRate = formatCacheHitRate(turnCacheHit, turnCacheMiss);
  const compactTokens = windowTokens > 0 ? Math.round(windowTokens * compactRatio) : 0;
  const tokensToCompact = compactTokens > used ? compactTokens - used : 0;
  const ringOffset = RING_C * (1 - usagePct / 100);
  const elapsed = info?.elapsedMs && info.elapsedMs > 0 ? fmtDuration(info.elapsedMs, t) : undefined;
  const quoteStatus = info?.sessionCostQuote?.displayStatus;
  const sessionCostBucketed = quoteStatus === "bucketed" || info?.sessionCostQuote?.aggregateMode === "currency_buckets";
  const sessionCostFallback = quoteStatus === "fallback_original";
  const sessionCostComplete = (sessionCostFallback || info?.sessionCostComplete !== false) && quoteStatus !== "unavailable";
  const sessionCostRaw = info?.sessionCostQuote?.selected
    ? Number(info.sessionCostQuote.selected.amount)
    : info?.sessionCost;
  const sessionCostCurrency = info?.sessionCostQuote?.selected?.currency || info?.sessionCurrency;
  const sessionCost =
    !sessionCostBucketed && sessionCostComplete && typeof sessionCostRaw === "number" && sessionCostRaw > 0
      ? `≈${formatMoneyLocalized(sessionCostRaw, sessionCostCurrency, { locale, empty: "dash" }).replace(/^≈/, "")}`
      : undefined;
  const sessionCostHint =
    info?.sessionBillingMode === "subscription_equivalent"
        ? "payg_equivalent"
        : sessionCostFallback
          ? "fallback_original"
        : sessionCost
          ? "estimated"
        : undefined;
  const turnCostLabel = formatMoneyLocalized(turnCost, info?.sessionCurrency || currency, { locale, empty: "dash" });

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        className={`context-ring${open ? " context-ring--open" : ""} context-ring--${status.tone}`}
        onMouseEnter={onEnter}
        onMouseLeave={onLeave}
        aria-label={t("context.windowUsageSummary", { used: String(used), window: String(windowTokens), pct: rawUsagePct })}
      >
        <svg width={RING} height={RING} viewBox={`0 0 ${RING} ${RING}`} className="context-ring__svg">
          <circle className="context-ring__track" cx={RING / 2} cy={RING / 2} r={RING_R} fill="none" strokeWidth={3} />
          <circle
            className="context-ring__arc"
            cx={RING / 2} cy={RING / 2} r={RING_R}
            fill="none" strokeWidth={3}
            strokeLinecap="round"
            strokeDasharray={RING_C}
            strokeDashoffset={ringOffset}
            transform={`rotate(-90 ${RING / 2} ${RING / 2})`}
          />
        </svg>
      </button>
      <AnchoredPopover
        open={open}
        anchorRef={triggerRef}
        onClose={() => setOpen(false)}
        className={`context-ring-popover context-ring-popover--${status.tone}`}
        align="end"
        placement="auto"
      >
        <div className="context-ring-popover__inner" onMouseEnter={onPopoverEnter} onMouseLeave={onPopoverLeave}>
          <div className="context-ring-popover__header">
            <span className="context-ring-popover__title">
              {fmtCompact(used)} / {fmtCompact(windowTokens)}
            </span>
            <span className="context-ring-popover__pct">{rawUsagePct}%</span>
          </div>
          <div className="context-ring-popover__gauge">
            <div className="context-ring-popover__bar">
              <span className="context-ring-popover__fill" style={{ width: `${usagePct}%` }} />
              <span className="context-ring-popover__mark context-ring-popover__mark--compact" style={{ left: `${compactPct}%` }} />
              <span className="context-ring-popover__mark context-ring-popover__mark--attention" style={{ left: `30%` }} />
            </div>
          </div>
          <div className="context-ring-popover__rows">
            <div className="context-ring-popover__row">
              <span className="context-ring-popover__label">{t("context.windowCompactDistance")}</span>
              <span className="context-ring-popover__value">{fmtCompact(tokensToCompact)}</span>
            </div>
            {info?.requestCount != null && info.requestCount > 0 && (
              <div className="context-ring-popover__row">
                <span className="context-ring-popover__label">{t("context.requests")}</span>
                <span className="context-ring-popover__value">{info.requestCount}</span>
              </div>
            )}
            {elapsed && (
              <div className="context-ring-popover__row">
                <span className="context-ring-popover__label">{t("context.time")}</span>
                <span className="context-ring-popover__value">{elapsed}</span>
              </div>
            )}
            <div className="context-ring-popover__row">
              <span className="context-ring-popover__label">{t("status.cacheLabel")}</span>
              <span className="context-ring-popover__value">{turnCacheRate}</span>
            </div>
            {turnCost != null && turnCost > 0 && (
              <div className="context-ring-popover__row">
                <span className="context-ring-popover__label">{t("status.turnCostLabel")}</span>
                <span className="context-ring-popover__value">{turnCostLabel}</span>
              </div>
            )}
            {sessionCost && (
              <div className="context-ring-popover__row">
                <span className="context-ring-popover__label">
                  {sessionCostHint === "payg_equivalent"
                    ? t("context.sessionCostPaygEquivalent")
                    : sessionCostHint === "fallback_original"
                      ? t("context.sessionCostFallback")
                    : t("context.sessionCostEstimated")}
                </span>
                <span className="context-ring-popover__value">{sessionCost}</span>
              </div>
            )}
            {!sessionCost && sessionCostBucketed && (
              <div className="context-ring-popover__row">
                <span className="context-ring-popover__label">{t("context.sessionCost")}</span>
                <span className="context-ring-popover__value">{t("context.sessionCostBucketed")}</span>
              </div>
            )}
            {!sessionCost && !sessionCostBucketed && info?.sessionCostComplete === false && (
              <div className="context-ring-popover__row">
                <span className="context-ring-popover__label">{t("context.sessionCostEstimated")}</span>
                <span className="context-ring-popover__value">—</span>
              </div>
            )}
            {balance?.available && balance.display && (
              <div className="context-ring-popover__row">
                <span className="context-ring-popover__label">{t("status.balanceLabel")}</span>
                <span className="context-ring-popover__value context-ring-popover__value--accent">{balance.display}</span>
              </div>
            )}
          </div>
        </div>
      </AnchoredPopover>
    </>
  );
}
