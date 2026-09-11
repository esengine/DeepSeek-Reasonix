import { writeClipboardText } from "./clipboard";
import type { Translator } from "./i18n";
import type { ToastContextValue } from "./toast";
import type { WorktreeCleanupResult } from "./types";

export function showWorktreeCleanupNotice(
  cleanup: WorktreeCleanupResult,
  t: Translator,
  showToast: ToastContextValue["showToast"],
): void {
  const lateContent = cleanup.blockers.find((blocker) => blocker.code === "late_content_preserved");
  if (cleanup.recoveryRetained && cleanup.recoveryRoot) {
    const recoveryRoot = cleanup.recoveryRoot;
    const detail = cleanup.error || lateContent?.message;
    showToast(
      `${t("worktree.recoveryRetained", { path: recoveryRoot })}${detail ? ` ${detail}` : ""}`,
      detail ? "error" : "info",
      {
        durationMs: 12000,
        actionLabel: t("worktree.copyRecoveryPath"),
        onAction: () => {
          void writeClipboardText(recoveryRoot).then((copied) => {
            showToast(copied ? t("worktree.recoveryPathCopied") : t("diag.copyFailed"), copied ? "info" : "error");
          });
        },
      },
    );
    return;
  }
  if (cleanup.completed) {
    showToast(t("worktree.mergeAndCleanupDone"), "info");
    return;
  }
  showToast(cleanup.error || t("worktree.cleanupPreserved"), "error", { durationMs: 9000 });
}
