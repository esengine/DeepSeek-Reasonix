import { useCallback, useEffect, useSyncExternalStore } from "react";
import { app, onEvent } from "./bridge";
import { tabMetaFallbackDelay } from "./tabMetaRefresh";
import type { WireWorkspaceChanged, WorkspaceRevisions, WorkspaceWatchState } from "./types";

export interface WorkspaceRefreshSnapshot {
  revisions: WorkspaceRevisions;
  changes: WireWorkspaceChanged["changes"];
  allPaths: boolean;
  source: WireWorkspaceChanged["source"];
  watchState: WorkspaceWatchState;
  sequence: number;
}

const zeroRevisions = (): WorkspaceRevisions => ({ content: 0, tree: 0, workingTree: 0, gitMeta: 0, session: 0 });
const EMPTY_SNAPSHOT: WorkspaceRefreshSnapshot = {
  revisions: zeroRevisions(), changes: [], allPaths: false, source: "reconcile", watchState: "unavailable", sequence: 0,
};
const emptySnapshot = (): WorkspaceRefreshSnapshot => EMPTY_SNAPSHOT;

const snapshots = new Map<string, WorkspaceRefreshSnapshot>();
const listeners = new Map<string, Set<() => void>>();
const activeScopeByTab = new Map<string, string>();

function key(tabId: string, scopeKey: string): string {
  return `${tabId}\u0000${scopeKey}`;
}

function notify(tabId: string, scopeKey?: string): void {
  const keys = scopeKey ? [key(tabId, scopeKey)] : Array.from(listeners.keys()).filter((candidate) => candidate.startsWith(`${tabId}\u0000`));
  for (const candidate of keys) listeners.get(candidate)?.forEach((listener) => listener());
}

function replace(tabId: string, scopeKey: string, next: WorkspaceRefreshSnapshot): void {
  const k = key(tabId, scopeKey);
  snapshots.set(k, next);
  notify(tabId, scopeKey);
}

function revisionsOlder(current: WorkspaceRevisions, previous: WorkspaceRevisions): boolean {
  return current.content < previous.content || current.tree < previous.tree || current.workingTree < previous.workingTree || current.gitMeta < previous.gitMeta || current.session < previous.session;
}

function acceptEvent(tabId: string, event: WireWorkspaceChanged): void {
  const scopeKey = activeScopeByTab.get(tabId);
  if (!scopeKey) return;
  const snapshotKey = key(tabId, scopeKey);
  if (!listeners.has(snapshotKey)) return;
  const previous = snapshots.get(snapshotKey) ?? emptySnapshot();
  const current = event.revisions;
  if (revisionsOlder(current, previous.revisions)) return;
  const next: WorkspaceRefreshSnapshot = { ...event, sequence: previous.sequence + 1, changes: Array.isArray(event.changes) ? event.changes : [] };
  snapshots.set(snapshotKey, next);
  notify(tabId, scopeKey);
}

let stopEvents: (() => void) | null = null;
function ensureEvents(): void {
  if (stopEvents) return;
  stopEvents = onEvent((event) => {
    if (event.kind === "workspace_changed" && event.tabId && event.workspace) {
      acceptEvent(event.tabId, event.workspace);
    }
  });
}

async function workspaceRevisionForTab(tabId: string) {
  const binding = app.WorkspaceRevisionForTab;
  if (typeof binding !== "function") return undefined;
  return binding(tabId);
}

type WorkspaceRevisionResult = Awaited<ReturnType<typeof workspaceRevisionForTab>>;

function applyWorkspaceReconciliation(tabId: string, scopeKey: string, result: WorkspaceRevisionResult, forceVisible: boolean): void {
  if (!result || activeScopeByTab.get(tabId) !== scopeKey) return;
  const snapshotKey = key(tabId, scopeKey);
  if (!listeners.has(snapshotKey)) return;
  const previous = snapshots.get(snapshotKey) ?? EMPTY_SNAPSHOT;
  let revisions = result.revisions ?? zeroRevisions();
  let watchState = result.watchState ?? "unavailable";
  // An event may advance the store while this bridge request is in flight.
  // Never let the older reconciliation response move a scope backwards. An
  // explicit focus fallback still invalidates visible resources, but retains
  // the newer event snapshot's revisions and watcher state.
  if (revisionsOlder(revisions, previous.revisions)) {
    if (!forceVisible) return;
    revisions = previous.revisions;
    watchState = previous.watchState;
  }
  const changed = revisionsOlder(previous.revisions, revisions);
  if (!forceVisible && !changed && watchState === previous.watchState) return;
  // Reconciliation is also the bounded fallback for degraded watchers and
  // authorized external paths that are intentionally not watched. Re-issue
  // an all-paths invalidation on explicit focus even when the hub revision is
  // unchanged, while ordinary mount/runtime checks stay revision-driven.
  replace(tabId, scopeKey, {
    revisions,
    changes: [],
    allPaths: true,
    source: "reconcile",
    watchState,
    sequence: previous.sequence + 1,
  });
}

// Kept as an explicit lifecycle hook for tests and hot-reload hosts. Production
// keeps the subscription for the lifetime of the webview.
export function disposeWorkspaceRefreshStore(): void {
  stopEvents?.();
  stopEvents = null;
  snapshots.clear();
  listeners.clear();
  activeScopeByTab.clear();
}

export function useWorkspaceRefresh(tabId: string, scopeKey: string, enabled: boolean): WorkspaceRefreshSnapshot {
  const snapshotKey = key(tabId, scopeKey);
  const subscribe = useCallback((listener: () => void) => {
    let set = listeners.get(snapshotKey);
    if (!set) {
      set = new Set();
      listeners.set(snapshotKey, set);
    }
    set.add(listener);
    return () => {
      set?.delete(listener);
      if (set?.size === 0) {
        listeners.delete(snapshotKey);
        snapshots.delete(snapshotKey);
        if (activeScopeByTab.get(tabId) === scopeKey) activeScopeByTab.delete(tabId);
      }
    };
  }, [snapshotKey]);
  const getSnapshot = useCallback(() => snapshots.get(snapshotKey) ?? emptySnapshot(), [snapshotKey]);
  const snapshot = useSyncExternalStore(subscribe, getSnapshot, getSnapshot);

  useEffect(() => {
    if (!enabled) return;
    activeScopeByTab.set(tabId, scopeKey);
    ensureEvents();
    let live = true;
    workspaceRevisionForTab(tabId).then((result) => {
      if (!live || !result) return;
      const previous = getSnapshot();
      const revisions = result.revisions ?? zeroRevisions();
      if (revisionsOlder(revisions, previous.revisions)) return;
      replace(tabId, scopeKey, {
        revisions,
        changes: [],
        allPaths: true,
        source: "reconcile",
        watchState: result.watchState ?? "unavailable",
        sequence: previous.sequence + 1,
      });
    }).catch(() => undefined);
    return () => {
      live = false;
      if (activeScopeByTab.get(tabId) === scopeKey) activeScopeByTab.delete(tabId);
    };
  }, [enabled, scopeKey, tabId]);

  return snapshot;
}

export function markWorkspaceRefresh(tabId: string, scopeKey: string): void {
  const previous = snapshots.get(key(tabId, scopeKey)) ?? emptySnapshot();
  replace(tabId, scopeKey, { ...previous, allPaths: true, source: "reconcile", sequence: previous.sequence + 1 });
}

export async function reconcileWorkspaceRefresh(
  tabId: string,
  scopeKey: string,
  options?: { forceVisible?: boolean },
): Promise<void> {
  try {
    activeScopeByTab.set(tabId, scopeKey);
    const result = await workspaceRevisionForTab(tabId);
    applyWorkspaceReconciliation(tabId, scopeKey, result, options?.forceVisible === true);
  } catch {
    // A transient runtime rebuild must not erase the last good snapshot.
  }
}

export function startWorkspaceFocusReconciliation(
  activeTabId: string | undefined,
  workspaceScopeKey: string,
  refreshTabMetas: () => unknown,
): () => void {
  let cancelled = false;
  let timer: number | undefined;
  let focusTimer: number | undefined;
  const schedule = () => {
    if (cancelled) return;
    timer = window.setTimeout(() => {
      void refreshTabMetas();
      schedule();
    }, tabMetaFallbackDelay(document.visibilityState));
  };
  const refreshAndSchedule = (forceVisible = false) => {
    if (timer !== undefined) window.clearTimeout(timer);
    timer = undefined;
    void refreshTabMetas();
    if (activeTabId) void reconcileWorkspaceRefresh(activeTabId, workspaceScopeKey, { forceVisible });
    schedule();
  };
  const requestVisibleRefresh = () => {
    if (cancelled || focusTimer !== undefined) return;
    // Focus and visibility commonly fire together; collapse the pair into
    // one bounded reconciliation for the foreground transition.
    focusTimer = window.setTimeout(() => {
      focusTimer = undefined;
      refreshAndSchedule(true);
    }, 0);
  };
  const onVisibilityChange = () => {
    if (document.visibilityState === "visible") requestVisibleRefresh();
    else {
      if (timer !== undefined) window.clearTimeout(timer);
      schedule();
    }
  };
  const onFocus = () => {
    if (document.visibilityState === "visible") requestVisibleRefresh();
  };
  refreshAndSchedule(false);
  document.addEventListener("visibilitychange", onVisibilityChange);
  window.addEventListener("focus", onFocus);
  return () => {
    cancelled = true;
    if (timer !== undefined) window.clearTimeout(timer);
    if (focusTimer !== undefined) window.clearTimeout(focusTimer);
    document.removeEventListener("visibilitychange", onVisibilityChange);
    window.removeEventListener("focus", onFocus);
  };
}

export default startWorkspaceFocusReconciliation;

// Deterministic seams for the store's scope and monotonicity contracts.
export function resetWorkspaceRefreshStoreForTests(): void {
  disposeWorkspaceRefreshStore();
}

export function activateWorkspaceRefreshScopeForTests(tabId: string, scopeKey: string): void {
  activeScopeByTab.set(tabId, scopeKey);
  listeners.set(key(tabId, scopeKey), new Set());
}

export function acceptWorkspaceRefreshForTests(tabId: string, event: WireWorkspaceChanged): void {
  acceptEvent(tabId, event);
}

export function reconcileWorkspaceRefreshForTests(
  tabId: string,
  scopeKey: string,
  result: NonNullable<WorkspaceRevisionResult>,
  forceVisible = false,
): void {
  activeScopeByTab.set(tabId, scopeKey);
  applyWorkspaceReconciliation(tabId, scopeKey, result, forceVisible);
}

export function workspaceRefreshSnapshotForTests(tabId: string, scopeKey: string): WorkspaceRefreshSnapshot {
  return snapshots.get(key(tabId, scopeKey)) ?? emptySnapshot();
}
