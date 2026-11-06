import { useCallback, useId, useRef, useState, type CSSProperties } from "react";
import { t } from "../i18n";
import { money } from "../i18n/format";
import type { ContextBreakdown, JobEntry, JobOutput, McpEntry, SessionUsage } from "../port/port";
import type { CostQuote } from "../port/wire";
import { cacheRate as rateOf } from "../state/overview";
import { SessionOverviewCard } from "./SessionOverviewCard";
import type { CurrencyAmount, Metrics } from "../state/session_types";
import { DeckChips, type Deck } from "./DeckChips";
import { ContextSummaryCard } from "./ContextSummaryCard";
import type { CompactAction } from "./CompactNow";
import { useDismiss } from "./dismiss";
import type { Task } from "./panels/Agents";
import { Spark } from "./Spark";
import { StudioIcon } from "./StudioIcon";
import { TariffChip } from "./TariffChip";
import type { Tariff } from "../port/session";
import { setHidesAmounts } from "../state/prefs";
import { MASK, type Wallet } from "./wallet";
import type { SpeedSummary } from "./speed";

/** The numbers about this turn, in one row under the composer. */
export function MeterRail({ tps, trail, running, speed, metrics, ctx, mcp, compact, cost, turnCost = [], wallet, hideAmounts, tasks, jobs, onSettings, onCancelJob, onReadJob, onOpenRun, onStopCall, onClearJobs, onOpenJob, usage, quote, tariff, onTariffFlip }: {
  tps: number;
  trail: number[];
  running: boolean;
  speed: SpeedSummary;
  metrics: Pick<Metrics, "hit" | "miss">;
  ctx: ContextBreakdown | null;
  mcp: McpEntry[];
  compact: CompactAction;
  // One entry per currency: two currencies are two amounts, never one sum.
  cost: CurrencyAmount[];
  // What the turn in front of you spent, restarted at each turn; the session total is cost.
  turnCost?: CurrencyAmount[];
  wallet: Wallet;
  hideAmounts: boolean;
  tasks: Task[];
  jobs: JobEntry[];
  onSettings: (section?: string) => void;
  onCancelJob: (id: string) => Promise<void>;
  onReadJob?: (id: string) => Promise<JobOutput>;
  onOpenRun?: (call: string) => Promise<boolean>;
  onStopCall?: (callId: string) => void;
  onClearJobs?: (ids: string[]) => void;
  onOpenJob?: (job: JobEntry) => void;
  usage?: SessionUsage;
  quote?: CostQuote;
  tariff?: Tariff;
  onTariffFlip?: () => void;
}) {
  const speedDetailId = useId();
  const [meterOpen, setMeterOpen] = useState(false);
  const meterRef = useRef<HTMLDivElement>(null);
  const closeMeter = useCallback(() => setMeterOpen(false), []);
  useDismiss(meterOpen, meterRef, closeMeter);
  const [deck, setDeck] = useState<Deck>("");
  const [overviewOpen, setOverviewOpen] = useState(false);
  const overviewRef = useRef<HTMLDivElement>(null);
  const closeOverview = useCallback(() => setOverviewOpen(false), []);
  useDismiss(overviewOpen, overviewRef, closeOverview);
  // The same rate the overview states: from the ledger when the kernel sends it,
  // so the meter and the card cannot disagree about one number.
  const ratio = usage && usage.requests > 0 ? rateOf(usage.cacheHit, usage.cacheMiss, usage.cacheUnknown ?? 0) : rateOf(metrics.hit, metrics.miss);
  const cacheRate = ratio === null ? null : Math.floor(ratio * 100);
  const contextPercent = ctx && ctx.window > 0 ? Math.min(100, Math.round((ctx.used / ctx.window) * 100)) : 0;
  return (
    <div className="studio-meterrail" ref={meterRef} aria-label={t("composer.run_metrics")}>
      <div className="studio-speed-anchor">
        <button
          className="studio-meter-static studio-meter-speed"
          type="button"
          aria-describedby={speedDetailId}
          aria-label={t("composer.view_generation_speed_details")}
        >
          <Spark points={trail} w={44} h={13} />
          <b>{tps > 0 ? tps.toFixed(1) : "—"}</b><span>tok/s</span><i data-live={running ? "" : undefined} aria-hidden="true" />
        </button>
        <div className="studio-speed-detail" id={speedDetailId} role="tooltip">
          <header><b>{t("composer.generation_speed")}</b><small>{running ? t("composer.live") : t("composer.latest_run")}</small></header>
          <dl>
            <div><dt>{t("composer.current_rate")}</dt><dd>{tps > 0 ? `${tps.toFixed(1)} tok/s` : "—"}</dd></div>
            <div><dt>{t("composer.run_average")}</dt><dd>{speed.average > 0 ? `${speed.average.toFixed(1)} tok/s` : "—"}</dd></div>
            <div><dt>{t("composer.time_to_first_token")}</dt><dd>{speed.firstToken > 0 ? `${speed.firstToken.toFixed(2)}s` : "—"}</dd></div>
            <div><dt>{t("composer.run_output")}</dt><dd>{speed.output > 0 ? t("{n} tokens", { n: speed.output.toLocaleString() }) : "—"}</dd></div>
            <div><dt>{t("composer.model_time")}</dt><dd>{speed.modelSeconds > 0 ? `${speed.modelSeconds.toFixed(1)}s` : "—"}</dd></div>
          </dl>
          <p>{t("composer.current_rate_estimates_the_latest")}</p>
        </div>
      </div>
      <div className="studio-overview-anchor" ref={overviewRef} data-open={overviewOpen ? "" : undefined}>
        <button className="studio-meter-cache" type="button" data-action="metrics.details" data-value="overview" aria-expanded={overviewOpen} aria-haspopup="dialog" aria-label={t("composer.open_the_session_overview")} title={cacheRate === null ? t("composer.no_cache_data_yet") : t("composer.hit_hit_miss_miss", { hit: metrics.hit.toLocaleString(), miss: metrics.miss.toLocaleString() })} onClick={() => { setMeterOpen(false); setOverviewOpen((open) => !open); }}>
          <StudioIcon name="bolt" /><span>{t("composer.cache")}</span><b>{cacheRate === null ? "—" : `${cacheRate}%`}</b>
        </button>
        <SessionOverviewCard className="studio-overview-popover" usage={usage} quote={quote} hideAmounts={hideAmounts} onContext={ctx && ctx.window > 0 ? () => { setOverviewOpen(false); setMeterOpen(true); } : undefined} />
      </div>
      {ctx && ctx.window > 0 && (
        <div className="studio-context-anchor" data-open={meterOpen ? "" : undefined}>
          <button className="studio-meter-context" data-level={contextPercent >= 80 ? "high" : undefined} data-action="metrics.details" data-value="context" aria-expanded={meterOpen} aria-haspopup="dialog" aria-label={t("composer.view_context_and_compaction")} onClick={() => { setOverviewOpen(false); setMeterOpen((open) => !open); }}>
            <span className="studio-context-ring" style={{ "--fill": `${contextPercent}%` } as CSSProperties} aria-hidden="true" />
            <span>{t("common.context")}</span><b>{contextPercent}%</b>
          </button>
          <ContextSummaryCard
            className="studio-context-summary"
            context={ctx}
            mcp={mcp}
            percent={contextPercent}
            compact={compact}
            onManage={() => { closeMeter(); onSettings("extensions"); }}
          />
        </div>
      )}
      {tariff && <TariffChip tariff={tariff} onFlip={onTariffFlip ?? (() => {})} />}
      {cost.length > 0 && <span className="studio-meter-cost"><StudioIcon name="coin" /><span>{t("common.this_session_2")}</span>{hideAmounts ? <b>{MASK}</b> : cost.map((m) => <b key={m.currency}>{money(m.amount, m.currency)}</b>)}</span>}
      {cost.length > 0 && turnCost.some((m) => m.amount > 0) && <span className="studio-meter-cost"><span>{t("composer.this_turn")}</span>{hideAmounts ? <b>{MASK}</b> : turnCost.map((m) => <b key={m.currency}>{money(m.amount, m.currency)}</b>)}</span>}
      <button className="studio-meter-mask" type="button" data-action="metrics.hide-amounts" aria-pressed={hideAmounts} aria-label={hideAmounts ? t("composer.show_amounts") : t("composer.hide_amounts")} title={hideAmounts ? t("composer.show_amounts") : t("composer.hide_amounts")} onClick={() => setHidesAmounts(!hideAmounts)}><StudioIcon name={hideAmounts ? "eyeoff" : "eye"} /></button>
      {wallet.kind === "read" && <button className="studio-meter-wallet" data-short={wallet.reading.available ? undefined : "true"} title={wallet.reading.available ? undefined : t("common.insufficient_balance")} data-action="settings.section" data-value="usage" aria-label={wallet.reading.available ? t("composer.view_wallet_balance") : `${t("composer.view_wallet_balance")}, ${t("common.insufficient_balance")}`} onClick={() => onSettings("spend")}><StudioIcon name="wallet" /><b>{hideAmounts ? MASK : wallet.reading.display}</b></button>}
      <DeckChips tasks={tasks} jobs={jobs} open={deck} onOpen={setDeck} onCancelJob={onCancelJob} onReadJob={onReadJob} onOpenRun={onOpenRun} onStopCall={onStopCall} onClearJobs={onClearJobs} onOpenJob={onOpenJob} />
    </div>
  );
}
