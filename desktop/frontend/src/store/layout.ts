// layout owns the desktop shell's geometry state — sidebar + right-dock widths
// and the sidebar collapse flag — as a selectable store rather than App-local
// useState. Components read a single slice via selector (only that slice
// re-renders), with no prop drilling. The geometry constants, clamps, and the
// localStorage-backed load/save helpers live here too: they are layout-domain
// knowledge that belongs with the store, and keeping them here lets the store
// initialize itself from persisted state at module load without depending on App.
//
// Persistence behavior is intentionally unchanged from the previous App-local
// implementation: the store's setters are pure (state only), and callers keep
// invoking the exported save* helpers exactly where they did before, so the
// on-disk localStorage schema and write timing are byte-identical.

import type { Dispatch, SetStateAction } from "react";
import { create } from "zustand";

import { loadLayoutSize, loadOptionalLayoutSize, saveLayoutSize } from "../lib/layoutPreferences";

import { applySetState } from "./setState";

const SIDEBAR_COLLAPSED_KEY = "reasonix.sidebar.collapsed";
const SIDEBAR_DEFAULT_WIDTH = 264;
export const SIDEBAR_MIN_WIDTH = 264;
export const CREATION_SIDEBAR_MIN_WIDTH = 236;
// Creation keeps the expanded rail at the narrow floor by default.
export const CREATION_SIDEBAR_DEFAULT_WIDTH = CREATION_SIDEBAR_MIN_WIDTH;
export const SIDEBAR_MAX_WIDTH = 300;
const SIDEBAR_VIEWPORT_RATIO = 0.18;

const RIGHT_DOCK_TREE_DEFAULT_WIDTH = 300;
export const RIGHT_DOCK_TREE_MIN_WIDTH = 300;
// Creation file-tree dock stays tighter than classic 300. With Creation's
// narrower Windows caption strip (~108px), 252 is enough for icon+label tabs.
export const CREATION_RIGHT_DOCK_TREE_MIN_WIDTH = 252;
export const CREATION_RIGHT_DOCK_TREE_DEFAULT_WIDTH = CREATION_RIGHT_DOCK_TREE_MIN_WIDTH;
export const RIGHT_DOCK_TREE_MAX_WIDTH = 560;
export const RIGHT_DOCK_PREVIEW_DEFAULT_WIDTH = 660;
export const RIGHT_DOCK_PREVIEW_MIN_WIDTH = 420;
export const RIGHT_DOCK_MIN_RENDER_WIDTH = 280;
// Creation tree mode may render below the classic 280 floor when the viewport squeezes.
export const CREATION_RIGHT_DOCK_MIN_RENDER_WIDTH = 236;
export const RIGHT_DOCK_MAX_WIDTH = 860;
const WORKSPACE_PANEL_DEFAULT_OPEN = true;

export function clampSidebarWidth(width: number): number {
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, Math.round(width)));
}

export function clampCreationSidebarWidth(width: number): number {
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(CREATION_SIDEBAR_MIN_WIDTH, Math.round(width)));
}

function clampStoredSidebarWidth(width: number): number {
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(CREATION_SIDEBAR_MIN_WIDTH, Math.round(width)));
}

export function clampRightDockPreviewWidth(width: number): number {
  return Math.min(RIGHT_DOCK_MAX_WIDTH, Math.max(RIGHT_DOCK_PREVIEW_MIN_WIDTH, Math.round(width)));
}

export function clampRightDockTreeWidth(width: number): number {
  return Math.min(RIGHT_DOCK_TREE_MAX_WIDTH, Math.max(RIGHT_DOCK_TREE_MIN_WIDTH, Math.round(width)));
}

export function clampCreationRightDockTreeWidth(width: number): number {
  return Math.min(RIGHT_DOCK_TREE_MAX_WIDTH, Math.max(CREATION_RIGHT_DOCK_TREE_MIN_WIDTH, Math.round(width)));
}

function clampStoredRightDockTreeWidth(width: number): number {
  return Math.min(RIGHT_DOCK_TREE_MAX_WIDTH, Math.max(CREATION_RIGHT_DOCK_TREE_MIN_WIDTH, Math.round(width)));
}

export function defaultSidebarWidth(): number {
  if (typeof window !== "undefined") {
    return clampSidebarWidth(window.innerWidth * SIDEBAR_VIEWPORT_RATIO);
  }
  return SIDEBAR_DEFAULT_WIDTH;
}

export function defaultCreationSidebarWidth(): number {
  return CREATION_SIDEBAR_DEFAULT_WIDTH;
}

export function defaultRightDockTreeWidth(): number {
  return RIGHT_DOCK_TREE_DEFAULT_WIDTH;
}

export function defaultCreationRightDockTreeWidth(): number {
  return CREATION_RIGHT_DOCK_TREE_DEFAULT_WIDTH;
}

function loadSidebarCollapsed(): boolean {
  if (typeof window === "undefined") return false;
  try {
    return window.localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === "1";
  } catch {
    return false;
  }
}

export function saveSidebarCollapsed(collapsed: boolean): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(SIDEBAR_COLLAPSED_KEY, collapsed ? "1" : "0");
  } catch {
    /* ignore storage failures */
  }
}

function loadSidebarWidth(): number {
  return loadLayoutSize("sidebarWidthGraphite", defaultSidebarWidth(), clampStoredSidebarWidth);
}

export function saveSidebarWidth(width: number): void {
  saveLayoutSize("sidebarWidthGraphite", width, clampStoredSidebarWidth);
}

function loadRightDockTreeWidth(): number {
  return loadLayoutSize("rightDockTreeWidth", defaultRightDockTreeWidth(), clampStoredRightDockTreeWidth);
}

export function saveRightDockTreeWidth(width: number): void {
  saveLayoutSize("rightDockTreeWidth", width, clampStoredRightDockTreeWidth);
}

function loadRightDockPreviewWidth(): number {
  return loadLayoutSize("rightDockPreviewWidth", RIGHT_DOCK_PREVIEW_DEFAULT_WIDTH, clampRightDockPreviewWidth);
}

export function saveRightDockPreviewWidth(width: number): void {
  saveLayoutSize("rightDockPreviewWidth", width, clampRightDockPreviewWidth);
}

// rightDockMode selects what the right dock shows; the workspace-panel flags are
// its open/maximized/preview layout configuration. None of these four are
// persisted — they reset to the defaults below on launch, exactly as the prior
// App-local useState did. (The truly view-local interaction ephemera — resize
// drag flags, button-press animation flags, measured footer height, viewport
// width — deliberately stay as useState in App.tsx; they have no cross-component
// readers and don't belong in shared state.)
export type RightDockMode = "context" | "files" | "changed";

export type LayoutState = {
  sidebarCollapsed: boolean;
  sidebarWidth: number;
  rightDockTreeWidth: number;
  rightDockPreviewWidth: number;
  workspacePanelOpen: boolean;
  workspacePanelMaximized: boolean;
  workspacePreviewActive: boolean;
  rightDockMode: RightDockMode;
  setSidebarCollapsed: (collapsed: boolean) => void;
  setSidebarWidth: (width: number) => void;
  setRightDockTreeWidth: (width: number) => void;
  setRightDockPreviewWidth: (width: number) => void;
  setWorkspacePanelOpen: Dispatch<SetStateAction<boolean>>;
  setWorkspacePanelMaximized: Dispatch<SetStateAction<boolean>>;
  setWorkspacePreviewActive: Dispatch<SetStateAction<boolean>>;
  setRightDockMode: Dispatch<SetStateAction<RightDockMode>>;
};

export const useLayoutStore = create<LayoutState>((set) => ({
  sidebarCollapsed: loadSidebarCollapsed(),
  sidebarWidth: loadSidebarWidth(),
  rightDockTreeWidth: loadRightDockTreeWidth(),
  rightDockPreviewWidth: loadRightDockPreviewWidth(),
  workspacePanelOpen: WORKSPACE_PANEL_DEFAULT_OPEN,
  workspacePanelMaximized: false,
  workspacePreviewActive: false,
  rightDockMode: "context",
  setSidebarCollapsed: (collapsed) => set({ sidebarCollapsed: collapsed }),
  setSidebarWidth: (width) => set({ sidebarWidth: width }),
  setRightDockTreeWidth: (width) => set({ rightDockTreeWidth: width }),
  setRightDockPreviewWidth: (width) => set({ rightDockPreviewWidth: width }),
  setWorkspacePanelOpen: (update) => set((s) => ({ workspacePanelOpen: applySetState(s.workspacePanelOpen, update) })),
  setWorkspacePanelMaximized: (update) => set((s) => ({ workspacePanelMaximized: applySetState(s.workspacePanelMaximized, update) })),
  setWorkspacePreviewActive: (update) => set((s) => ({ workspacePreviewActive: applySetState(s.workspacePreviewActive, update) })),
  setRightDockMode: (update) => set((s) => ({ rightDockMode: applySetState(s.rightDockMode, update) })),
}));

export function applyLayoutStyleDefaults(style: "classic" | "workbench" | "creation"): void {
  const state = useLayoutStore.getState();
  if (loadOptionalLayoutSize("sidebarWidthGraphite", clampStoredSidebarWidth) === null) {
    state.setSidebarWidth(style === "creation" ? defaultCreationSidebarWidth() : defaultSidebarWidth());
  }
  if (loadOptionalLayoutSize("rightDockTreeWidth", clampStoredRightDockTreeWidth) === null) {
    state.setRightDockTreeWidth(style === "creation" ? defaultCreationRightDockTreeWidth() : defaultRightDockTreeWidth());
  }
}
