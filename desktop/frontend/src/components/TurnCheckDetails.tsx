import { useEffect, useState } from "react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { turnCheckText } from "../lib/turnResult";
import type { WireCompletionSummary } from "../lib/types";

function CheckLog({ tabId, sessionPath, toolCallId, toolResultId, liveOutput }: { tabId: string; sessionPath: string; toolCallId?: string; toolResultId?: string; liveOutput?: string }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [loaded, setLoaded] = useState<{ key: string; output?: string; loading: boolean; truncated?: boolean }>();
  const key = `${tabId}\u0000${sessionPath}\u0000${toolCallId ?? ""}\u0000${toolResultId ?? ""}`;
  useEffect(() => {
    if (!open || liveOutput !== undefined) return;
    let active = true;
    if (!toolCallId || !toolResultId || !sessionPath) return;
    setLoaded({ key, loading: true });
    void app.TurnCheckLog(tabId, sessionPath, toolCallId, toolResultId).then(result => {
      if (active) setLoaded({ key, loading: false, output: result?.output, truncated: result?.truncated });
    }).catch(() => { if (active) setLoaded({ key, loading: false }); });
    return () => { active = false; };
  }, [key, liveOutput, open, sessionPath, tabId, toolCallId, toolResultId]);
  const value = liveOutput ?? (loaded?.key === key ? loaded.output : undefined);
  const loading = liveOutput === undefined && loaded?.key === key && loaded.loading;
  return <details className="turn-check-log" onToggle={e => setOpen(e.currentTarget.open)}>
    <summary>{t("completion.viewLogs")}</summary>
    {open && (loading ? <p role="status">{t("completion.loadingResult")}</p> : value !== undefined ? <>{loaded?.key === key && loaded.truncated && <p>{t("completion.logsTruncated")}</p>}<pre>{value}</pre></> : <p>{t("completion.logsUnavailable")}</p>)}
  </details>;
}

export function TurnCheckDetails({ summary, tabId = "", sessionPath = "" }: { summary: WireCompletionSummary; tabId?: string; sessionPath?: string }) {
  const t = useT();
  const checks = summary.receipt?.verifications ?? [];
  return <div className="turn-check-details">
    <p className="turn-check-details__status">{turnCheckText(summary, t)}</p>
    {checks.map((check, index) => <div className="turn-check-details__entry" key={check.toolCallId ?? `${index}:${check.command}`}>
      <div className="turn-check-details__head"><code>{check.command}</code><span className={check.interrupted || check.stale ? "turn-result-warning" : check.passed ? "turn-result-added" : "turn-result-removed"}>
        {t(check.interrupted ? "completion.checkInterruptedItem" : check.passed ? "completion.checkPassedItem" : "completion.checkFailedItem")}
        {check.stale && ` · ${t("completion.checkStaleItem")}`}
      </span></div>
      {check.exitCode !== undefined && <div>{t("completion.exitCode", { code: check.exitCode })}</div>}
      <CheckLog tabId={tabId} sessionPath={sessionPath} toolCallId={check.toolCallId} toolResultId={check.toolResultId} />
    </div>)}
    {summary.checking && summary.liveChecks?.map(check => <div className="turn-check-details__entry" key={check.toolCallId}>
      <div className="turn-check-details__head"><code>{check.command}</code><span>{t("completion.checkRunning")}</span></div>
      <CheckLog tabId={tabId} sessionPath={sessionPath} toolCallId={check.toolCallId} liveOutput={check.output} />
    </div>)}
    {(summary.receipt?.gaps?.length ?? 0) > 0 && <ul className="turn-check-details__gaps">{summary.receipt!.gaps!.map((gap, index) => <li key={`${index}:${gap.kind}`}>{gap.detail || t("completion.checkUnknown")}</li>)}</ul>}
    {(summary.receipt?.risks?.length ?? 0) > 0 && <ul className="turn-check-details__gaps">{summary.receipt!.risks!.map((risk, index) => <li key={index}>{risk}</li>)}</ul>}
    {checks.length > 0 && <p className="turn-check-details__footnote">{t("completion.checksOnly")}</p>}
  </div>;
}
