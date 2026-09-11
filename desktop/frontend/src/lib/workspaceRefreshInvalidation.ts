import { useEffect, useRef } from "react";
import type { WorkspaceRefreshSnapshot } from "./workspaceRefreshStore";
import type { WorkspaceRefreshScheduler } from "./workspaceRefreshScheduler";

type SchedulerRef = { current: WorkspaceRefreshScheduler | null };
type Load = () => Promise<void> | void;

interface WorkspaceRefreshInvalidationOptions {
  commitHistoryOpen: boolean;
  filter: string;
  gitMetaSchedulerRef: SchedulerRef;
  loadChangeDetail: Load;
  loadDir: (dir: string) => unknown;
  loadGitHistory: Load;
  loadWorkspaceChanges: Load;
  open: boolean;
  openDirsRef: { current: Set<string> };
  refreshSelected: () => unknown;
  selectedPath: string | null;
  setSearchResults: (value: null) => void;
  viewMode: string;
  workingTreeSchedulerRef: SchedulerRef;
  workspaceRefresh: WorkspaceRefreshSnapshot;
  workspaceScopeKey: string;
}

export interface WorkspaceRefreshActions {
  forceVisible: boolean;
  content: boolean;
  tree: boolean;
  workingTree: boolean;
  gitMeta: boolean;
}

export function workspaceRefreshActions(
  previous: WorkspaceRefreshSnapshot["revisions"],
  next: WorkspaceRefreshSnapshot,
  commitHistoryOpen: boolean,
): WorkspaceRefreshActions {
  // Focus reconciliation is also the recovery path for authorized paths that
  // intentionally live outside the watched root. Watcher failures publish an
  // all-paths degraded event even when no resource revision could be advanced.
  const forceVisible = next.allPaths && (next.source === "reconcile" || next.watchState !== "active");
  return {
    forceVisible,
    content: next.revisions.content > previous.content || forceVisible,
    tree: next.revisions.tree > previous.tree || forceVisible,
    workingTree: next.revisions.workingTree > previous.workingTree || next.revisions.session > previous.session || forceVisible,
    gitMeta: next.revisions.gitMeta > previous.gitMeta || (forceVisible && commitHistoryOpen),
  };
}

export function workspaceRefreshFallbackSequence(snapshot: WorkspaceRefreshSnapshot): number {
  return snapshot.allPaths && (snapshot.source === "reconcile" || snapshot.watchState !== "active") ? snapshot.sequence : 0;
}

function parentDirs(path: string): string[] {
  const parts = path.split("/").filter(Boolean);
  const dirs = [""];
  let acc = "";
  for (let i = 0; i < parts.length - 1; i++) {
    acc += `${parts[i]}/`;
    dirs.push(acc);
  }
  return dirs;
}

export function useWorkspaceRefreshInvalidation({
  commitHistoryOpen,
  filter,
  gitMetaSchedulerRef,
  loadChangeDetail,
  loadDir,
  loadGitHistory,
  loadWorkspaceChanges,
  open,
  openDirsRef,
  refreshSelected,
  selectedPath,
  setSearchResults,
  viewMode,
  workingTreeSchedulerRef,
  workspaceRefresh,
  workspaceScopeKey,
}: WorkspaceRefreshInvalidationOptions): void {
  const lastSequenceRef = useRef(workspaceRefresh.sequence);
  const lastRevisionsRef = useRef(workspaceRefresh.revisions);
  const lastScopeRef = useRef(workspaceScopeKey);

  useEffect(() => {
    if (!open) return;
    if (lastScopeRef.current !== workspaceScopeKey) {
      lastScopeRef.current = workspaceScopeKey;
      lastSequenceRef.current = workspaceRefresh.sequence;
      lastRevisionsRef.current = workspaceRefresh.revisions;
      return;
    }
    if (lastSequenceRef.current === workspaceRefresh.sequence) return;
    const previous = lastRevisionsRef.current;
    lastSequenceRef.current = workspaceRefresh.sequence;
    lastRevisionsRef.current = workspaceRefresh.revisions;
    const { changes } = workspaceRefresh;
    const actions = workspaceRefreshActions(previous, workspaceRefresh, commitHistoryOpen);
    const affectsSelected = workspaceRefresh.allPaths || !selectedPath || changes.some((change) =>
      change.path === selectedPath || change.oldPath === selectedPath || selectedPath.startsWith(`${change.path}/`),
    );
    if (actions.content && (actions.forceVisible || affectsSelected) && selectedPath) void refreshSelected();
    if (actions.tree && (actions.forceVisible || workspaceRefresh.allPaths || changes.length > 0)) {
      const affectedDirs = workspaceRefresh.allPaths
        ? openDirsRef.current
        : new Set(changes.flatMap((change) => [change.path, change.oldPath].filter(Boolean).flatMap((path) => parentDirs(path as string))));
      for (const dir of affectedDirs) {
        if (openDirsRef.current.has(dir)) void loadDir(dir);
      }
      if (filter.trim()) setSearchResults(null);
    }
    if (viewMode === "changed") {
      if (actions.workingTree) {
        workingTreeSchedulerRef.current?.trigger(async () => {
          await Promise.all([loadWorkspaceChanges(), selectedPath ? loadChangeDetail() : Promise.resolve()]);
        });
      }
      if (actions.gitMeta) gitMetaSchedulerRef.current?.trigger(loadGitHistory);
    }
  }, [
    filter,
    commitHistoryOpen,
    gitMetaSchedulerRef,
    loadChangeDetail,
    loadDir,
    loadGitHistory,
    loadWorkspaceChanges,
    open,
    openDirsRef,
    refreshSelected,
    selectedPath,
    setSearchResults,
    viewMode,
    workingTreeSchedulerRef,
    workspaceRefresh,
    workspaceScopeKey,
  ]);
}
