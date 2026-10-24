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
  if (h > 0) return t("{h} 小时 {m} 分", { h, m });
  if (m > 0) return t("{m} 分 {s} 秒", { m, s });
  return t("{s} 秒", { s });
}

// The labels of the usage sources the kernel names; any other source is shown by its own id.
function sourceName(id: string): string {
  switch (id) {
    case "executor": return t("主代理");
    case "subagent": return t("子代理");
    case "planner": return t("规划");
    case "compaction": return t("压缩");
    case "title": return t("会话标题");
    case "classifier": return t("分类器");
    case "goal-evaluator": return t("目标评估");
    case "web-search": return t("网页搜索");
    default: return id;
  }
}

function cost(say: CostSay, hide: boolean): { text: string; note: string } {
  const show = (n: number, c: string) => (hide ? MASK : money(n, c));
  switch (say.kind) {
    case "exact": return { text: show(say.amount, say.currency), note: "" };
    case "atLeast": return { text: `≥ ${show(say.amount, say.currency)}`, note: t("部分请求没有价格信息") };
    case "unknown": return { text: "—", note: say.reason ? t("价格未知：{reason}", { reason: say.reason }) : t("价格未知") };
    default: return { text: "—", note: t("还没有计费的请求") };
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
    <div className={["chrome-context-card", "studio-overview-card", className].filter(Boolean).join(" ")} role="dialog" aria-label={t("会话概览")}>
      <header><span><b>{t("会话概览")}</b><small>{t("自本次打开此会话起；恢复或分叉会重新计数")}</small></span></header>
      {!usage || usage.requests === 0 ? (
        <p>{t("这个会话还没有计费的请求")}</p>
      ) : (
        <>
          <dl className="studio-overview-grid">
            <div><dt>{t("请求")}</dt><dd>{usage.requests}</dd><small>{t("返回了用量与价格的模型响应")}</small></div>
            <div><dt>{t("Token")}</dt><dd>{tokens(sessionTokens(usage))}</dd><small>{t("输入 {a} · 输出 {b}", { a: tokens(usage.promptTokens), b: tokens(usage.completionTokens) })}</small></div>
            <div>
              <dt>{t("缓存命中")}</dt><dd>{percent(cacheRate(usage.cacheHit, usage.cacheMiss, unknown))}</dd>
              <small>{unknown > 0 ? t("命中 ÷ (命中 + 未命中)；不含服务商未区分的 {n} tokens", { n: tokens(unknown) }) : t("命中 ÷ (命中 + 未命中)")}</small>
            </div>
            <div><dt>{t("工作时长")}</dt><dd>{clock(usage.workMs)}</dd><small>{t("给出最终回答的回合耗时之和，含审批与工具等待")}</small></div>
            <div><dt>{t("费用")}</dt><dd>{total.text}</dd>{total.note && <small>{total.note}</small>}</div>
          </dl>
          <section aria-label={t("按来源")}>
            <div><b>{t("按来源")}</b><small>{t("请求 · Token · 缓存 · 费用")}</small></div>
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
    <section aria-label={t("上下文窗口")}>
      <div><b>{t("上下文窗口")}</b><b>{Math.min(100, Math.round((ctx.used / ctx.window) * 100))}%</b></div>
      <small>{t("下一次请求会带上的内容，是当前值，不是累计")}</small>
      <div className="chrome-context-total"><span>{t("已使用")} <b>{tokens(ctx.used)}</b></span><span>{t("模型容量")} <b>{tokens(ctx.window)}</b></span></div>
      <div className="chrome-context-bar">{PARTS.map(([key, field]) => <i key={key} data-part={key} style={{ width: `${ctx.used ? Math.max(1, (ctx[field] / ctx.used) * 100) : 0}%` }} />)}</div>
      {ctx.compact_at > 0 && <small>{t("自动压缩于 {n}", { n: tokens(ctx.compact_at) })}</small>}
      {onContext && <button type="button" data-action="metrics.details" data-value="context-from-overview" onClick={onContext}>{t("上下文详情")}</button>}
    </section>
  );
}
