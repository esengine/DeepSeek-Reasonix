import { useCommittedCommand } from "../lib/useCommittedCommand";
import { runWorktreeMergeLifecycle } from "../lib/worktreeMergeLifecycle";
import type { WorktreeMergeResult } from "../lib/types";
import type { Translator } from "../lib/i18n";

export function useWorktreeMergeCommands(input: {
  worktreeTabId: string | null;
  singleSurfaceLayout: boolean;
  noteNavigationIntent: () => number;
  registeredNavigationIntent: (seq: number) => Promise<string | null>;
  isNavigationIntentCurrent: (seq: number) => boolean;
  ensureBlankSurface: (scope: string, workspace: string, seq: number) => Promise<any>;
  ensureBlankTab: (scope: string, workspace: string, seq: number) => Promise<any>;
  seedSource: (tab: any) => void;
  listTabs: () => Promise<any[]>;
  closeWorktree: (request: any) => Promise<any>;
  finalize: (request: any) => Promise<any>;
  showToast: (message: string, level: "error", options?: { durationMs?: number }) => void;
  t: Translator;
  showCleanup: (cleanup: any, t: Translator) => void;
}) {
  const handleWorktreeMerged = useCommittedCommand(async (result: WorktreeMergeResult) => {
    const tabToClose = input.worktreeTabId;
    if (!tabToClose || !result.sourceRoot || !result.worktreeRoot || !result.targetBranch || !result.mergedCommit || !result.worktreeBranch || !result.worktreeHead) {
      throw new Error(result.error || input.t("worktree.mergeReceiptInvalid"));
    }
    const seq = input.noteNavigationIntent();
    try {
      const token = await input.registeredNavigationIntent(seq);
      if (!token || !input.isNavigationIntentCurrent(seq)) {
        input.showToast(input.t("worktree.navigationChangedPreserved"), "error", { durationMs: 9000 });
        return;
      }
      const lifecycle = await runWorktreeMergeLifecycle(result, tabToClose, token, {
        ensureSource: (root) => input.singleSurfaceLayout
          ? input.ensureBlankSurface("project", root, seq)
          : input.ensureBlankTab("project", root, seq),
        isNavigationCurrent: () => input.isNavigationIntentCurrent(seq),
        seedSource: input.seedSource,
        listTabs: input.listTabs,
        closeWorktree: input.closeWorktree,
        finalize: input.finalize,
        onNavigationPreserved: () => input.showToast(input.t("worktree.navigationChangedPreserved"), "error", { durationMs: 9000 }),
        onCloseBlocked: () => input.showToast(input.t("worktree.cleanupViewBlocked"), "error", { durationMs: 8000 }),
      });
      if (lifecycle.phase === "finalized") input.showCleanup(lifecycle.cleanup, input.t);
    } catch (error) {
      input.showToast(`${input.t("worktree.mergeDoneCleanupFailed")} ${error instanceof Error ? error.message : String(error)}`, "error", { durationMs: 9000 });
    }
  });
  return { handleWorktreeMerged };
}
