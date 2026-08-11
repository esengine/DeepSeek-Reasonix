import { useCallback } from "react";
import { useConfirmDialog } from "../components/ConfirmDialog";
import { app } from "./bridge";
import { t } from "./i18n";
import type { HistoryMessage } from "./types";

// estimateHistoryTokens approximates prompt tokens for a display-message list
// (chars × 0.25, matching the agent's fallback calibration). It is not a
// provider usage figure; the resume guard only uses it to decide whether a
// restore needs confirmation.
export function estimateHistoryTokens(messages: readonly HistoryMessage[]): number {
  let chars = 0;
  for (const m of messages) {
    chars += m.content.length + (m.detail?.length ?? 0) + (m.reasoning?.length ?? 0) + (m.summary?.length ?? 0);
    for (const tc of m.toolCalls ?? []) {
      chars += tc.name.length + tc.arguments.length;
    }
  }
  return Math.round(chars * 0.25);
}

// useResumeGuard backs the resume-flow confirmation: restoring a session whose
// estimated prompt size already exceeds the compact threshold would trigger an
// immediate cleanup pass, so ask first. The caller estimates from the read-only
// preview and must invoke this before the mutating resume call, so cancelling
// leaves the session untouched.
export function useResumeGuard() {
  const { confirm: confirmDialog, dialog: resumeGuardDialog } = useConfirmDialog();
  const confirmOverThresholdResume = useCallback(async (estimatedTokens: number, tabId: string): Promise<boolean> => {
    if (!estimatedTokens || estimatedTokens <= 0) return true;
    const context = await app.ContextUsageForTab(tabId).catch(() => undefined);
    const compactRatio = context?.compactRatio ?? 0;
    const compactTokens = context && context.window > 0 && compactRatio > 0
      ? Math.round(context.window * compactRatio)
      : 0;
    if (compactTokens <= 0 || estimatedTokens < compactTokens) return true;
    return confirmDialog({
      title: t("history.overThresholdResumeTitle"),
      message: t("history.overThresholdResumeMessage", {
        used: estimatedTokens.toLocaleString(),
        threshold: compactTokens.toLocaleString(),
      }),
      confirmLabel: t("history.overThresholdResumeConfirm"),
      cancelLabel: t("common.cancel"),
      tone: "danger",
    });
  }, [confirmDialog]);
  return { confirmOverThresholdResume, resumeGuardDialog };
}
