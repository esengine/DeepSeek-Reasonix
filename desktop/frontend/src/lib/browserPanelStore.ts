import { create } from "zustand";

import { normalizeAddress, zoomStep } from "./browserAddress";
import type { BrowserDownloadView, BrowserNavigationTarget, BrowserTabView, DesktopBrowserHost } from "./browserHost";

/** Tabs the user opens from the panel belong to this pseudo task and show beside every task's tabs. */
export const USER_TASK_ID = "user";
const DOWNLOAD_LIMIT = 50;

type Projection = { tabs: BrowserTabView[]; taskId: string; visible: boolean; activeTabId: string | null };

export type BrowserPanelState = Projection & {
  host: DesktopBrowserHost | null;
  notify: (message: string) => void;
  shown: BrowserTabView[];
  activated: string | null;
  downloads: BrowserDownloadView[];
  drafts: Record<string, string>;
  attach(host: DesktopBrowserHost, notify: (message: string) => void): () => void;
  setTaskId(taskId: string | undefined): void;
  setVisible(visible: boolean): void;
  setDraft(tabId: string | null, value: string): void;
  clearDraft(tabId: string | null): void;
  clearDownloads(): void;
  activate(tabId: string): void;
  open(url: string, temporary?: boolean): Promise<void>;
  submitAddress(): Promise<void>;
  openDraft(): Promise<boolean>;
  close(tabId: string): Promise<void>;
  navigate(tabId: string, target: BrowserNavigationTarget): Promise<void>;
  zoom(tabId: string, direction: -1 | 0 | 1): Promise<void>;
  toggleDevTools(tabId: string): Promise<void>;
  resume(tabId: string): Promise<void>;
  takeover(tabId: string): Promise<void>;
};

const errorText = (error: unknown) => (error instanceof Error ? error.message : String(error));
const draftKey = (tabId: string | null) => tabId ?? "";

export const shownTabs = (tabs: BrowserTabView[], taskId: string) =>
  tabs.filter((tab) => tab.taskId === USER_TASK_ID || tab.taskId === taskId);

export const selectActiveTab = (state: BrowserPanelState) => state.shown.find((tab) => tab.id === state.activeTabId);
export const selectAddress = (state: BrowserPanelState) =>
  state.drafts[draftKey(state.activeTabId)] ?? selectActiveTab(state)?.url ?? "";

export const useBrowserPanelStore = create<BrowserPanelState>((set, get) => {
  const call = (promise: Promise<unknown>) => promise.then(() => undefined, (error: unknown) => get().notify(errorText(error)));

  // Every projection input funnels through here so the shell is told about
  // exactly one visible tab per state, never a tab of another task.
  const project = (patch: Partial<Projection>) => {
    const previous = get();
    const next = { ...previous, ...patch };
    const shown = shownTabs(next.tabs, next.taskId);
    const activeTabId = shown.some((tab) => tab.id === next.activeTabId) ? next.activeTabId : shown[shown.length - 1]?.id ?? null;
    const activated = next.visible && next.host ? activeTabId : null;
    const drafts = Object.fromEntries(Object.entries(previous.drafts)
      .filter(([key]) => key === "" || next.tabs.some((tab) => tab.id === key)));
    set({ ...patch, shown, activeTabId, activated, drafts });
    if (activated !== previous.activated && next.host) void call(next.host.activate(activated));
  };
  const clearDraft = (key: string) => {
    const drafts = { ...get().drafts };
    delete drafts[key];
    set({ drafts });
  };

  return {
    host: null,
    notify: () => {},
    tabs: [],
    shown: [],
    taskId: "",
    visible: false,
    activeTabId: null,
    activated: null,
    downloads: [],
    drafts: {},
    attach(host, notify) {
      set({ host, notify });
      const offTabs = host.onTabs((tabs) => project({ tabs }));
      const offDownload = host.onDownload((download) => set((state) => ({
        downloads: [download, ...state.downloads.filter((entry) => entry.id !== download.id)].slice(0, DOWNLOAD_LIMIT),
      })));
      // A subscription update that landed before this reply is newer; never
      // let the initial list clobber it.
      const atAttach = get().tabs;
      void call(host.list().then((tabs) => {
        if (get().tabs === atAttach) project({ tabs });
      }));
      project({});
      return () => {
        offTabs();
        offDownload();
        if (get().host !== host) return;
        if (get().activated !== null) {
          set({ activated: null });
          void call(host.activate(null));
        }
        set({ host: null, notify: () => {} });
      };
    },
    setTaskId: (taskId) => project({ taskId: taskId ?? "" }),
    setVisible: (visible) => project({ visible }),
    setDraft: (tabId, value) => set((state) => ({ drafts: { ...state.drafts, [draftKey(tabId)]: value } })),
    clearDraft: (tabId) => clearDraft(draftKey(tabId)),
    clearDownloads: () => set((state) => ({ downloads: state.downloads.filter((entry) => entry.state === "progressing") })),
    activate: (tabId) => project({ activeTabId: tabId }),
    async open(url, temporary = false) {
      const { host } = get();
      if (!host) return;
      await call(host.open(url, { taskId: USER_TASK_ID, temporary }).then((tab) => {
        const tabs = get().tabs;
        project({ tabs: tabs.some((entry) => entry.id === tab.id) ? tabs : [...tabs, tab], activeTabId: tab.id });
      }));
    },
    async submitAddress() {
      const state = get();
      const key = draftKey(state.activeTabId);
      const url = normalizeAddress(state.drafts[key] ?? selectActiveTab(state)?.url ?? "");
      if (!url) return;
      clearDraft(key);
      if (state.activeTabId && state.host) await call(state.host.navigate(state.activeTabId, { url }));
      else await get().open(url);
    },
    async openDraft() {
      const state = get();
      const key = draftKey(state.activeTabId);
      const url = normalizeAddress(state.drafts[key] ?? "");
      if (!url) return false;
      clearDraft(key);
      await get().open(url);
      return true;
    },
    async close(tabId) {
      const { host } = get();
      if (!host) return;
      await call(host.close(tabId).then(() => project({ tabs: get().tabs.filter((tab) => tab.id !== tabId) })));
    },
    async navigate(tabId, target) {
      const { host } = get();
      if (host) await call(host.navigate(tabId, target));
    },
    async zoom(tabId, direction) {
      const { host, tabs } = get();
      if (host) await call(host.setZoom(tabId, zoomStep(tabs.find((tab) => tab.id === tabId)?.zoom ?? 1, direction)));
    },
    async toggleDevTools(tabId) {
      const { host } = get();
      if (host) await call(host.toggleDevTools(tabId));
    },
    async resume(tabId) {
      const { host } = get();
      if (host) await call(host.resume(tabId));
    },
    async takeover(tabId) {
      const { host } = get();
      if (host) await call(host.takeover(tabId));
    },
  };
});
