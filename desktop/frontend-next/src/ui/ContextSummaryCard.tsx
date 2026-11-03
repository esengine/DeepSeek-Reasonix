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
    [t("common.systemPrompt"), context.system, "sys"],
    [t("common.toolDefinitions"), context.tools, "tool"],
    [t("common.whatYouSaid"), context.user, "user"],
    [t("common.modelReplies"), context.reply, "reply"],
    [t("common.toolOutput"), context.output, "output"],
  ] as const : [];
  const holdersOf = (key: string) => (context?.largest ?? []).filter((h) => h.class === key);
  const opened = parts.find(([, , key]) => key === listed);

  return (
    <div className={["chrome-context-card", className].filter(Boolean).join(" ")} role="dialog" aria-label={t("common.contextDetails")}>
      <header><span><b>{t("common.contextDetails")}</b><small>{t("contextSummaryCard.contentSentToTheModel")}</small></span><strong>{percent ?? 0}%</strong></header>
      {context ? (
        <>
          <div className="chrome-context-total"><span>{t("common.used")} <b>{compact(context.used)}</b></span><span>{t("common.modelCapacity")} <b>{compact(context.window)}</b></span></div>
          <div className="chrome-context-bar">{parts.map(([label, value, key]) => <i key={key} data-part={key} title={`${label} ${compact(value)}`} style={{ width: `${context.used ? Math.max(1, (value / context.used) * 100) : 0}%` }} />)}</div>
          <div className="chrome-context-parts">{parts.map(([label, value, key]) => (
            <div key={key}>
              <i data-part={key} /><span>{label}</span>
              {holdersOf(key).length > 0
                ? <button type="button" className="chrome-context-more" data-action="context.largest" data-value={key} aria-expanded={listed === key} aria-label={t("contextSummaryCard.showTheLargestItemsIn", { name: label })} onClick={() => setListed(listed === key ? null : key)}>{compact(value)}</button>
                : <b>{compact(value)}</b>}
            </div>
          ))}</div>
          {opened && (
            <ul className="chrome-context-items" aria-labelledby="chrome-context-items-title">
              <li id="chrome-context-items-title" className="head">{t("contextSummaryCard.largestItemsIn", { name: opened[0] })}</li>
              {holdersOf(opened[2]).map((h, i) => (
                <li key={i}>
                  <span data-tool={h.tool ? "" : undefined}>{h.tool || opened[0]}</span>
                  {h.active && <em>{t("contextSummaryCard.runningTurn")}</em>}
                  <b>{compact(h.tokens)}</b>
                </li>
              ))}
            </ul>
          )}
        </>
      ) : <p>{t("contextSummaryCard.noContextDataYet")}</p>}
      {action && <CompactNow action={action} />}
      <section>
        <div><b>MCP</b><small>{ready.length} / {active.length} {t("contextSummaryCard.connected")} · {tools} {t("contextSummaryCard.tools")}</small></div>
        {active.slice(0, 4).map((server) => <div className="chrome-mcp-row" key={server.name}><i data-ready={server.state === "ready" ? "" : undefined} /><span>{server.name}</span><small>{server.state === "ready" ? `${server.tools} ${t("contextSummaryCard.tools")}` : t("contextSummaryCard.waitingToConnect")}</small></div>)}
        {!active.length && <p>{t("contextSummaryCard.noMCPServicesEnabled")}</p>}
        <button data-action="settings.section" data-value="ext" onClick={onManage}>{t("contextSummaryCard.manageMCPAndTools")}</button>
      </section>
    </div>
  );
}
