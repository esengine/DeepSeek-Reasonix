import type { Translator } from "./i18n";

export function completionPresetLabel(preset: string, t: Translator): string {
  switch (preset.trim().toLowerCase()) {
    case "light":
    case "economy": return t("composer.runtimeProfileEconomyShort");
    case "delivery": return t("composer.runtimeProfileDeliveryShort");
    case "balanced":
    case "full": return t("composer.runtimeProfileBalancedShort");
    default: return t("context.other");
  }
}

export function completionVerdictLabel(verdict: string, t: Translator): string {
  switch (verdict.trim().toLowerCase()) {
    case "complete": return t("task.state.succeeded");
    case "partial": return t("completion.verdictPartial");
    case "blocked": return t("completion.verdictBlocked");
    case "continue": return t("notice.decisionRecoveryContinue");
    default: return t("projectTree.status.waitingConfirmation");
  }
}

export function completionReviewLabel(review: string, t: Translator): string {
  switch (review.trim().toLowerCase()) {
    case "none": return t("common.none");
    case "passed": return t("task.state.succeeded");
    case "warned": return t("diag.warnings");
    case "failed": return t("task.state.failed");
    case "unavailable": return t("caps.toolUnavailable");
    default: return t("projectTree.status.waitingConfirmation");
  }
}

export function completionGapLabel(gap: string, t: Translator): string {
  switch (gap.trim().toLowerCase()) {
    case "suppressed":
    case "suppressed_requirement": return t("tool.shell.verificationNotRun");
    case "stale_check": return t("completion.gapStaleCheck");
    default: return t("context.other");
  }
}
