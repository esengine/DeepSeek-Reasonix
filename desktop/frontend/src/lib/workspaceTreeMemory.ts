import { createWorkspaceTreePersistenceScheduler } from "./workspaceTreePersistence";

export type WorkspaceTreeWidthMode = "manual" | "even";

export interface WorkspaceTreeMemorySnapshot {
  openDirs: Set<string>;
  visitId: number;
  selectedFilePath: string | null;
  selectedChangePath: string | null;
  treeWidth: number | null;
  treeWidthMode: WorkspaceTreeWidthMode;
  scrollTop: number;
  dockTreeWidth: number | null;
  dockPreviewWidth: number | null;
  recentPaths: string[];
}

type PersistedWorkspaceState = Omit<WorkspaceTreeMemorySnapshot, "openDirs" | "visitId"> & {
  openDirs: string[];
  updatedAt: number;
};

interface PersistedWorkspaceEnvelope {
  version: 2;
  projects: Array<{ key: string; state: PersistedWorkspaceState }>;
}

const STORAGE_KEY = "reasonix.workspaceState.v2";
const MAX_PERSISTED_PROJECTS = 50;
const MAX_RECENT_PATHS = 10;
const workspaceTreeMemory = new Map<string, WorkspaceTreeMemorySnapshot>();
let storageHydrated = false;
let activeWorkspaceTreeKey = "";
let workspaceTreeVisitSequence = 0;

function defaultSnapshot(visitId = 0): WorkspaceTreeMemorySnapshot {
  return {
    openDirs: new Set([""]),
    visitId,
    selectedFilePath: null,
    selectedChangePath: null,
    treeWidth: null,
    treeWidthMode: "manual",
    scrollTop: 0,
    dockTreeWidth: null,
    dockPreviewWidth: null,
    recentPaths: [],
  };
}

function validPath(value: unknown): string | null {
  return typeof value === "string" && value.length > 0 ? value : null;
}

function hydrateWorkspaceTreeMemory(): void {
  if (storageHydrated) return;
  storageHydrated = true;
  if (typeof localStorage === "undefined") return;
  try {
    const parsed = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "null") as Partial<PersistedWorkspaceEnvelope> | null;
    if (!parsed || parsed.version !== 2 || !Array.isArray(parsed.projects)) return;
    for (const project of parsed.projects) {
      if (!project || typeof project.key !== "string" || !project.state || typeof project.state !== "object") continue;
      const state = project.state as Partial<PersistedWorkspaceState>;
      const openDirs = Array.isArray(state.openDirs)
        ? state.openDirs.filter((path): path is string => typeof path === "string")
        : [""];
      workspaceTreeMemory.set(project.key, {
        openDirs: new Set(openDirs.length > 0 ? openDirs : [""]),
        visitId: 0,
        selectedFilePath: validPath(state.selectedFilePath),
        selectedChangePath: validPath(state.selectedChangePath),
        treeWidth: typeof state.treeWidth === "number" && Number.isFinite(state.treeWidth) && state.treeWidth > 0
          ? state.treeWidth
          : null,
        treeWidthMode: state.treeWidthMode === "even" ? "even" : "manual",
        scrollTop: typeof state.scrollTop === "number" && Number.isFinite(state.scrollTop) && state.scrollTop >= 0
          ? state.scrollTop
          : 0,
        dockTreeWidth: typeof state.dockTreeWidth === "number" && Number.isFinite(state.dockTreeWidth) && state.dockTreeWidth > 0
          ? state.dockTreeWidth
          : null,
        dockPreviewWidth: typeof state.dockPreviewWidth === "number" && Number.isFinite(state.dockPreviewWidth) && state.dockPreviewWidth > 0
          ? state.dockPreviewWidth
          : null,
        recentPaths: Array.isArray(state.recentPaths)
          ? state.recentPaths.filter((path): path is string => typeof path === "string").slice(0, MAX_RECENT_PATHS)
          : [],
      });
    }
  } catch {
    // Corrupt or newer storage must never prevent the workspace from opening.
  }
}

function persistWorkspaceTreeMemory(recentKey: string): void {
  if (typeof localStorage === "undefined") return;
  try {
    const now = Date.now();
    const entries = Array.from(workspaceTreeMemory.entries())
      .map(([key, snapshot]) => ({
        key,
        state: {
          openDirs: Array.from(snapshot.openDirs),
          selectedFilePath: snapshot.selectedFilePath,
          selectedChangePath: snapshot.selectedChangePath,
          treeWidth: snapshot.treeWidth,
          treeWidthMode: snapshot.treeWidthMode,
          scrollTop: snapshot.scrollTop,
          dockTreeWidth: snapshot.dockTreeWidth,
          dockPreviewWidth: snapshot.dockPreviewWidth,
          recentPaths: snapshot.recentPaths,
          updatedAt: key === recentKey ? now : 0,
        },
      }))
      .sort((left, right) => (right.key === recentKey ? 1 : 0) - (left.key === recentKey ? 1 : 0))
      .slice(0, MAX_PERSISTED_PROJECTS);
    const envelope: PersistedWorkspaceEnvelope = { version: 2, projects: entries };
    localStorage.setItem(STORAGE_KEY, JSON.stringify(envelope));
  } catch {
    // localStorage can be disabled or full; the in-memory state still works.
  }
}

const deferredScrollPersistence = createWorkspaceTreePersistenceScheduler(persistWorkspaceTreeMemory);

function cloneSnapshot(snapshot: WorkspaceTreeMemorySnapshot): WorkspaceTreeMemorySnapshot {
  return { ...snapshot, openDirs: new Set(snapshot.openDirs) };
}

export function workspaceTreeVisitId(memoryKey: string): number {
  if (activeWorkspaceTreeKey !== memoryKey) {
    activeWorkspaceTreeKey = memoryKey;
    workspaceTreeVisitSequence += 1;
  }
  return workspaceTreeVisitSequence;
}

export function readWorkspaceTreeMemory(memoryKey: string): WorkspaceTreeMemorySnapshot | null {
  hydrateWorkspaceTreeMemory();
  const snapshot = workspaceTreeMemory.get(memoryKey);
  return snapshot ? cloneSnapshot(snapshot) : null;
}

export function rememberWorkspaceTreeState(
  memoryKey: string,
  patch: Partial<Omit<WorkspaceTreeMemorySnapshot, "openDirs">> & { openDirs?: ReadonlySet<string> },
): void {
  hydrateWorkspaceTreeMemory();
  const current = workspaceTreeMemory.get(memoryKey) ?? defaultSnapshot();
  workspaceTreeMemory.set(memoryKey, {
    ...current,
    ...patch,
    openDirs: patch.openDirs ? new Set(patch.openDirs) : new Set(current.openDirs),
  });
  // An immediate state write already includes the latest in-memory scroll
  // position, so retire any trailing scroll write instead of duplicating it.
  deferredScrollPersistence.cancel();
  persistWorkspaceTreeMemory(memoryKey);
}

export function rememberWorkspaceTreeScroll(memoryKey: string, scrollTop: number): void {
  hydrateWorkspaceTreeMemory();
  const current = workspaceTreeMemory.get(memoryKey) ?? defaultSnapshot();
  workspaceTreeMemory.set(memoryKey, {
    ...current,
    openDirs: new Set(current.openDirs),
    scrollTop: Number.isFinite(scrollTop) && scrollTop >= 0 ? scrollTop : current.scrollTop,
  });
  deferredScrollPersistence.schedule(memoryKey);
}

export function flushWorkspaceTreeMemory(): void {
  deferredScrollPersistence.flush();
}

export function rememberWorkspaceTreeOpenDirs(memoryKey: string, openDirs: ReadonlySet<string>, visitId: number): void {
  rememberWorkspaceTreeState(memoryKey, { openDirs, visitId });
}

export function touchWorkspaceTreeVisit(memoryKey: string, visitId: number): void {
  rememberWorkspaceTreeState(memoryKey, { visitId });
}

export function resetWorkspaceTreeMemoryForTests(): void {
  deferredScrollPersistence.cancel();
  workspaceTreeMemory.clear();
  storageHydrated = false;
  activeWorkspaceTreeKey = "";
  workspaceTreeVisitSequence = 0;
  try {
    if (typeof localStorage !== "undefined") localStorage.removeItem(STORAGE_KEY);
  } catch {
    // Ignore storage cleanup failures in test environments.
  }
}
