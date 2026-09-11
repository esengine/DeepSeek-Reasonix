import { forwardRef } from "react";
import { completionSummaryNeedsAttention } from "../lib/completionSummary";
import {
  completionGapLabel,
  completionReviewLabel,
} from "../lib/completionSummaryDisplay";
import { useT } from "../lib/i18n";
import type { WireCompletionSummary } from "../lib/types";
import { TurnCheckDetails } from "./TurnCheckDetails";

export const WORKSPACE_TURN_VERIFICATION_ID = "workspace-turn-verification";

export const WorkspaceTurnVerification = forwardRef<HTMLElement, {
  summary: WireCompletionSummary;
  qualityFloor?: "standard" | "delivery";
  tabId?: string;
  sessionPath?: string;
}>(
  function WorkspaceTurnVerification({ summary, qualityFloor = "standard", tabId, sessionPath }, ref) {
    const t = useT();
    return (
      <section
        ref={ref}
        id={WORKSPACE_TURN_VERIFICATION_ID}
        className={`workspace-note workspace-completion-summary${completionSummaryNeedsAttention(summary, qualityFloor) ? " workspace-completion-summary--attention" : ""}`}
        aria-labelledby={`${WORKSPACE_TURN_VERIFICATION_ID}-title`}
      >
        <div className="workspace-completion-summary__head">
          <h3 id={`${WORKSPACE_TURN_VERIFICATION_ID}-title`} className="workspace-completion-summary__title">{t("completion.panelTitle")}</h3>
        </div>
        <TurnCheckDetails summary={summary} tabId={tabId} sessionPath={sessionPath} />
        <div className="workspace-completion-summary__details">
          <span>{t("completion.review", { status: completionReviewLabel(summary.review, t) })}</span>
          {(summary.gap_kinds?.length ?? 0) > 0 && (
            <span>{t("completion.gaps", { gaps: summary.gap_kinds!.map((gap) => completionGapLabel(gap, t)).join(t("notice.deliveryRequirementSeparator")) })}</span>
          )}
          {summary.constraint_degraded && <span>{t("completion.constraintsLimited")}</span>}
        </div>
      </section>
    );
  },
);
