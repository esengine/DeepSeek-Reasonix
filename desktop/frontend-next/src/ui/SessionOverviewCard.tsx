import { t } from "../i18n";
import { money } from "../i18n/format";
import type { ContextBreakdown } from "../port/shell";
import type { SessionUsage } from "../port/session";
import type { CostQuote } from "../port/wire";
import { cacheRate, costSay, sessionTokens, sourceTokens, type CostSay } from "../state/overview";
import { MASK } from "./wallet";

const tokens = (n: number): string => (n >= 1_000_000 ? `${(n / 1_000_000).toFixed(1)}M` : n >= 1_000 ? `${Math.round(n / 100) / 10}k` : String(n));
const percent = (r: number | null): string => (r === null ? "—" : `${Math.floor(r * 100)}%`);

function clock(ms: number): string {
  if (ms <= 0) return "—";
  const total = Math.round(ms / 1000);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h > 0) return t("common.h_h_m_m", { h, m });
  if (m > 0) return t("common.m_m_s_s", { m, s });
  return t("session.s_s", { s });
}

// The labels of the usage sources the kernel names; any other source is shown by its own id.
function sourceName(id: string): string {
  switch (id) {
    case "executor": return t("session.main_agent");
    case "subagent": return t("common.subagents");
    case "planner": return t("session.planning");
    case "compaction": return t("session.compaction");
    case "title": return t("session.session_title");
    case "classifier": return t("session.classifier");
    case "goal-evaluator": return t("session.goal_evaluation");
    case "web-search": return t("common.web_search");
    default: return id;
  }
}

function cost(say: CostSay, hide: boolean): { text: string; note: string } {
  const show = (n: number, c: string) => (hide ? MASK : money(n, c));
  switch (say.kind) {
    case "exact": return { text: show(say.amount, say.currency), note: "" };
    case "atLeast": return { text: `≥ ${show(say.amount, say.currency)}`, note: t("session.some_requests_carried_no_price") };
    case "unknown": return { text: "—", note: say.reason ? t("session.price_unknown_reason", { reason: say.reason }) : t("session.price_unknown") };
    default: return { text: "—", note: t("session.nothing_billed_yet") };
  }
}

// The session as the kernel's ledger holds it, each figure beside what it counts.
// Everything here is since this session was opened in this run: a resumed or
// forked session counts again from the start.
export function SessionOverviewCard({ usage, quote, ctx, hideAmounts, onContext, className = "" }: {
  usage?: SessionUsage;
  ctx?: ContextBreakdown | null;
  onContext?: () => void;
  quote?: CostQuote;
  hideAmounts: boolean;
  className?: string;
}) {
  const total = cost(costSay(quote), hideAmounts);
  const unknown = usage?.cacheUnknown ?? 0;
  return (
    <div className={["chrome-context-card", "studio-overview-card", className].filter(Boolean).join(" ")} role="dialog" aria-label={t("session.session_overview")}>
      <header><span><b>{t("session.session_overview")}</b><small>{t("session.since_this_session_was_opened")}</small></span></header>
      {!usage || usage.requests === 0 ? (
        <p>{t("session.this_session_has_no_billed")}</p>
      ) : (
        <>
          <dl className="studio-overview-grid">
            <div><dt>{t("common.requests")}</dt><dd>{usage.requests}</dd><small>{t("session.model_responses_that_reported_usage")}</small></div>
            <div><dt>{t("Token")}</dt><dd>{tokens(sessionTokens(usage))}</dd><small>{t("session.in_a_out_b", { a: tokens(usage.promptTokens), b: tokens(usage.completionTokens) })}</small></div>
            <div>
              <dt>{t("common.cache_hit")}</dt><dd>{percent(cacheRate(usage.cacheHit, usage.cacheMiss, unknown))}</dd>
              <small>{unknown > 0 ? t("session.hit_hit_miss_leaves_out", { n: tokens(unknown) }) : t("session.hit_hit_miss")}</small>
            </div>
            <div><dt>{t("session.work_time")}</dt><dd>{clock(usage.workMs)}</dd><small>{t("session.sum_of_the_turns_that")}</small></div>
            <div><dt>{t("session.cost")}</dt><dd>{total.text}</dd>{total.note && <small>{total.note}</small>}</div>
          </dl>
          <section aria-label={t("common.by_source")}>
            <div><b>{t("common.by_source")}</b><small>{t("session.requests_tokens_cache_cost")}</small></div>
            {usage.sources.map((src) => {
              const c = cost(costSay(src.cost), hideAmounts);
              return (
                <div className="studio-overview-row" key={src.source}>
                  <span>{sourceName(src.source)}</span>
                  <small>{src.requests}</small>
                  <b>{tokens(sourceTokens(src))}</b>
                  <small>{percent(cacheRate(src.cacheHit, src.cacheMiss, src.cacheUnknown ?? 0))}</small>
                  <b title={c.note || undefined}>{c.text}</b>
                </div>
              );
            })}
          </section>
        </>
      )}
      {ctx && ctx.window > 0 && <ContextWindow ctx={ctx} onContext={onContext} />}
    </div>
  );
}

const PARTS = [["sys", "system"], ["tool", "tools"], ["user", "user"], ["reply", "reply"], ["output", "output"]] as const;

// The window is a present reading, not a running total: what the next request would carry.
function ContextWindow({ ctx, onContext }: { ctx: ContextBreakdown; onContext?: () => void }) {
  return (
    <section aria-label={t("common.context_window")}>
      <div><b>{t("common.context_window")}</b><b>{Math.min(100, Math.round((ctx.used / ctx.window) * 100))}%</b></div>
      <small>{t("session.what_the_next_request_would")}</small>
      <div className="chrome-context-total"><span>{t("common.used")} <b>{tokens(ctx.used)}</b></span><span>{t("common.model_capacity")} <b>{tokens(ctx.window)}</b></span></div>
      <div className="chrome-context-bar">{PARTS.map(([key, field]) => <i key={key} data-part={key} style={{ width: `${ctx.used ? Math.max(1, (ctx[field] / ctx.used) * 100) : 0}%` }} />)}</div>
      {ctx.compact_at > 0 && <small>{t("session.folds_automatically_at_n", { n: tokens(ctx.compact_at) })}</small>}
      {onContext && <button type="button" data-action="metrics.details" data-value="context-from-overview" onClick={onContext}>{t("common.context_details")}</button>}
    </section>
  );
}
