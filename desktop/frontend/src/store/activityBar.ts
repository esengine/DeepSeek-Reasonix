// activityBar owns the right dock's activity-bar + tab-container state: the
// set of open tabs, the active one, whether the tab container is expanded,
// and the + add-menu open flag. Panel contents stay in App (they need its
// props); this store only tracks which tab is open so the dock can switch
// between them.
//
// The dock follows the plan's interaction model: by default only the 48px
// activity bar is visible; opening an entry expands the tab container; closing
// the last tab collapses it back to the bar. `activityBarOpen` records that
// expanded state (true while ≥1 tab is open).
//
// Tabs are persisted to localStorage (a window-level preference, not a
// per-project one), so a restart restores the same open tabs. The add menu
// stays session-local.

import { create } from "zustand";

export type TabType = "file" | "changed" | "terminal" | "browser" | "remote" | "context" | "instructions";

export interface TabItem {
  id: string;
  type: TabType;
  label: string;
  meta?: Record<string, unknown>;
}

const STORAGE_KEY = "reasonix.dock.tabs";

// Tabs are scoped per project (workspace root), so switching projects shows
// each one's own open tabs. A root of "" falls back to the legacy global key.
let workspaceRoot = "";
function storageKey(): string {
  return workspaceRoot ? `${STORAGE_KEY}.${workspaceRoot}` : STORAGE_KEY;
}

// tabSeq must not collide with ids restored from localStorage (which may
// contain dock-tab-N from a previous session). Seed it past the highest
// persisted id so fresh tabs never duplicate an existing key.
let tabSeq = 0;
function seedTabSeq(restoredTabs: TabItem[]): void {
  for (const tab of restoredTabs) {
    const match = /^dock-tab-(\d+)$/.exec(tab.id);
    if (match) tabSeq = Math.max(tabSeq, Number(match[1]));
  }
}
function nextTabId(): string {
  tabSeq += 1;
  return `dock-tab-${tabSeq}`;
}

function loadTabs(): { tabs: TabItem[]; activeTabId: string | null } {
  if (typeof window === "undefined") return { tabs: [], activeTabId: null };
  try {
    const raw = window.localStorage.getItem(storageKey());
    if (!raw) return { tabs: [], activeTabId: null };
    const parsed = JSON.parse(raw) as { tabs?: TabItem[]; activeTabId?: string | null };
    const tabs = Array.isArray(parsed.tabs) ? parsed.tabs : [];
    // Drop structurally invalid entries and duplicate ids (a persisted
    // snapshot may contain them from an earlier bug); duplicate ids would
    // make React report "two children with the same key".
    const seen = new Set<string>();
    const valid = tabs.filter((tab) => {
      if (!tab || typeof tab.id !== "string" || typeof tab.type !== "string") return false;
      if (seen.has(tab.id)) return false;
      seen.add(tab.id);
      return true;
    });
    seedTabSeq(valid);
    return { tabs: valid, activeTabId: valid.some((tab) => tab.id === parsed.activeTabId) ? parsed.activeTabId ?? null : valid[valid.length - 1]?.id ?? null };
  } catch {
    return { tabs: [], activeTabId: null };
  }
}

function persist(tabs: TabItem[], activeTabId: string | null): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(storageKey(), JSON.stringify({ tabs, activeTabId }));
  } catch {
    /* ignore storage failures */
  }
}

const initial = loadTabs();

export type ActivityBarState = {
  tabs: TabItem[];
  activeTabId: string | null;
  /** True while the tab container is expanded (dock shows the panel, not just
   *  the 48px activity bar). Independent of tabs: collapsing via the toggle
   *  keeps the tabs so re-expanding restores them. */
  activityBarOpen: boolean;
  addMenuOpen: boolean;
  /** Open the entry's default tab, switching to it when one of that type exists. */
  openEntry: (type: TabType, label: string, meta?: Record<string, unknown>) => void;
  /** Append a new tab of the given type and activate it. */
  addTab: (type: TabType, label: string, meta?: Record<string, unknown>) => void;
  /** Update a tab's label and meta (e.g. a file tab whose preview file changes
   *  its title from 文件 to the file name). */
  updateTab: (tabId: string, label: string, meta?: Record<string, unknown>) => void;
  closeTab: (tabId: string) => void;
  activateTab: (tabId: string) => void;
  /** Move a tab so it lands on the left/right side of another tab. */
  moveTab: (fromId: string, toId: string, side: "left" | "right") => void;
  /** Collapse/expand the tab container without touching the tab list. */
  setActivityBarOpen: (open: boolean) => void;
  setAddMenuOpen: (open: boolean) => void;
  /** Switch the active project (workspace root): reloads that project's own
   *  persisted tabs. A root of "" falls back to the legacy global tabs. */
  setWorkspaceRoot: (root: string) => void;
};

export const useActivityBarStore = create<ActivityBarState>((set) => ({
  tabs: initial.tabs,
  activeTabId: initial.activeTabId,
  activityBarOpen: initial.tabs.length > 0,
  addMenuOpen: false,
  openEntry: (type, label, meta) =>
    set((state) => {
      const existing = state.tabs.find((tab) => tab.type === type);
      if (existing) {
        persist(state.tabs, existing.id);
        return { activeTabId: existing.id, activityBarOpen: true };
      }
      const tab: TabItem = { id: nextTabId(), type, label, meta };
      const tabs = [...state.tabs, tab];
      persist(tabs, tab.id);
      return { tabs, activeTabId: tab.id, activityBarOpen: true };
    }),
  addTab: (type, label, meta) =>
    set((state) => {
      const tab: TabItem = { id: nextTabId(), type, label, meta };
      const tabs = [...state.tabs, tab];
      persist(tabs, tab.id);
      return { tabs, activeTabId: tab.id, activityBarOpen: true };
    }),
  updateTab: (tabId, label, meta) =>
    set((state) => {
      const tabs = state.tabs.map((tab) => (tab.id === tabId ? { ...tab, label, ...(meta ? { meta } : {}) } : tab));
      persist(tabs, state.activeTabId);
      return { tabs };
    }),
  closeTab: (tabId) =>
    set((state) => {
      const index = state.tabs.findIndex((tab) => tab.id === tabId);
      if (index < 0) return state;
      const tabs = state.tabs.filter((tab) => tab.id !== tabId);
      let activeTabId = state.activeTabId;
      if (state.activeTabId === tabId) {
        // Fall back to the neighbor on the left, then the right, then null.
        activeTabId = tabs[index - 1]?.id ?? tabs[index]?.id ?? null;
      }
      persist(tabs, activeTabId);
      // Closing the last tab collapses the container back to the activity bar.
      return { tabs, activeTabId, activityBarOpen: tabs.length > 0 };
    }),
  activateTab: (tabId) =>
    set((state) => {
      if (!state.tabs.some((tab) => tab.id === tabId)) return state;
      persist(state.tabs, tabId);
      return { activeTabId: tabId, activityBarOpen: true };
    }),
  moveTab: (fromId, toId, side) =>
    set((state) => {
      if (fromId === toId) return state;
      const tabs = [...state.tabs];
      const fromIndex = tabs.findIndex((tab) => tab.id === fromId);
      if (fromIndex < 0) return state;
      const [moved] = tabs.splice(fromIndex, 1);
      const toIndex = tabs.findIndex((tab) => tab.id === toId);
      if (toIndex < 0) return state;
      tabs.splice(side === "right" ? toIndex + 1 : toIndex, 0, moved);
      persist(tabs, state.activeTabId);
      return { tabs };
    }),
  setActivityBarOpen: (open) => set({ activityBarOpen: open }),
  setAddMenuOpen: (open) => set({ addMenuOpen: open }),
  setWorkspaceRoot: (root) => {
    if (root === workspaceRoot) return;
    workspaceRoot = root;
    const loaded = loadTabs();
    set({
      tabs: loaded.tabs,
      activeTabId: loaded.activeTabId,
      activityBarOpen: loaded.tabs.length > 0,
      addMenuOpen: false,
    });
  },
}));
