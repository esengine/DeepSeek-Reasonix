import { app } from "./bridge";
import { t } from "./i18n";
import type { RewindResultView } from "./types";

export async function commitRewindWithPreview(
  sourceTabId: string,
  turn: number,
  scope: "conversation" | "code" | "both",
): Promise<RewindResultView> {
  const plan = await app.PreviewRewindForTab(sourceTabId, turn, scope);
  const remoteLegacy = !plan?.ok && /remote host uses legacy rewind semantics/i.test(plan?.error || "");
  if (!remoteLegacy) {
    const canCommit = scope === "conversation"
      ? plan?.canConversation
      : scope === "both"
        ? plan?.canConversation && plan?.canFiles
        : plan?.canFiles;
    if (!plan?.ok || !canCommit) {
      return {
        ok: false,
        error: plan?.error
          || plan?.disabledReason
          || (plan?.conflicts?.length ? plan.conflicts.join("; ") : "")
          || "rewind unavailable",
        conflicts: plan?.conflicts,
      };
    }
    if (scope !== "conversation" && (plan.coverage === "partial" || (plan.coverageGaps?.length ?? 0) > 0)) {
      const gaps = (plan.coverageGaps || []).join("\n");
      if (!window.confirm(t("rewind.confirmPartialCoverage", { gaps: gaps || t("rewind.partialCoverageUnknown") }))) {
        return { ok: false, error: "rewind cancelled" };
      }
    }
  }
  return app.CommitRewindForTab(sourceTabId, remoteLegacy ? "" : (plan.planId || ""), turn, scope);
}

export function undoCommittedRewind(sourceTabId: string, transactionId: string): Promise<RewindResultView> {
  return app.UndoRewindForTab(sourceTabId, transactionId);
}
