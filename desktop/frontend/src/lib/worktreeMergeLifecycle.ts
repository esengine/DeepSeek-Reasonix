import type {
  CloseMergedWorktreeTabRequest,
  CloseMergedWorktreeTabResult,
  TabMeta,
  WorktreeCleanupRequest,
  WorktreeCleanupResult,
  WorktreeMergeResult,
} from "./types";

export interface WorktreeMergeLifecycleDeps {
  ensureSource(sourceRoot: string): Promise<TabMeta>;
  isNavigationCurrent(): boolean;
  seedSource(tab: TabMeta): void;
  listTabs(): Promise<TabMeta[]>;
  closeWorktree(request: CloseMergedWorktreeTabRequest): Promise<CloseMergedWorktreeTabResult>;
  finalize(request: WorktreeCleanupRequest): Promise<WorktreeCleanupResult>;
  onNavigationPreserved(): void;
  onCloseBlocked(): void;
}

export type WorktreeMergeLifecycleResult =
  | { phase: "preserved" }
  | { phase: "close-blocked" }
  | { phase: "finalized"; cleanup: WorktreeCleanupResult };

export async function runWorktreeMergeLifecycle(
  receipt: WorktreeMergeResult,
  worktreeTabId: string,
  navigationIntentToken: string,
  deps: WorktreeMergeLifecycleDeps,
): Promise<WorktreeMergeLifecycleResult> {
  if (!receipt.sourceRoot || !receipt.worktreeRoot || !receipt.targetBranch || !receipt.mergedCommit || !receipt.worktreeBranch || !receipt.worktreeHead) {
    throw new Error(receipt.error || "invalid merge receipt");
  }
  const preserveIfStale = (): boolean => {
    if (deps.isNavigationCurrent()) return false;
    deps.onNavigationPreserved();
    return true;
  };

  const sourceTab = await deps.ensureSource(receipt.sourceRoot);
  if (preserveIfStale()) return { phase: "preserved" };
  deps.seedSource(sourceTab);

  await deps.listTabs();
  if (preserveIfStale()) return { phase: "preserved" };
  const closed = await deps.closeWorktree({
    tabId: worktreeTabId,
    worktreeRoot: receipt.worktreeRoot,
    sourceTabId: sourceTab.id,
    sourceRoot: receipt.sourceRoot,
    navigationIntentToken,
  });
  if (!closed.closed) {
    deps.onCloseBlocked();
    return { phase: "close-blocked" };
  }
  if (preserveIfStale()) return { phase: "preserved" };

  const cleanup = await deps.finalize({
    worktreeRoot: receipt.worktreeRoot,
    sourceRoot: receipt.sourceRoot,
    targetBranch: receipt.targetBranch,
    mergedCommit: receipt.mergedCommit,
    worktreeBranch: receipt.worktreeBranch,
    worktreeHead: receipt.worktreeHead,
  });
  return { phase: "finalized", cleanup };
}
