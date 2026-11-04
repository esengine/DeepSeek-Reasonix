import { useState } from "react";
import { t } from "../i18n";
import type { ContextBreakdown, McpEntry } from "../port/port";
import { CompactNow, type CompactAction } from "./CompactNow";

const compact = (n: number): string => n >= 1_000_000 ? `${(n / 1_000_000).toFixed(1)}M` : n >= 1_000 ? `${Math.round(n / 100) / 10}k` : String(n);

export function ContextSummaryCard({ context, mcp, percent, onManage, compact: action, className = "" }: {
  context: ContextBreakdown | null;
  mcp: McpEntry[];
  percent: number | null;
  onManage: () => void;
  compact?: CompactAction;
  className?: string;
}) {
  // One class open at a time: two lists in a card this narrow push the rest off the screen.
  const [listed, setListed] = useState<string | null>(null);
  const active = mcp.filter((entry) => entry.enabled);
  const ready = active.filter((entry) => entry.state === "ready");
  const tools = ready.reduce((sum, entry) => sum + (entry.tools || 0), 0);
  const parts = context ? [
    [t("common.system_prompt"), context.system, "sys"],
    [t("common.tool_definitions"), context.tools, "tool"],
    [t("common.what_you_said"), context.user, "user"],
    [t("common.model_replies"), context.reply, "reply"],
    [t("common.tool_output"), context.output, "output"],
  ] as const : [];
  const holdersOf = (key: string) => (context?.largest ?? []).filter((h) => h.class === key);
  const opened = parts.find(([, , key]) => key === listed);

  return (
    <div className={["chrome-context-card", className].filter(Boolean).join(" ")} role="dialog" aria-label={t("common.context_details")}>
      <header><span><b>{t("common.context_details")}</b><small>{t("composer.content_sent_to_the_model")}</small></span><strong>{percent ?? 0}%</strong></header>
      {context ? (
        <>
          <div className="chrome-context-total"><span>{t("common.used")} <b>{compact(context.used)}</b></span><span>{t("common.model_capacity")} <b>{compact(context.window)}</b></span></div>
          <div className="chrome-context-bar">{parts.map(([label, value, key]) => <i key={key} data-part={key} title={`${label} ${compact(value)}`} style={{ width: `${context.used ? Math.max(1, (value / context.used) * 100) : 0}%` }} />)}</div>
          <div className="chrome-context-parts">{parts.map(([label, value, key]) => (
            <div key={key}>
              <i data-part={key} /><span>{label}</span>
              {holdersOf(key).length > 0
                ? <button type="button" className="chrome-context-more" data-action="context.largest" data-value={key} aria-expanded={listed === key} aria-label={t("composer.show_the_largest_items_in", { name: label })} onClick={() => setListed(listed === key ? null : key)}>{compact(value)}</button>
                : <b>{compact(value)}</b>}
            </div>
          ))}</div>
          {opened && (
            <ul className="chrome-context-items" aria-labelledby="chrome-context-items-title">
              <li id="chrome-context-items-title" className="head">{t("composer.largest_items_in_name", { name: opened[0] })}</li>
              {holdersOf(opened[2]).map((h, i) => (
                <li key={i}>
                  <span data-tool={h.tool ? "" : undefined}>{h.tool || opened[0]}</span>
                  {h.active && <em>{t("composer.running_turn")}</em>}
                  <b>{compact(h.tokens)}</b>
                </li>
              ))}
            </ul>
          )}
        </>
      ) : <p>{t("composer.no_context_data_yet")}</p>}
      {action && <CompactNow action={action} />}
      <section>
        <div><b>MCP</b><small>{ready.length} / {active.length} {t("composer.connected")} · {tools} {t("composer.tools")}</small></div>
        {active.slice(0, 4).map((server) => <div className="chrome-mcp-row" key={server.name}><i data-ready={server.state === "ready" ? "" : undefined} /><span>{server.name}</span><small>{server.state === "ready" ? `${server.tools} ${t("composer.tools")}` : t("composer.waiting_to_connect")}</small></div>)}
        {!active.length && <p>{t("composer.no_mcp_services_enabled")}</p>}
        <button data-action="settings.section" data-value="ext" onClick={onManage}>{t("composer.manage_mcp_and_tools")}</button>
      </section>
    </div>
  );
}
