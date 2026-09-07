import "./SubagentDetails.css";

import { useT, type Translator } from "../lib/i18n";
import { parseSubagentOutcomeText, type SubagentOutcome } from "../lib/subagentOutcome";

type SubagentOutcomeCardProps = {
  text?: string;
  outcome?: SubagentOutcome;
};

function outcomeLabel(t: Translator, status: string): string {
  switch (status) {
    case "completed": return t("subagent.phase.completed");
    case "partial": return t("subagent.phase.partial");
    case "failed": return t("subagent.phase.failed");
    case "cancelled": return t("subagent.phase.cancelled");
    default: return status;
  }
}

export function SubagentOutcomeCard({
  text,
  outcome,
}: SubagentOutcomeCardProps) {
  const t = useT();
  const [ref, status, errorCode, retryable] = outcome ?? parseSubagentOutcomeText(text) ?? [];
  if (!ref && !status) return null;
  return (
    <div className="tool__subagent-outcome">
      <div className="tool__subagent-outcome-status">
        {t("caps.subagent")} {outcomeLabel(t, status ?? "unknown")}
        {retryable ? ` · ${t("subagent.outcome.retryable")}` : ""}
      </div>
      {ref && <code>{ref}</code>}
      {errorCode && <div className="tool__note">{errorCode}</div>}
    </div>
  );
}
