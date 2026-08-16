import { create } from "zustand";

import { app } from "../lib/bridge";
import type { TerminalSessionView, TerminalWorkspaceView } from "../lib/types";
import { forgetTerminalSession, registerTerminalExitListener } from "../lib/terminalEvents";

type TerminalState = {
  tabId: string;
  generation: number;
  workspace: TerminalWorkspaceView | null;
  loading: boolean;
  error: string | null;
  activeSessionId: string | null;
  syncWorkspace: (tabId: string, force?: boolean) => Promise<TerminalWorkspaceView | null>;
  ensureReady: (tabId: string) => Promise<TerminalWorkspaceView | null>;
  createSession: (tabId: string, relativePath?: string, shellId?: string) => Promise<TerminalSessionView | null>;
  write: (tabId: string, sessionId: string, data: string) => Promise<void>;
  resize: (tabId: string, sessionId: string, cols: number, rows: number) => Promise<void>;
  closeSession: (tabId: string, sessionId: string) => Promise<void>;
  renameSession: (tabId: string, sessionId: string, title: string) => Promise<void>;
  clearError: () => void;
  setActiveSession: (sessionId: string | null) => void;
};

let inFlight: { tabId: string; promise: Promise<TerminalWorkspaceView | null> } | null = null;

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function normalizedWorkspace(value: TerminalWorkspaceView): TerminalWorkspaceView {
  return {
    ...value,
    sessions: Array.isArray(value.sessions) ? value.sessions : [],
    shells: Array.isArray(value.shells) ? value.shells : [],
  };
}

export const useTerminalStore = create<TerminalState>((set, get) => ({
  tabId: "",
  generation: 0,
  workspace: null,
  loading: false,
  error: null,
  activeSessionId: null,
  async syncWorkspace(tabId, force = false) {
    const normalizedTabId = tabId.trim();
    if (!normalizedTabId) {
      set({ tabId: "", workspace: null, activeSessionId: null, loading: false, error: null });
      return null;
    }
    if (!force && inFlight?.tabId === normalizedTabId) return inFlight.promise;
    const previous = get();
    // A background refresh for the same tab should not tear down a painted
    // xterm. Keep its workspace until the replacement response is ready.
    const keepWorkspace = !force && previous.tabId === normalizedTabId && previous.workspace != null;
    const generation = previous.generation + 1;
    set({
      tabId: normalizedTabId,
      generation,
      loading: true,
      workspace: keepWorkspace ? previous.workspace : null,
      activeSessionId: keepWorkspace ? previous.activeSessionId : null,
      error: null,
    });
    const request = app.TerminalWorkspaceForTab(normalizedTabId)
      .then((value) => {
        const workspace = normalizedWorkspace(value);
        const current = get();
        if (current.tabId !== normalizedTabId || current.generation !== generation) return null;
        const active = workspace.sessions.find((session) => session.running)?.id ?? workspace.sessions[0]?.id ?? null;
        set({ workspace, loading: false, error: null, activeSessionId: active });
        return workspace;
      })
      .catch((error) => {
        const current = get();
        if (current.tabId === normalizedTabId && current.generation === generation) {
          set({ workspace: null, loading: false, error: errorMessage(error), activeSessionId: null });
        }
        throw error;
      });
    inFlight = { tabId: normalizedTabId, promise: request };
    try {
      return await request;
    } finally {
      if (inFlight?.promise === request) inFlight = null;
    }
  },
  async ensureReady(tabId) {
    const normalizedTabId = tabId.trim();
    const current = get();
    if (current.tabId === normalizedTabId && current.workspace && !current.loading) return current.workspace;
    if (inFlight?.tabId === normalizedTabId) return inFlight.promise;
    return get().syncWorkspace(normalizedTabId);
  },
  async createSession(tabId, relativePath = ".", shellId = "default") {
    const normalizedTabId = tabId.trim();
    set({ error: null });
    try {
      const workspace = await get().ensureReady(normalizedTabId);
      if (!workspace?.available || workspace.readOnly) return null;
      const session = await app.CreateTerminalForTab(normalizedTabId, relativePath, shellId);
      set((state) => {
        if (state.tabId !== normalizedTabId || !state.workspace) return {};
        const sessions = state.workspace.sessions.some((item) => item.id === session.id)
          ? state.workspace.sessions.map((item) => item.id === session.id ? session : item)
          : [...state.workspace.sessions, session];
        return { workspace: { ...state.workspace, sessions }, error: null, activeSessionId: session.id };
      });
      return session;
    } catch (error) {
      if (get().tabId === normalizedTabId) set({ error: errorMessage(error) });
      throw error;
    }
  },
  async write(tabId, sessionId, data) {
    try {
      await app.WriteTerminalForTab(tabId, sessionId, data);
    } catch (error) {
      const current = get();
      if (current.tabId === tabId && current.workspace?.sessions.some((session) => session.id === sessionId)) {
        set({ error: errorMessage(error) });
      }
      throw error;
    }
  },
  async resize(tabId, sessionId, cols, rows) {
    await app.ResizeTerminalForTab(tabId, sessionId, cols, rows);
  },
  async closeSession(tabId, sessionId) {
    try {
      await app.CloseTerminalForTab(tabId, sessionId);
      forgetTerminalSession(sessionId);
      set((state) => {
        if (state.tabId !== tabId || !state.workspace) return {};
        const sessions = state.workspace.sessions.filter((session) => session.id !== sessionId);
        const activeSessionId = state.activeSessionId === sessionId
          ? sessions.find((session) => session.running)?.id ?? sessions[0]?.id ?? null
          : state.activeSessionId;
        return { workspace: { ...state.workspace, sessions }, error: null, activeSessionId };
      });
    } catch (error) {
      if (get().tabId === tabId) set({ error: errorMessage(error) });
      throw error;
    }
  },
  async renameSession(tabId, sessionId, title) {
    try {
      await app.RenameTerminalForTab(tabId, sessionId, title);
      const current = get();
      if (current.tabId !== tabId || !current.workspace) return;
      const sessions = current.workspace.sessions.map((session) => session.id === sessionId ? { ...session, title } : session);
      set({ workspace: { ...current.workspace, sessions }, error: null });
    } catch (error) {
      if (get().tabId === tabId) set({ error: errorMessage(error) });
      throw error;
    }
  },
  clearError: () => set({ error: null }),
  setActiveSession: (activeSessionId) => set({ activeSessionId }),
}));

registerTerminalExitListener((event) => {
  useTerminalStore.setState((state) => {
    if (!state.workspace) return {};
    if (event.removed) {
      const sessions = state.workspace.sessions.filter((session) => session.id !== event.id);
      const activeSessionId = state.activeSessionId === event.id
        ? sessions.find((session) => session.running)?.id ?? sessions[0]?.id ?? null
        : state.activeSessionId;
      return { workspace: { ...state.workspace, sessions }, activeSessionId };
    }
    const sessions = state.workspace.sessions.map((session) => session.id === event.id
      ? { ...session, running: false, exitCode: event.exitCode }
      : session);
    return { workspace: { ...state.workspace, sessions } };
  });
});

export function resetTerminalStoreForTests(): void {
  inFlight = null;
  useTerminalStore.setState({ tabId: "", generation: 0, workspace: null, loading: false, error: null, activeSessionId: null });
}
