import { t } from "../i18n";
import { money } from "../i18n/format";
import type { SessionUsage } from "../port/session";
import type { CostQuote } from "../port/wire";
import { cacheRate, costSay, outputRatio, sessionTokens, sourceTokens, warmCacheRate, type CostSay } from "../state/overview";
import { MASK } from "./wallet";

const tokens = (n: number): string => (n >= 1_000_000 ? `${(n / 1_000_000).toFixed(1)}M` : n >= 1_000 ? `${Math.round(n / 100) / 10}k` : String(n));
const percent = (r: number | null): string => (r === null ? "—" : `${Math.floor(r * 100)}%`);
const fine = (r: number | null): string => (r === null ? "—" : `${(r * 100).toFixed(1)}%`);

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
export function SessionOverviewCard({ usage, quote, hideAmounts, onContext, className = "" }: {
  usage?: SessionUsage;
  onContext?: () => void;
  quote?: CostQuote;
  hideAmounts: boolean;
  className?: string;
}) {
  const total = cost(costSay(quote), hideAmounts);
  const unknown = usage?.cacheUnknown ?? 0;
  const sources = usage?.sources ?? [];
  const asked = (id: string) => sources.find((src) => src.source === id)?.requests ?? 0;
  const priced = sources.map((src) => { const say = costSay(src.cost); return say.kind === "exact" ? say.amount : 0; });
  const pricedTotal = priced.reduce((n, v) => n + v, 0);
  return (
    <div className={["chrome-context-card", "studio-overview-card", className].filter(Boolean).join(" ")} role="dialog" aria-label={t("session.session_overview")}>
      <header><span><b>{t("session.session_overview")}</b><small>{t("session.since_this_session_was_opened")}</small></span></header>
      {!usage || usage.requests === 0 ? (
        <p>{t("session.this_session_has_no_billed")}</p>
      ) : (
        <>
          <dl className="studio-overview-grid">
            <div>
              <dt>{t("common.requests")}</dt><dd>{usage.requests}</dd>
              <small>{asked("executor") + asked("subagent") > 0 ? t("session.main_n_sub_m", { a: asked("executor"), b: asked("subagent") }) : t("session.model_responses_that_reported_usage")}</small>
            </div>
            <div><dt>{t("session.cost")}</dt><dd>{total.text}</dd>{total.note && <small>{total.note}</small>}</div>
            <div><dt>{t("Token")}</dt><dd>{tokens(sessionTokens(usage))}</dd><small>{t("session.in_a_out_b", { a: tokens(usage.promptTokens), b: tokens(usage.completionTokens) })}</small></div>
            <div>
              <dt>{t("common.cache_hit")}</dt><dd>{percent(cacheRate(usage.cacheHit, usage.cacheMiss, unknown))}</dd>
              <small>{unknown > 0 ? t("session.hit_hit_miss_leaves_out", { n: tokens(unknown) }) : t("session.hit_hit_miss")}</small>
            </div>
            <div>
              <dt>{t("session.warm_cache_hit")}</dt><dd>{percent(warmCacheRate(usage))}</dd>
              <small>{usage.coldStart ? t("session.leaves_out_the_cold_start_n", { n: tokens(usage.coldStart.promptTokens) }) : t("session.leaves_out_the_cold_start")}</small>
            </div>
            <div><dt>{t("session.output_ratio")}</dt><dd>{fine(outputRatio(usage))}</dd><small>{t("session.output_input")}</small></div>
          </dl>
          {sources.length > 0 && (
            <section aria-label={t("common.by_source")}>
              <div><b>{t("common.by_source")}</b></div>
              {sources.map((src, i) => {
                const c = cost(costSay(src.cost), hideAmounts);
                const detail = `${t("session.requests_tokens_cache_cost")}: ${src.requests} · ${tokens(sourceTokens(src))} · ${percent(cacheRate(src.cacheHit, src.cacheMiss, src.cacheUnknown ?? 0))} · ${c.text}`;
                return (
                  <div className="studio-overview-row" key={src.source} title={detail}>
                    <span>{sourceName(src.source)}</span>
                    <i aria-hidden="true"><u style={{ width: `${pricedTotal > 0 ? Math.max(priced[i] > 0 ? 2 : 0, (priced[i] / pricedTotal) * 100) : 0}%` }} /></i>
                    <b title={c.note || undefined}>{c.text}</b>
                  </div>
                );
              })}
            </section>
          )}
          <footer className="studio-overview-foot">
            <span title={t("session.sum_of_the_turns_that")}>{t("session.work_time")} {clock(usage.workMs)}</span>
            {onContext && <button type="button" data-action="metrics.details" data-value="context-from-overview" onClick={onContext}>{t("common.context_details")}</button>}
          </footer>
        </>
      )}
    </div>
  );
}
