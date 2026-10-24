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
export function MeterRail({ tps, trail, running, speed, metrics, ctx, mcp, compact, cost, turnCost = [], wallet, hideAmounts, tasks, jobs, onSettings, onCancelJob, onReadJob, onOpenRun, usage, quote, tariff, onTariffFlip }: {
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
    <div className="studio-meterrail" ref={meterRef} aria-label={t("运行统计")}>
      <div className="studio-speed-anchor">
        <button
          className="studio-meter-static studio-meter-speed"
          type="button"
          aria-describedby={speedDetailId}
          aria-label={t("查看生成速度详情")}
        >
          <Spark points={trail} w={44} h={13} />
          <b>{tps > 0 ? tps.toFixed(1) : "—"}</b><span>tok/s</span><i data-live={running ? "" : undefined} aria-hidden="true" />
        </button>
        <div className="studio-speed-detail" id={speedDetailId} role="tooltip">
          <header><b>{t("生成速度")}</b><small>{running ? t("实时更新") : t("最近一轮")}</small></header>
          <dl>
            <div><dt>{t("当前速度")}</dt><dd>{tps > 0 ? `${tps.toFixed(1)} tok/s` : "—"}</dd></div>
            <div><dt>{t("整轮平均")}</dt><dd>{speed.average > 0 ? `${speed.average.toFixed(1)} tok/s` : "—"}</dd></div>
            <div><dt>{t("首字时间")}</dt><dd>{speed.firstToken > 0 ? `${speed.firstToken.toFixed(2)}s` : "—"}</dd></div>
            <div><dt>{t("本轮输出")}</dt><dd>{speed.output > 0 ? t("{n} tokens", { n: speed.output.toLocaleString() }) : "—"}</dd></div>
            <div><dt>{t("模型耗时")}</dt><dd>{speed.modelSeconds > 0 ? `${speed.modelSeconds.toFixed(1)}s` : "—"}</dd></div>
          </dl>
          <p>{t("当前速度按最近 4 秒流式文本估算；整轮平均使用服务商返回的输出 Token 除以模型回合耗时。")}</p>
        </div>
      </div>
      <div className="studio-overview-anchor" ref={overviewRef} data-open={overviewOpen ? "" : undefined}>
        <button className="studio-meter-cache" type="button" data-action="metrics.details" data-value="overview" aria-expanded={overviewOpen} aria-haspopup="dialog" aria-label={t("查看会话概览")} title={cacheRate === null ? t("尚无缓存数据") : t("命中 {hit} · 未命中 {miss}", { hit: metrics.hit.toLocaleString(), miss: metrics.miss.toLocaleString() })} onClick={() => { setMeterOpen(false); setOverviewOpen((open) => !open); }}>
          <span>{t("缓存")}</span><b>{cacheRate === null ? "—" : `${cacheRate}%`}</b>
        </button>
        <SessionOverviewCard className="studio-overview-popover" usage={usage} quote={quote} ctx={ctx} hideAmounts={hideAmounts} onContext={ctx && ctx.window > 0 ? () => { setOverviewOpen(false); setMeterOpen(true); } : undefined} />
      </div>
      {ctx && ctx.window > 0 && (
        <div className="studio-context-anchor" data-open={meterOpen ? "" : undefined}>
          <button className="studio-meter-context" data-action="metrics.details" data-value="context" aria-expanded={meterOpen} aria-haspopup="dialog" aria-label={t("查看上下文与压缩")} onClick={() => { setOverviewOpen(false); setMeterOpen((open) => !open); }}>
            <span className="studio-context-ring" style={{ "--fill": `${contextPercent}%` } as CSSProperties} aria-hidden="true" />
            <span>{t("上下文")}</span><b>{contextPercent}%</b>
          </button>
          <ContextSummaryCard
            className="studio-context-summary"
            context={ctx}
            mcp={mcp}
            percent={contextPercent}
            compact={compact}
            onManage={() => { closeMeter(); onSettings("ext"); }}
          />
        </div>
      )}
      {tariff && <TariffChip tariff={tariff} onFlip={onTariffFlip ?? (() => {})} />}
      {cost.length > 0 && <span className="studio-meter-cost"><span>{t("本会话")}</span>{hideAmounts ? <b>{MASK}</b> : cost.map((m) => <b key={m.currency}>{money(m.amount, m.currency)}</b>)}</span>}
      {cost.length > 0 && turnCost.some((m) => m.amount > 0) && <span className="studio-meter-cost"><span>{t("本轮")}</span>{hideAmounts ? <b>{MASK}</b> : turnCost.map((m) => <b key={m.currency}>{money(m.amount, m.currency)}</b>)}</span>}
      <button className="studio-meter-mask" type="button" data-action="metrics.hide-amounts" aria-pressed={hideAmounts} aria-label={hideAmounts ? t("显示金额") : t("隐藏金额")} title={hideAmounts ? t("显示金额") : t("隐藏金额")} onClick={() => setHidesAmounts(!hideAmounts)}><StudioIcon name={hideAmounts ? "eyeoff" : "eye"} /></button>
      {wallet.kind === "read" && <button className="studio-meter-wallet" data-short={wallet.reading.available ? undefined : "true"} title={wallet.reading.available ? undefined : t("余额不足")} data-action="settings.section" data-value="usage" aria-label={wallet.reading.available ? t("查看钱包余额") : `${t("查看钱包余额")}, ${t("余额不足")}`} onClick={() => onSettings("usage")}><StudioIcon name="wallet" /><b>{hideAmounts ? MASK : wallet.reading.display}</b></button>}
      <DeckChips tasks={tasks} jobs={jobs} open={deck} onOpen={setDeck} onCancelJob={onCancelJob} onReadJob={onReadJob} onOpenRun={onOpenRun} />
    </div>
  );
}
