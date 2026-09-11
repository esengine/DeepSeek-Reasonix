import { app } from "./bridge";
import { t } from "./i18n";
import type { RewindResultView, TabMeta } from "./types";

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
    if (scope !== "conversation" && rewindNeedsCoverageConfirm(plan)) {
      const gaps = (plan.coverageGaps || []).filter((gap) => !isScratchCoverageGap(gap)).join("\n");
      if (!window.confirm(t("rewind.confirmPartialCoverage", { gaps: gaps || t("rewind.partialCoverageUnknown") }))) {
        return { ok: false, error: "rewind cancelled" };
      }
    }
  }
  return app.CommitRewindForTab(sourceTabId, remoteLegacy ? "" : (plan.planId || ""), turn, scope);
}

export function partialRewindNotice(result: RewindResultView): string {
  if (!result.partial) return "";
  const summary = t("rewind.partialRestoreFailed");
  const detail = result.error
    || (result.conflicts?.length ? result.conflicts.join("; ") : "");
  return detail ? `${summary} ${detail}` : summary;
}

function isScratchCoverageGap(gap: string): boolean {
  return gap === "scratch" || gap.startsWith("scratch:");
}

function rewindNeedsCoverageConfirm(plan: { coverage?: string; coverageGaps?: string[]; expiredFilePayload?: boolean; legacy?: boolean }): boolean {
  if (plan.expiredFilePayload || plan.legacy) return true;
  const projectGaps = (plan.coverageGaps || []).filter((gap) => !isScratchCoverageGap(gap));
  return projectGaps.length > 0 || (plan.coverage === "partial" && projectGaps.length > 0);
}

export function rewindFailureDetail(result?: RewindResultView | null): string {
  return result?.error
    || (result?.conflicts?.length ? result.conflicts.join("; ") : "")
    || "rewind failed";
}

export function rewindOutcome(result: RewindResultView): RewindResultView & { ok: true } {
  return { ...result, ok: true, tabId: result.tabId || result.tab?.id };
}

export async function settleRewindTarget(
  result: RewindResultView,
  adopt: (tab: TabMeta) => Promise<unknown>,
  waitUntilReady: (tabId: string) => Promise<unknown>,
): Promise<string | undefined> {
  const targetTabId = result.tab?.id || result.tabId;
  if (result.tab?.id) await adopt(result.tab);
  else if (targetTabId) await waitUntilReady(targetTabId);
  return targetTabId;
}

export function dispatchPartialRewindNotice(
  notice: string,
  sourceTabId: string,
  targetTabId: string | undefined,
  notify: (tabId: string, text: string) => void,
): void {
  if (!notice) return;
  notify(sourceTabId, notice);
  if (targetTabId && targetTabId !== sourceTabId) notify(targetTabId, notice);
}

export function undoCommittedRewind(sourceTabId: string, transactionId: string): Promise<RewindResultView> {
  return app.UndoRewindForTab(sourceTabId, transactionId);
}
