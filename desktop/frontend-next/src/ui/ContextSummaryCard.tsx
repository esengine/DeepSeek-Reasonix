import { t } from "../i18n";
import type { ContextBreakdown, McpEntry } from "../port/port";

const compact = (n: number): string => n >= 1_000_000 ? `${(n / 1_000_000).toFixed(1)}M` : n >= 1_000 ? `${Math.round(n / 100) / 10}k` : String(n);

export function ContextSummaryCard({ context, mcp, percent, onManage, className = "" }: {
  context: ContextBreakdown | null;
  mcp: McpEntry[];
  percent: number | null;
  onManage: () => void;
  className?: string;
}) {
  const active = mcp.filter((entry) => entry.enabled);
  const ready = active.filter((entry) => entry.state === "ready");
  const tools = ready.reduce((sum, entry) => sum + (entry.tools || 0), 0);
  const parts = context ? [
    [t("系统提示"), context.system, "sys"],
    [t("工具定义"), context.tools, "tool"],
    [t("你的输入"), context.user, "user"],
    [t("模型回复"), context.reply, "reply"],
    [t("工具输出"), context.output, "output"],
  ] as const : [];

  return (
    <div className={["chrome-context-card", className].filter(Boolean).join(" ")} role="dialog" aria-label={t("上下文详情")}>
      <header><span><b>{t("上下文详情")}</b><small>{t("当前会话发送给模型的内容")}</small></span><strong>{percent ?? 0}%</strong></header>
      {context ? (
        <>
          <div className="chrome-context-total"><span>{t("已使用")} <b>{compact(context.used)}</b></span><span>{t("模型容量")} <b>{compact(context.window)}</b></span></div>
          <div className="chrome-context-bar">{parts.map(([label, value, key]) => <i key={key} data-part={key} title={`${label} ${compact(value)}`} style={{ width: `${context.used ? Math.max(1, (value / context.used) * 100) : 0}%` }} />)}</div>
          <div className="chrome-context-parts">{parts.map(([label, value, key]) => <div key={key}><i data-part={key} /><span>{label}</span><b>{compact(value)}</b></div>)}</div>
        </>
      ) : <p>{t("暂时没有上下文数据")}</p>}
      <section>
        <div><b>MCP</b><small>{ready.length} / {active.length} {t("个已连接")} · {tools} {t("个工具")}</small></div>
        {active.slice(0, 4).map((server) => <div className="chrome-mcp-row" key={server.name}><i data-ready={server.state === "ready" ? "" : undefined} /><span>{server.name}</span><small>{server.state === "ready" ? `${server.tools} ${t("个工具")}` : t("等待连接")}</small></div>)}
        {!active.length && <p>{t("尚未启用 MCP 服务")}</p>}
        <button data-action="settings.section" data-value="ext" onClick={onManage}>{t("管理 MCP 与工具")}</button>
      </section>
    </div>
  );
}
