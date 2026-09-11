import { CircleCheck, CircleX, Clock3, TriangleAlert } from "lucide-react";
import { useT } from "../lib/i18n";
import { turnChangeText, turnCheckState, turnCheckText } from "../lib/turnResult";
import type { WireCompletionSummary } from "../lib/types";

export function TurnResultSummary({ summary }: { summary: WireCompletionSummary }) {
  const t = useT();
  const diff = summary.receipt?.diff;
  const state = turnCheckState(summary);
  const Icon = state.status === "passed" ? CircleCheck : state.status === "failed" ? CircleX : state.status === "running" ? Clock3 : TriangleAlert;
  return <div className="turn-result-summary">
    <div className="turn-result-summary__changes">
      {diff && diff.coverage !== "unknown" && diff.files.length > 0 ? <>
        <span>{t(diff.coverage === "complete" ? "completion.filesChanged" : "completion.filesCounted", { count: diff.files.length })}</span>
        <span aria-hidden="true"> · </span><span className="turn-result-added">+{diff.added}</span>{" "}<span className="turn-result-removed">−{diff.removed}</span>
        {diff.coverage === "partial" && <span> · {t("completion.partialStats")}</span>}
      </> : turnChangeText(summary, t)}
    </div>
    <div className={`turn-result-summary__checks turn-result-summary__checks--${state.status}`} aria-busy={summary.checking || undefined}>
      <Icon size={14} aria-hidden="true" /><span>{turnCheckText(summary, t)}</span>
    </div>
    {diff?.coverage === "partial" && <details className="notice-line__details"><summary>{t("notice.details")}</summary><p>{t("completion.coverageReason")}</p></details>}
  </div>;
}
