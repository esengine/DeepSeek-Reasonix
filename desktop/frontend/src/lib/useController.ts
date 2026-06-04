// useController is the frontend's state machine over the agent's event stream. It
// maintains per-tab state so background tabs preserve their streaming output, tool
// states, and approvals when the user switches away and back. The active tab's state
// is what components render.

import { useCallback, useEffect, useRef, useState } from "react";
import { asArray } from "./array";
import { app, onEvent, onReady } from "./bridge";
import type {
  BalanceInfo,
  ContextInfo,
  EffortInfo,
  HistoryMessage,
  JobView,
  MemoryView,
  Meta,
  QuestionAnswer,
  SessionMeta,
  TabMeta,
  WireApproval,
  WireAsk,
  WireEvent,
  WireUsage,
} from "./types";

export type ToolStatus = "running" | "done" | "error" | "stopped";

export type LiveStream = { id: string; text: string; reasoning: string };

export type Item =
  | { kind: "user"; id: string; text: string }
  | { kind: "assistant"; id: string; text: string; reasoning: string; streaming: boolean }
  | { kind: "phase"; id: string; text: string }
  | { kind: "notice"; id: string; level: "info" | "warn"; text: string }
  | {
      kind: "compaction";
      id: string;
      pending: boolean;
      trigger: string;
      messages: number;
      summary: string;
      archive: string;
    }
  | {
      kind: "tool";
      id: string;
      name: string;
      args: string;
      readOnly: boolean;
      status: ToolStatus;
      output?: string;
      error?: string;
      truncated?: boolean;
      parentId?: string;
    };

interface State {
  items: Item[];
  running: boolean;
  turnActive: boolean;
  approval?: WireApproval;
  ask?: WireAsk;
  usage?: WireUsage;
  context: ContextInfo;
  meta?: Meta;
  balance?: BalanceInfo;
  effort?: EffortInfo;
  jobs: JobView[];
  currentAssistant?: string;
  live?: LiveStream;
  pendingUser?: string;
  discardTurn?: boolean;
  turnStartAt: number;
  turnTokens: number;
  sessionCostUsd: number;
  retry?: { attempt: number; max: number };
  seq: number;
}

const initialState: State = {
  items: [],
  running: false,
  turnActive: false,
  context: { used: 0, window: 0 },
  jobs: [],
  turnStartAt: 0,
  turnTokens: 0,
  sessionCostUsd: 0,
  seq: 0,
};

type Action =
  | { type: "event"; e: WireEvent }
  | { type: "user"; text: string }
  | { type: "unsend" }
  | { type: "meta"; meta: Meta }
  | { type: "context"; context: ContextInfo }
  | { type: "balance"; balance: BalanceInfo }
  | { type: "effort"; effort: EffortInfo }
  | { type: "jobs"; jobs: JobView[] }
  | { type: "history"; messages: HistoryMessage[] }
  | { type: "local_notice"; level: "info" | "warn"; text: string }
  | { type: "clearApproval" }
  | { type: "clearAsk" }
  | { type: "reset" };

// ---- reducer helpers (unchanged logic) ----

function ensureAssistant(s: State): { items: Item[]; id: string; seq: number } {
  if (s.currentAssistant) {
    const exists = s.items.some((it) => it.id === s.currentAssistant && it.kind === "assistant");
    if (exists) return { items: s.items, id: s.currentAssistant, seq: s.seq };
  }
  const id = `a${s.seq}`;
  const item: Item = { kind: "assistant", id, text: "", reasoning: "", streaming: true };
  return { items: [...s.items, item], id, seq: s.seq + 1 };
}

function flushPendingUser(s: State): State {
  if (s.pendingUser === undefined) return s;
  return {
    ...s,
    seq: s.seq + 1,
    items: [...s.items, { kind: "user", id: `u${s.seq}`, text: s.pendingUser }],
    pendingUser: undefined,
  };
}

function applyEvent(s: State, e: WireEvent): State {
  if (s.discardTurn) {
    if (e.kind === "turn_done") return { ...s, discardTurn: false, running: false, turnActive: false, currentAssistant: undefined, live: undefined };
    return s;
  }
  if (s.pendingUser !== undefined && e.kind !== "turn_started" && e.kind !== "turn_done") {
    s = flushPendingUser(s);
  }
  if (e.kind === "retrying") {
    return { ...s, retry: { attempt: e.retryAttempt ?? 0, max: e.retryMax ?? 0 } };
  }
  if (s.retry) s = { ...s, retry: undefined };
  switch (e.kind) {
    case "turn_started":
      return { ...s, running: true, turnActive: true, currentAssistant: undefined, turnStartAt: Date.now(), turnTokens: 0 };
    case "text":
    case "reasoning": {
      const { items, id, seq } = ensureAssistant(s);
      const delta = e.text ?? e.reasoning ?? "";
      const base = s.live?.id === id ? s.live : { id, text: "", reasoning: "" };
      const live = e.kind === "text" ? { ...base, text: base.text + delta } : { ...base, reasoning: base.reasoning + delta };
      return { ...s, items, live, currentAssistant: id, seq };
    }
    case "message": {
      const { items, id, seq } = ensureAssistant(s);
      const next = items.map((it) =>
        it.kind === "assistant" && it.id === id
          ? { ...it, text: e.text ?? s.live?.text ?? it.text, reasoning: e.reasoning ?? s.live?.reasoning ?? it.reasoning, streaming: false }
          : it,
      );
      return { ...s, items: next, live: undefined, currentAssistant: undefined, seq };
    }
    case "tool_dispatch": {
      const t = e.tool;
      if (!t) return s;
      const id = t.id || `tool${s.seq}`;
      const idx = s.items.findIndex((it) => it.kind === "tool" && it.id === id);
      if (idx >= 0) {
        const next = [...s.items];
        const it = next[idx];
        if (it.kind === "tool") next[idx] = { ...it, name: t.name, args: t.args ? t.args : it.args, readOnly: t.readOnly };
        return { ...s, items: next };
      }
      return { ...s, seq: s.seq + 1, items: [...s.items, { kind: "tool", id, name: t.name, args: t.args ?? "", readOnly: t.readOnly, status: "running", parentId: t.parentId }] };
    }
    case "tool_result": {
      const t = e.tool;
      if (!t) return s;
      const next = [...s.items];
      let idx = t.id ? next.findIndex((it) => it.kind === "tool" && it.id === t.id) : -1;
      if (idx < 0) {
        for (let i = next.length - 1; i >= 0; i--) {
          const it = next[i];
          if (it.kind === "tool" && it.status === "running") { idx = i; break; }
        }
      }
      if (idx >= 0) {
        const it = next[idx];
        if (it.kind === "tool") next[idx] = { ...it, status: t.err ? "error" : "done", output: t.output, error: t.err, truncated: t.truncated };
      }
      return { ...s, items: next };
    }
    case "tool_progress": {
      const t = e.tool;
      if (!t?.id) return s;
      const idx = s.items.findIndex((it) => it.kind === "tool" && it.id === t.id);
      if (idx < 0) return s;
      const next = [...s.items];
      const it = next[idx];
      if (it.kind === "tool") next[idx] = { ...it, output: (it.output ?? "") + (t.output ?? "") };
      return { ...s, items: next };
    }
    case "usage": {
      const used = e.usage && s.context.window ? e.usage.promptTokens : s.context.used;
      const turnTokens = s.turnTokens + (e.usage?.completionTokens ?? 0);
      const sessionCostUsd = s.sessionCostUsd + (e.usage?.costUsd ?? 0);
      return { ...s, usage: e.usage, context: { ...s.context, used }, turnTokens, sessionCostUsd };
    }
    case "notice":
      return { ...s, running: s.turnActive ? s.running : false, seq: s.seq + 1, items: [...s.items, { kind: "notice", id: `n${s.seq}`, level: e.level ?? "info", text: e.text ?? "" }] };
    case "phase":
      return { ...s, seq: s.seq + 1, items: [...s.items, { kind: "phase", id: `p${s.seq}`, text: e.text ?? "" }] };
    case "compaction_started":
      return { ...s, seq: s.seq + 1, items: [...s.items, { kind: "compaction", id: `c${s.seq}`, pending: true, trigger: e.compaction?.trigger ?? "", messages: 0, summary: "", archive: "" }] };
    case "compaction_done": {
      const c = e.compaction;
      const idx = [...s.items].reverse().findIndex((it) => it.kind === "compaction" && it.pending);
      const at = idx < 0 ? -1 : s.items.length - 1 - idx;
      if (!c?.summary) {
        const items = at < 0 ? s.items : s.items.filter((_, i) => i !== at);
        return { ...s, running: s.turnActive ? s.running : false, items };
      }
      const filled: Item = { kind: "compaction", id: at < 0 ? `c${s.seq}` : (s.items[at] as Extract<Item, { kind: "compaction" }>).id, pending: false, trigger: c.trigger ?? "", messages: c.messages ?? 0, summary: c.summary, archive: c.archive ?? "" };
      const items = at < 0 ? [...s.items, filled] : s.items.map((it, i) => (i === at ? filled : it));
      return { ...s, running: s.turnActive ? s.running : false, seq: s.seq + 1, items };
    }
    case "approval_request": return { ...s, approval: e.approval };
    case "ask_request": return { ...s, ask: e.ask };
    case "turn_done": {
      if (s.pendingUser !== undefined) s = flushPendingUser(s);
      const finalized = s.items.map((it) => {
        if (it.kind === "assistant" && s.live && it.id === s.live.id) return { ...it, text: s.live.text, reasoning: s.live.reasoning, streaming: false };
        if (it.kind === "assistant" && it.streaming) return { ...it, streaming: false };
        if (it.kind === "tool" && it.status === "running") return { ...it, status: "stopped" as const };
        return it;
      });
      const items: Item[] = e.err ? [...finalized, { kind: "notice", id: `e${s.seq}`, level: "warn", text: e.err }] : finalized;
      return { ...s, items, live: undefined, running: false, turnActive: false, currentAssistant: undefined, approval: undefined, ask: undefined, seq: s.seq + 1 };
    }
    default: return s;
  }
}

function reducer(s: State, a: Action): State {
  switch (a.type) {
    case "user": return { ...s, running: true, turnStartAt: Date.now(), turnTokens: 0, pendingUser: a.text, discardTurn: false };
    case "unsend": return { ...s, pendingUser: undefined, discardTurn: true, running: false, live: undefined };
    case "meta": return { ...s, meta: a.meta };
    case "context": return { ...s, context: a.context };
    case "balance": return { ...s, balance: a.balance };
    case "effort": return { ...s, effort: a.effort };
    case "jobs": return { ...s, jobs: a.jobs };
    case "history": {
      const visible = a.messages.filter(
        (m) => (m.role === "user" && m.content.trim() !== "") ||
               (m.role === "assistant" && (m.content.trim() !== "" || (m.reasoning ?? "").trim() !== "")),
      );
      const items: Item[] = visible.map((m, i) =>
        m.role === "user"
          ? { kind: "user", id: `h${i}`, text: m.content }
          : { kind: "assistant", id: `h${i}`, text: m.content, reasoning: m.reasoning ?? "", streaming: false },
      );
      return { ...s, items, seq: s.seq + visible.length };
    }
    case "local_notice": return { ...s, running: false, turnActive: false, seq: s.seq + 1, items: [...s.items, { kind: "notice", id: `n${s.seq}`, level: a.level, text: a.text }] };
    case "clearApproval": return { ...s, approval: undefined };
    case "clearAsk": return { ...s, ask: undefined };
    case "reset": return { ...initialState, meta: s.meta, context: { ...s.context, used: 0 }, balance: s.balance, effort: s.effort, jobs: s.jobs };
    case "event": return applyEvent(s, a.e);
    default: return s;
  }
}

// ---- per-tab state map ----

type TabStates = Map<string, State>;

function getOrCreateState(states: TabStates, tabId: string): State {
  if (!states.has(tabId)) states.set(tabId, { ...initialState });
  return states.get(tabId)!;
}

export function useController() {
  const statesRef = useRef<TabStates>(new Map());
  const [activeTabId, setActiveTabId] = useState<string | undefined>();
  // A render-triggering counter so that mutations to a non-active tab's state still
  // cause a re-render when that tab becomes active.
  const [, setVersion] = useState(0);
  const bump = useCallback(() => setVersion((v) => v + 1), []);

  // The active tab's current state, with a stable identity for cancel().
  const activeState = activeTabId ? getOrCreateState(statesRef.current, activeTabId) : initialState;
  const stateRef = useRef(activeState);
  stateRef.current = activeState;

  // Dispatch to a specific tab's state. If the tab doesn't have state yet, it's
  // created. Bumps the version so React re-renders when it becomes active.
  const dispatchTo = useCallback((tabId: string, action: Action) => {
    const states = statesRef.current;
    const prev = getOrCreateState(states, tabId);
    const next = reducer(prev, action);
    if (prev !== next) {
      states.set(tabId, next);
      bump();
    }
  }, [bump]);

  const loadSessionDataForTab = useCallback(async (tabId: string, reset = false) => {
    try {
      if (reset) dispatchTo(tabId, { type: "reset" });
      dispatchTo(tabId, { type: "meta", meta: await app.Meta() });
      dispatchTo(tabId, { type: "context", context: await app.ContextUsage() });
      dispatchTo(tabId, { type: "effort", effort: await app.Effort() });
      dispatchTo(tabId, { type: "balance", balance: await app.Balance() });
      dispatchTo(tabId, { type: "jobs", jobs: asArray(await app.Jobs()) });
      const history = asArray(await app.History());
      if (history && history.length) dispatchTo(tabId, { type: "history", messages: history });
    } catch { /* ignore */ }
  }, [dispatchTo]);

  const activeTabFromBackend = useCallback(async (): Promise<TabMeta | undefined> => {
    const tabs = asArray(await app.ListTabs().catch(() => [] as TabMeta[]));
    return tabs.find((tab) => tab.active) ?? tabs[0];
  }, []);

  const syncActiveTabFromBackend = useCallback(async (reset = false): Promise<string | undefined> => {
    const active = await activeTabFromBackend();
    if (!active) return undefined;
    setActiveTabId(active.id);
    await loadSessionDataForTab(active.id, reset);
    return active.id;
  }, [activeTabFromBackend, loadSessionDataForTab]);

  const loadSessionData = useCallback(async () => {
    if (activeTabId) {
      await loadSessionDataForTab(activeTabId);
      return;
    }
    await syncActiveTabFromBackend();
  }, [activeTabId, loadSessionDataForTab, syncActiveTabFromBackend]);

  useEffect(() => {
    const off = onEvent((e) => {
      const targetTabId = e.tabId || activeTabId;
      if (!targetTabId) return;
      dispatchTo(targetTabId, { type: "event", e });
      if (e.kind === "turn_done") {
        app
          .ContextPanel(targetTabId)
          .then((info) => dispatchTo(targetTabId, { type: "context", context: { used: info.usedTokens, window: info.windowTokens } }))
          .catch(() => {});
        app.Balance().then((balance) => dispatchTo(targetTabId, { type: "balance", balance })).catch(() => {});
        app.Effort().then((effort) => dispatchTo(targetTabId, { type: "effort", effort })).catch(() => {});
      }
      if (e.kind === "turn_done" || e.kind === "notice") {
        app.Jobs().then((jobs) => dispatchTo(targetTabId, { type: "jobs", jobs: asArray(jobs) })).catch(() => {});
      }
    });

    const offReady = onReady(() => {
      void loadSessionData();
      app.Balance().then((balance) => { if (activeTabId) dispatchTo(activeTabId, { type: "balance", balance }); }).catch(() => {});
      app.Jobs().then((jobs) => { if (activeTabId) dispatchTo(activeTabId, { type: "jobs", jobs: asArray(jobs) }); }).catch(() => {});
      app.Effort().then((effort) => { if (activeTabId) dispatchTo(activeTabId, { type: "effort", effort }); }).catch(() => {});
    });

    void loadSessionData();
    if (activeTabId) {
      app.Balance().then((balance) => dispatchTo(activeTabId, { type: "balance", balance })).catch(() => {});
      app.Effort().then((effort) => dispatchTo(activeTabId, { type: "effort", effort })).catch(() => {});
      app.Jobs().then((jobs) => dispatchTo(activeTabId, { type: "jobs", jobs: asArray(jobs) })).catch(() => {});
    }

    return () => { off(); offReady(); };
  }, [loadSessionData, activeTabId, dispatchTo]);

  const send = useCallback((displayText: string, submitText = displayText) => {
    if (!activeTabId) return;
    dispatchTo(activeTabId, { type: "user", text: displayText });
    const display = displayText.trim(); const submit = submitText.trim();
    (display !== submit ? app.SubmitDisplay(display, submit) : app.Submit(submit)).catch(() => {});
  }, [activeTabId, dispatchTo]);

  const notice = useCallback((text: string, level: "info" | "warn" = "info") => {
    if (!activeTabId) return;
    dispatchTo(activeTabId, { type: "local_notice", level, text });
  }, [activeTabId, dispatchTo]);

  const cancel = useCallback((): string | undefined => {
    const cur = stateRef.current;
    if (cur.running && cur.pendingUser !== undefined) {
      const text = cur.pendingUser;
      if (activeTabId) dispatchTo(activeTabId, { type: "unsend" });
      app.Cancel().catch(() => {});
      return text;
    }
    app.Cancel().catch(() => {});
    return undefined;
  }, [activeTabId, dispatchTo]);

  const approve = useCallback((id: string, allow: boolean, session: boolean, persist: boolean) => {
    if (!activeTabId) return;
    dispatchTo(activeTabId, { type: "clearApproval" });
    app.Approve(id, allow, session, persist).catch(() => {});
  }, [activeTabId, dispatchTo]);

  const answerQuestion = useCallback((id: string, answers: QuestionAnswer[]) => {
    if (!activeTabId) return;
    dispatchTo(activeTabId, { type: "clearAsk" });
    app.AnswerQuestion(id, answers).catch(() => {});
  }, [activeTabId, dispatchTo]);

  const setControllerMode = useCallback((mode: "plan" | "yolo" | "normal"): Promise<void> => {
    return app.SetMode(mode).then(() => {
      if (mode === "yolo" && activeTabId) dispatchTo(activeTabId, { type: "clearApproval" });
    }).catch(() => {});
  }, [activeTabId, dispatchTo]);

  const newSession = useCallback(async () => {
    await app.NewSession().catch(() => {});
    if (activeTabId) dispatchTo(activeTabId, { type: "reset" });
  }, [activeTabId, dispatchTo]);

  const listSessions = useCallback(async (): Promise<SessionMeta[]> => asArray<SessionMeta>(await app.ListSessions().catch(() => [])), []);
  const resumeSession = useCallback(async (path: string) => {
    const messages = asArray(await app.ResumeSession(path).catch(() => [] as HistoryMessage[]));
    if (activeTabId) {
      dispatchTo(activeTabId, { type: "reset" });
      if (messages.length) dispatchTo(activeTabId, { type: "history", messages });
      app.ContextUsage().then((context) => dispatchTo(activeTabId, { type: "context", context })).catch(() => {});
    }
  }, [activeTabId, dispatchTo]);
  const previewSession = useCallback(async (path: string): Promise<HistoryMessage[]> => asArray<HistoryMessage>(await app.PreviewSession(path).catch(() => [])), []);
  const deleteSession = useCallback((path: string) => app.DeleteSession(path).catch(() => {}), []);
  const renameSession = useCallback((path: string, title: string) => app.RenameSession(path, title).catch(() => {}), []);

  const refreshMeta = useCallback(async () => {
    if (!activeTabId) return;
    try {
      dispatchTo(activeTabId, { type: "meta", meta: await app.Meta() });
      dispatchTo(activeTabId, { type: "context", context: await app.ContextUsage() });
      dispatchTo(activeTabId, { type: "effort", effort: await app.Effort() });
    } catch { /* ignore */ }
  }, [activeTabId, dispatchTo]);

  const refreshWorkspaceState = useCallback(async (path: string): Promise<string> => {
    if (path) await syncActiveTabFromBackend(true);
    return path;
  }, [syncActiveTabFromBackend]);

  const pickWorkspace = useCallback(async (): Promise<string> => {
    const path = await app.PickWorkspace().catch(() => "");
    return refreshWorkspaceState(path);
  }, [refreshWorkspaceState]);
  const switchWorkspace = useCallback(async (path: string): Promise<string> => {
    const next = await app.SwitchWorkspace(path).catch(() => "");
    return refreshWorkspaceState(next);
  }, [refreshWorkspaceState]);

  const compact = useCallback(() => { app.Compact().catch(() => {}); }, []);

  const setModel = useCallback(async (name: string) => {
    try {
      await app.SetModel(name);
    } catch (e) {
      if (activeTabId) {
        const text = `Model switch failed: ${e instanceof Error ? e.message : String(e)}`;
        dispatchTo(activeTabId, { type: "local_notice", level: "warn", text });
      }
      return;
    }
    if (!activeTabId) return;
    try {
      dispatchTo(activeTabId, { type: "meta", meta: await app.Meta() });
      dispatchTo(activeTabId, { type: "context", context: await app.ContextUsage() });
      dispatchTo(activeTabId, { type: "effort", effort: await app.Effort() });
    } catch { /* ignore */ }
  }, [activeTabId, dispatchTo]);

  const setEffort = useCallback(async (level: string) => {
    await app.SetEffort(level).catch(() => {});
    if (!activeTabId) return;
    try {
      dispatchTo(activeTabId, { type: "meta", meta: await app.Meta() });
      dispatchTo(activeTabId, { type: "context", context: await app.ContextUsage() });
      dispatchTo(activeTabId, { type: "effort", effort: await app.Effort() });
    } catch { /* ignore */ }
  }, [activeTabId, dispatchTo]);

  const fetchMemory = useCallback((): Promise<MemoryView> =>
    app.Memory().catch(() => ({ docs: [], facts: [], scopes: [], storeDir: "", available: false })), []);
  const remember = useCallback(async (scope: string, note: string) => { await app.Remember(scope, note).catch(() => {}); }, []);
  const forget = useCallback(async (name: string) => { await app.Forget(name).catch(() => {}); }, []);
  const saveDoc = useCallback(async (path: string, body: string) => { await app.SaveDoc(path, body).catch(() => {}); }, []);

  const rewind = useCallback(async (turn: number, scope: string) => {
    if (scope === "fork") await app.Fork(turn).catch(() => {});
    else if (scope === "summ-from") await app.SummarizeFrom(turn).catch(() => {});
    else if (scope === "summ-upto") await app.SummarizeUpTo(turn).catch(() => {});
    else await app.Rewind(turn, scope).catch(() => {});
    if (!activeTabId) return;
    const messages = asArray(await app.History().catch(() => [] as HistoryMessage[]));
    dispatchTo(activeTabId, { type: "reset" });
    if (messages.length) dispatchTo(activeTabId, { type: "history", messages });
    app.ContextUsage().then((context) => dispatchTo(activeTabId, { type: "context", context })).catch(() => {});
  }, [activeTabId, dispatchTo]);

  // Tab management: switch preserves per-tab state; open creates it.
  const switchTab = useCallback(async (tabId: string) => {
    try {
      await app.SetActiveTab(tabId);
      setActiveTabId(tabId);
      // Load session data into the tab's state if it hasn't been loaded yet.
      const states = statesRef.current;
      if (!states.has(tabId) || !states.get(tabId)?.meta) {
        await loadSessionDataForTab(tabId);
      }
    } catch { /* ignore */ }
  }, [loadSessionDataForTab]);

  const openProjectTab = useCallback(async (workspaceRoot: string, topicId: string) => {
    try {
      const meta = await app.OpenProjectTab(workspaceRoot, topicId);
      setActiveTabId(meta.id);
      await loadSessionDataForTab(meta.id, !statesRef.current.has(meta.id));
    } catch { /* ignore */ }
  }, [loadSessionDataForTab]);

  const openGlobalTab = useCallback(async (topicId: string) => {
    try {
      const meta = await app.OpenGlobalTab(topicId);
      setActiveTabId(meta.id);
      await loadSessionDataForTab(meta.id, !statesRef.current.has(meta.id));
    } catch { /* ignore */ }
  }, [loadSessionDataForTab]);

  const closeTab = useCallback(async (tabId: string) => {
    try {
      await app.CloseTab(tabId);
      statesRef.current.delete(tabId);
      bump();
      if (tabId === activeTabId) await syncActiveTabFromBackend(true);
    } catch { /* ignore */ }
  }, [activeTabId, bump, syncActiveTabFromBackend]);

  return {
    state: activeState,
    activeTabId,
    send, notice, cancel, approve, answerQuestion, setControllerMode,
    newSession, listSessions, resumeSession, previewSession, deleteSession, renameSession,
    refreshMeta, pickWorkspace, switchWorkspace, compact, rewind, setModel, setEffort,
    fetchMemory, remember, forget, saveDoc,
    switchTab, openProjectTab, openGlobalTab, closeTab,
  };
}
