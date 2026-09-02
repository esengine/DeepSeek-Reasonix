import type {
  CloseMergedWorktreeTabRequest,
  TabMeta,
  WorktreeCleanupRequest,
  WorktreeMergeRequest,
} from "./types";

export function makeMockWorktreeMergeBindings(
  readTabs: () => TabMeta[],
  writeTabs: (tabs: TabMeta[]) => void,
) {
  return {
    async RegisterNavigationIntent(_token: string) {},
    async InspectWorktreeMerge(tabID: string) {
      const tabs = readTabs();
      const tab = tabs.find((candidate) => candidate.id === tabID) ?? tabs.find((candidate) => candidate.active);
      return {
        available: true,
        canMerge: true,
        alreadyMerged: false,
        worktreeRoot: tab?.workspaceRoot || "/mock/worktree",
        sourceRoot: "/mock/source",
        worktreeBranch: tab?.gitBranch || "reasonix/fork-mock",
        targetBranch: "main",
        createdHead: "mock-created-head",
        worktreeHead: "mock-worktree-head",
        worktreeStateToken: "mock-worktree-state-token",
        targetHead: "mock-target-head",
        aheadCount: 2,
        behindCount: 0,
        filesChanged: 1,
        insertions: 15,
        deletions: 3,
        changedFiles: ["feature.ts"],
        hasConflicts: false,
        conflictFiles: [],
        worktreeDirty: false,
        sourceDirty: false,
        blockers: [],
        cleanupBlockers: [{ code: "not_merged", message: "Worktree is not merged yet", paths: [] }],
      };
    },
    async MergeWorktreeBack(request: WorktreeMergeRequest) {
      const tabs = readTabs();
      const tab = tabs.find((candidate) => candidate.id === request.tabId) ?? tabs.find((candidate) => candidate.active);
      return {
        merged: true,
        alreadyMerged: false,
        recoveryRequired: false,
        sourceRoot: "/mock/source",
        targetBranch: "main",
        targetHead: "mock-commit-hash",
        mergedCommit: "mock-commit-hash",
        worktreeRoot: tab?.workspaceRoot || "/mock/worktree",
        worktreeBranch: tab?.gitBranch || "reasonix/delivery-mock",
        worktreeHead: "mock-worktree-head",
      };
    },
    async CloseMergedWorktreeTab(request: CloseMergedWorktreeTabRequest) {
      writeTabs(readTabs().filter((candidate) => candidate.id !== request.tabId));
      return { closed: true, idempotent: false };
    },
    async FinalizeWorktreeMerge(request: WorktreeCleanupRequest) {
      const tabs = readTabs().filter((candidate) => candidate.workspaceRoot !== request.worktreeRoot);
      if (tabs.length > 0) tabs[0].active = true;
      writeTabs(tabs);
      const allocationRoot = request.worktreeRoot.replace(/[\\/][^\\/]+$/, "");
      return {
        completed: false,
        worktreeRemoved: false,
        branchDeleted: false,
        recoveryRetained: true,
        recoveryRoot: `${allocationRoot}/.reasonix-cleanup/recovery-mock`,
        recoveryWorktreeRegistered: true,
        branchRetained: true,
        blockers: [],
      };
    },
  };
}
