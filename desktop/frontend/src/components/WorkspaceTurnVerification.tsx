import { forwardRef } from "react";
import { completionSummaryNeedsAttention } from "../lib/completionSummary";
import {
  completionGapLabel,
  completionReviewLabel,
  completionVerdictLabel,
} from "../lib/completionSummaryDisplay";
import { useT } from "../lib/i18n";
import type { WireCompletionSummary } from "../lib/types";

export const WORKSPACE_TURN_VERIFICATION_ID = "workspace-turn-verification";

export const WorkspaceTurnVerification = forwardRef<HTMLElement, {
  summary: WireCompletionSummary;
  qualityFloor?: "standard" | "delivery";
}>(
  function WorkspaceTurnVerification({ summary, qualityFloor = "standard" }, ref) {
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
          <span>{completionVerdictLabel(summary.verdict, t)}</span>
        </div>
        <div className="workspace-completion-summary__metrics">
          <span>{t("completion.mutations", { count: summary.mutations })}</span>
          <span>{t("completion.checksPassed", { count: summary.checks_passed })}</span>
          <span className={summary.checks_failed > 0 ? "workspace-completion-summary__metric--attention" : undefined}>
            {t("completion.checksFailed", { count: summary.checks_failed })}
          </span>
          <span className={summary.checks_suppressed > 0 ? "workspace-completion-summary__metric--attention" : undefined}>
            {t("completion.checksSkipped", { count: summary.checks_suppressed })}
          </span>
        </div>
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
