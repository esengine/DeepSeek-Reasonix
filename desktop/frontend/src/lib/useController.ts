// useController is the frontend's state machine over the agent's event stream. It
// reduces the flat WireEvent flow (text/reasoning deltas, tool dispatch/result,
// notices, approvals, usage) into a structured transcript the components render,
// and exposes the command surface (send/cancel/approve/…) that calls back into
// the kernel via the bridge. This is the desktop analogue of the chat TUI's
// update loop — same controller, different renderer.

import { useCallback, useEffect, useReducer, useRef } from "react";
import { app, onEvent, onReady } from "./bridge";
import type {
  BalanceInfo,
  ContextInfo,
  HistoryMessage,
  JobView,
  MemoryView,
  Meta,
  QuestionAnswer,
  SessionMeta,
  WireApproval,
  WireAsk,
  WireEvent,
  WireUsage,
} from "./types";

export type ToolStatus = "running" | "done" | "error" | "stopped";

export type Item =
  | { kind: "user"; id: string; text: string }
  | { kind: "assistant"; id: string; text: string; reasoning: string; streaming: boolean }
  | { kind: "phase"; id: string; text: string }
  | { kind: "notice"; id: string; level: "info" | "warn"; text: string }
  | {
      kind: "compaction";
      id: string;
      pending: boolean; // true between compaction_started and compaction_done
      trigger: string; // "auto" | "manual"
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
      parentId?: string; // a sub-agent call nests under the `task` call with this id
    };

interface State {
  items: Item[];
  running: boolean;
  // turnActive is true only while a real model turn is in flight (between
  // turn_started and turn_done). `running` may be set optimistically on send for
  // immediate feedback; turnActive distinguishes that from an actual turn, so a
  // local command that only emits a Notice (e.g. /skill, /compact) clears the
  // optimistic spinner instead of leaving it stuck forever.
  turnActive: boolean;
  approval?: WireApproval;
  ask?: WireAsk;
  usage?: WireUsage;
  context: ContextInfo;
  meta?: Meta;
  // balance is the active provider's wallet readout, refreshed on mount and after
  // each turn; undefined until first fetched, available:false when not configured.
  balance?: BalanceInfo;
  // jobs are the running background jobs, refreshed on mount, turn end, and on
  // each notice (job start/finish emit notices).
  jobs: JobView[];
  // currentAssistant tracks the in-flight assistant item that text/reasoning
  // deltas accumulate into; cleared at turn boundaries.
  currentAssistant?: string;
  // pendingUser holds a just-sent message whose bubble is deferred until the
  // server's first real packet, so an Esc/Stop before any reply "un-sends" it —
  // restoring the text to the composer with nothing left in the transcript. It's
  // committed by the first packet (or, defensively, at turn end). discardTurn is
  // set on un-send so the cancelled turn's already-buffered events are swallowed
  // until its turn_done settles.
  pendingUser?: string;
  discardTurn?: boolean;
  // turnStartAt is the wall-clock ms the current turn began (0 when idle), and
  // turnTokens accumulates the output tokens reported this turn — together they
  // drive the live "thinking… (12s · ↓3.6k tokens)" activity readout. Pure
  // frontend-observed harness state; no model cooperation needed.
  turnStartAt: number;
  turnTokens: number;
  // seq is a monotonic id source so React keys stay stable across re-renders.
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
  seq: 0,
};

type Action =
  | { type: "event"; e: WireEvent }
  | { type: "user"; text: string }
  | { type: "unsend" }
  | { type: "meta"; meta: Meta }
  | { type: "context"; context: ContextInfo }
  | { type: "balance"; balance: BalanceInfo }
  | { type: "jobs"; jobs: JobView[] }
  | { type: "history"; messages: HistoryMessage[] }
  | { type: "clearApproval" }
  | { type: "clearAsk" }
  | { type: "reset" };

// ensureAssistant returns the items array containing the active assistant item
// (creating one if the turn hasn't produced text yet), its id, and the next seq.
function ensureAssistant(s: State): { items: Item[]; id: string; seq: number } {
  if (s.currentAssistant) {
    const exists = s.items.some((it) => it.id === s.currentAssistant && it.kind === "assistant");
    if (exists) return { items: s.items, id: s.currentAssistant, seq: s.seq };
  }
  const id = `a${s.seq}`;
  const item: Item = { kind: "assistant", id, text: "", reasoning: "", streaming: true };
  return { items: [...s.items, item], id, seq: s.seq + 1 };
}

// flushPendingUser commits the deferred user bubble into the transcript (a no-op
// when none is pending). Called by the first real packet of a turn, and at turn
// end as a fallback so an error-before-reply or empty turn still shows what the
// user sent.
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
  // After an un-send, swallow the cancelled turn's still-buffered events so no
  // orphan assistant/tool bubble appears; its turn_done clears the discard.
  if (s.discardTurn) {
    if (e.kind === "turn_done") return { ...s, discardTurn: false, running: false, turnActive: false, currentAssistant: undefined };
    return s;
  }
  // The first real packet means the server replied — commit the deferred user
  // bubble before rendering it. turn_started is local (emitted before the
  // request) and turn_done is handled in its own case, so neither commits.
  if (s.pendingUser !== undefined && e.kind !== "turn_started" && e.kind !== "turn_done") {
    s = flushPendingUser(s);
  }
  switch (e.kind) {
    case "turn_started":
      return { ...s, running: true, turnActive: true, currentAssistant: undefined, turnStartAt: Date.now(), turnTokens: 0 };

    case "text":
    case "reasoning": {
      const { items, id, seq } = ensureAssistant(s);
      const delta = e.text ?? e.reasoning ?? "";
      const next = items.map((it) =>
        it.kind === "assistant" && it.id === id
          ? e.kind === "text"
            ? { ...it, text: it.text + delta }
            : { ...it, reasoning: it.reasoning + delta }
          : it,
      );
      return { ...s, items: next, currentAssistant: id, seq };
    }

    case "message": {
      const { items, id, seq } = ensureAssistant(s);
      const next = items.map((it) =>
        it.kind === "assistant" && it.id === id
          ? { ...it, text: e.text ?? it.text, reasoning: e.reasoning ?? it.reasoning, streaming: false }
          : it,
      );
      return { ...s, items: next, currentAssistant: undefined, seq };
    }

    case "tool_dispatch": {
      const t = e.tool;
      if (!t) return s;
      const id = t.id || `tool${s.seq}`;
      // A call streams two dispatches: an early partial one (name only, so the
      // card shows at once) and a full one (with args) when it completes. Merge
      // by id — update the existing card rather than appending a duplicate.
      const idx = s.items.findIndex((it) => it.kind === "tool" && it.id === id);
      if (idx >= 0) {
        const next = [...s.items];
        const it = next[idx];
        if (it.kind === "tool") {
          next[idx] = { ...it, name: t.name, args: t.args ? t.args : it.args, readOnly: t.readOnly };
        }
        return { ...s, items: next };
      }
      const item: Item = {
        kind: "tool",
        id,
        name: t.name,
        args: t.args ?? "",
        readOnly: t.readOnly,
        status: "running",
        parentId: t.parentId,
      };
      return { ...s, seq: s.seq + 1, items: [...s.items, item] };
    }

    case "tool_result": {
      const t = e.tool;
      if (!t) return s;
      const next = [...s.items];
      // Match the dispatched card by id; if the kernel omitted one, fall back to
      // the most recent still-running tool.
      let idx = t.id ? next.findIndex((it) => it.kind === "tool" && it.id === t.id) : -1;
      if (idx < 0) {
        for (let i = next.length - 1; i >= 0; i--) {
          const cand = next[i];
          if (cand.kind === "tool" && cand.status === "running") {
            idx = i;
            break;
          }
        }
      }
      if (idx >= 0) {
        const it = next[idx];
        if (it.kind === "tool") {
          next[idx] = {
            ...it,
            status: t.err ? "error" : "done",
            output: t.output,
            error: t.err,
            truncated: t.truncated,
          };
        }
      }
      return { ...s, items: next };
    }

    case "usage": {
      const used = e.usage && s.context.window ? e.usage.promptTokens : s.context.used;
      // Usage arrives once per model step; sum the output across steps for the
      // turn's running token tally.
      const turnTokens = s.turnTokens + (e.usage?.completionTokens ?? 0);
      return { ...s, usage: e.usage, context: { ...s.context, used }, turnTokens };
    }

    case "notice":
      // A Notice with no real turn in flight means a local command (e.g. /skill,
      // /compact) produced output without starting a turn — clear the optimistic
      // spinner so it doesn't read seconds forever. Mid-turn notices keep running.
      return {
        ...s,
        running: s.turnActive ? s.running : false,
        seq: s.seq + 1,
        items: [...s.items, { kind: "notice", id: `n${s.seq}`, level: e.level ?? "info", text: e.text ?? "" }],
      };

    case "phase":
      return {
        ...s,
        seq: s.seq + 1,
        items: [...s.items, { kind: "phase", id: `p${s.seq}`, text: e.text ?? "" }],
      };

    case "compaction_started":
      // Drop a pending card the moment the summarizer starts, so the user sees
      // the pass is running rather than a frozen window.
      return {
        ...s,
        seq: s.seq + 1,
        items: [
          ...s.items,
          {
            kind: "compaction",
            id: `c${s.seq}`,
            pending: true,
            trigger: e.compaction?.trigger ?? "",
            messages: 0,
            summary: "",
            archive: "",
          },
        ],
      };

    case "compaction_done": {
      const c = e.compaction;
      // An aborted pass (no summary) drops the pending placeholder; the
      // accompanying notice explains why. Otherwise fill the last pending card.
      const idx = [...s.items].reverse().findIndex((it) => it.kind === "compaction" && it.pending);
      const at = idx < 0 ? -1 : s.items.length - 1 - idx;
      if (!c?.summary) {
        const items = at < 0 ? s.items : s.items.filter((_, i) => i !== at);
        return { ...s, running: s.turnActive ? s.running : false, items };
      }
      const filled: Item = {
        kind: "compaction",
        id: at < 0 ? `c${s.seq}` : (s.items[at] as Extract<Item, { kind: "compaction" }>).id,
        pending: false,
        trigger: c.trigger ?? "",
        messages: c.messages ?? 0,
        summary: c.summary,
        archive: c.archive ?? "",
      };
      const items = at < 0 ? [...s.items, filled] : s.items.map((it, i) => (i === at ? filled : it));
      return { ...s, running: s.turnActive ? s.running : false, seq: s.seq + 1, items };
    }

    case "approval_request":
      return { ...s, approval: e.approval };

    case "ask_request":
      return { ...s, ask: e.ask };

    case "turn_done": {
      // A turn that ended while its bubble was still deferred (an error before any
      // reply, or an empty turn) was really sent — commit it so it isn't lost. A
      // user-cancel before any reply takes the un-send path instead (discardTurn).
      if (s.pendingUser !== undefined) s = flushPendingUser(s);
      // The turn is over, so nothing more will arrive: freeze a streaming
      // assistant, and settle any tool still "running" (e.g. a call interrupted
      // by cancel, which never gets a result) to "stopped" so it stops spinning.
      const finalized = s.items.map((it) => {
        if (it.kind === "assistant" && it.streaming) return { ...it, streaming: false };
        if (it.kind === "tool" && it.status === "running") return { ...it, status: "stopped" as const };
        return it;
      });
      const items: Item[] = e.err
        ? [...finalized, { kind: "notice", id: `e${s.seq}`, level: "warn", text: e.err }]
        : finalized;
      return { ...s, items, running: false, turnActive: false, currentAssistant: undefined, approval: undefined, ask: undefined, seq: s.seq + 1 };
    }
  }
}

function reducer(s: State, a: Action): State {
  switch (a.type) {
    case "user":
      // Defer the bubble (see pendingUser): it lands in the transcript only once
      // the server replies, so an Esc before then can un-send it cleanly.
      return {
        ...s,
        running: true,
        turnStartAt: Date.now(),
        turnTokens: 0,
        pendingUser: a.text,
        discardTurn: false,
      };
    case "unsend":
      // Esc/Stop before any reply: drop the deferred bubble and mark the turn
      // discarded so its trailing events are swallowed. The composer restores the
      // text from cancel()'s return value.
      return { ...s, pendingUser: undefined, discardTurn: true, running: false };
    case "meta":
      return { ...s, meta: a.meta };
    case "context":
      return { ...s, context: a.context };
    case "balance":
      return { ...s, balance: a.balance };
    case "jobs":
      return { ...s, jobs: a.jobs };
    case "history": {
      // Only user/assistant turns with visible text — never the system prompt or
      // tool-result messages, and not the empty content of a tool-call-only turn.
      const visible = a.messages.filter(
        (m) => (m.role === "user" || m.role === "assistant") && m.content.trim() !== "",
      );
      const items: Item[] = visible.map((m, i) =>
        m.role === "user"
          ? { kind: "user", id: `h${i}`, text: m.content }
          : { kind: "assistant", id: `h${i}`, text: m.content, reasoning: "", streaming: false },
      );
      return { ...s, items, seq: s.seq + visible.length };
    }
    case "clearApproval":
      return { ...s, approval: undefined };
    case "clearAsk":
      return { ...s, ask: undefined };
    case "reset":
      // Background jobs and the balance are session-scoped (the controller and its
      // job manager survive a new-session rotation), so carry them across a reset.
      return { ...initialState, meta: s.meta, context: { ...s.context, used: 0 }, balance: s.balance, jobs: s.jobs };
    case "event":
      return applyEvent(s, a.e);
  }
}

export function useController() {
  const [state, dispatch] = useReducer(reducer, initialState);
  // A live mirror of state for event-handler callbacks (useCallback closures are
  // pinned to the first render); cancel() reads it to decide un-send vs. cancel.
  const stateRef = useRef(state);
  stateRef.current = state;

  // loadSessionData fetches Meta, ContextUsage, and History — called on mount
  // and again when agent:ready fires (boot.Build completed in the background).
  const loadSessionData = useCallback(async () => {
    try {
      dispatch({ type: "meta", meta: await app.Meta() });
      dispatch({ type: "context", context: await app.ContextUsage() });
      const history = await app.History();
      if (history && history.length) dispatch({ type: "history", messages: history });
    } catch {
      // Bound methods unavailable (pre-startup / build error) — ignore; Meta's
      // startupErr surfaces the reason once it's reachable.
    }
  }, []);

  useEffect(() => {
    const off = onEvent((e) => {
      dispatch({ type: "event", e });
      // The gauge's denominator (window) and post-turn prompt size come from the
      // kernel, not the stream — refresh once a turn settles. The wallet balance
      // moves with spend, so refresh it on the same boundary.
      if (e.kind === "turn_done") {
        app
          .ContextUsage()
          .then((context) => dispatch({ type: "context", context }))
          .catch(() => {});
        app
          .Balance()
          .then((balance) => dispatch({ type: "balance", balance }))
          .catch(() => {});
      }
      // Background jobs start/finish via notices and bound around a turn, so
      // refresh the running set on both — keeps the status-bar count live.
      if (e.kind === "turn_done" || e.kind === "notice") {
        app
          .Jobs()
          .then((jobs) => dispatch({ type: "jobs", jobs }))
          .catch(() => {});
      }
    });

    // When boot.Build completes asynchronously, the Go side emits agent:ready.
    // Re-fetch session data so the UI reflects the now-available controller.
    const offReady = onReady(() => {
      void loadSessionData();
      app
        .Balance()
        .then((balance) => dispatch({ type: "balance", balance }))
        .catch(() => {});
      app
        .Jobs()
        .then((jobs) => dispatch({ type: "jobs", jobs }))
        .catch(() => {});
    });

    // Initial load — picks up the pre-build Meta (ready=false) and, if the
    // build already finished, the full session.
    void loadSessionData();

    // Wallet balance is a network call — fetch it independently so it never delays
    // the transcript/meta load (and is a no-op readout when not configured).
    app
      .Balance()
      .then((balance) => dispatch({ type: "balance", balance }))
      .catch(() => {});
    app
      .Jobs()
      .then((jobs) => dispatch({ type: "jobs", jobs }))
      .catch(() => {});

    return () => {
      off();
      offReady();
    };
  }, [loadSessionData]);

  const send = useCallback((displayText: string, submitText = displayText) => {
    dispatch({ type: "user", text: displayText });
    const display = displayText.trim();
    const submit = submitText.trim();
    const call = display !== submit ? app.SubmitDisplay(display, submit) : app.Submit(submit);
    call.catch(() => {});
  }, []);

  // cancel aborts the in-flight turn. If the server hasn't replied yet (the user
  // bubble is still deferred), it instead "un-sends" the message and returns its
  // text so the composer can restore it; otherwise it returns undefined.
  const cancel = useCallback((): string | undefined => {
    const cur = stateRef.current;
    if (cur.running && cur.pendingUser !== undefined) {
      const text = cur.pendingUser;
      dispatch({ type: "unsend" });
      app.Cancel().catch(() => {});
      return text;
    }
    app.Cancel().catch(() => {});
    return undefined;
  }, []);

  const approve = useCallback((id: string, allow: boolean, session: boolean) => {
    dispatch({ type: "clearApproval" });
    app.Approve(id, allow, session).catch(() => {});
  }, []);

  // answerQuestion resolves an ask_request with the user's per-question picks.
  const answerQuestion = useCallback((id: string, answers: QuestionAnswer[]) => {
    dispatch({ type: "clearAsk" });
    app.AnswerQuestion(id, answers).catch(() => {});
  }, []);

  const setPlan = useCallback((on: boolean) => {
    app.SetPlanMode(on).catch(() => {});
  }, []);

  // setBypass toggles YOLO mode (auto-approve every tool call this session).
  const setBypass = useCallback((on: boolean) => {
    app.SetBypass(on).catch(() => {});
  }, []);

  const newSession = useCallback(async () => {
    await app.NewSession().catch(() => {});
    dispatch({ type: "reset" });
  }, []);

  // Session history: list saved sessions (the panel fetches on open), and resume
  // one — the model/folder are unchanged, only the transcript is swapped.
  const listSessions = useCallback((): Promise<SessionMeta[]> => {
    return app.ListSessions().catch(() => []);
  }, []);

  const resumeSession = useCallback(async (path: string) => {
    const messages = await app.ResumeSession(path).catch(() => [] as HistoryMessage[]);
    dispatch({ type: "reset" });
    if (messages.length) dispatch({ type: "history", messages });
    app.ContextUsage().then((context) => dispatch({ type: "context", context })).catch(() => {});
  }, []);

  // Manage saved sessions: delete one, or give it a custom name (""=clear). Both
  // only touch on-disk state; the caller re-fetches the list to reflect the change.
  const deleteSession = useCallback((path: string) => {
    return app.DeleteSession(path).catch(() => {});
  }, []);

  const renameSession = useCallback((path: string, title: string) => {
    return app.RenameSession(path, title).catch(() => {});
  }, []);

  // refreshMeta re-pulls the model label, gauge, and cwd — used by the Settings
  // panel after a change that rebuilds the controller (model/provider/sandbox/…).
  const refreshMeta = useCallback(async () => {
    try {
      dispatch({ type: "meta", meta: await app.Meta() });
      dispatch({ type: "context", context: await app.ContextUsage() });
    } catch {
      /* ignore */
    }
  }, []);

  // Workspace: open a folder chooser and switch to that project. On a pick the
  // backend rebuilds the controller (new model/config) with a fresh session, so
  // reset and refresh meta/context. Returns the chosen path ("" if cancelled).
  const pickWorkspace = useCallback(async (): Promise<string> => {
    const path = await app.PickWorkspace().catch(() => "");
    if (path) {
      dispatch({ type: "reset" });
      try {
        dispatch({ type: "meta", meta: await app.Meta() });
        dispatch({ type: "context", context: await app.ContextUsage() });
      } catch {
        /* ignore */
      }
    }
    return path;
  }, []);

  const compact = useCallback(() => {
    app.Compact().catch(() => {});
  }, []);

  // setModel switches the active model (the backend carries the conversation into
  // the new model's session); refresh the header/gauge to reflect the new label.
  const setModel = useCallback(async (name: string) => {
    await app.SetModel(name).catch(() => {});
    try {
      dispatch({ type: "meta", meta: await app.Meta() });
      dispatch({ type: "context", context: await app.ContextUsage() });
    } catch {
      /* ignore */
    }
  }, []);

  // Memory panel actions. fetchMemory re-reads the loaded snapshot; remember and
  // saveDoc mutate then return so the caller can re-fetch to reflect the change.
  const fetchMemory = useCallback((): Promise<MemoryView> => {
    return app.Memory().catch(
      () => ({ docs: [], facts: [], scopes: [], storeDir: "", available: false }),
    );
  }, []);

  const remember = useCallback(async (scope: string, note: string) => {
    await app.Remember(scope, note).catch(() => {});
  }, []);

  const saveDoc = useCallback(async (path: string, body: string) => {
    await app.SaveDoc(path, body).catch(() => {});
  }, []);

  // rewind restores the session to the start of a turn (scope "code" |
  // "conversation" | "both"), then reloads the transcript from the truncated
  // history so the view reflects the rewound state (unlike the CLI, the desktop
  // can re-render).
  const rewind = useCallback(async (turn: number, scope: string) => {
    // "fork" branches into a new session; "summ-*" compress the log; the rest
    // restore in place. All keep code intact (except the code/both restores).
    if (scope === "fork") {
      await app.Fork(turn).catch(() => {});
    } else if (scope === "summ-from") {
      await app.SummarizeFrom(turn).catch(() => {});
    } else if (scope === "summ-upto") {
      await app.SummarizeUpTo(turn).catch(() => {});
    } else {
      await app.Rewind(turn, scope).catch(() => {});
    }
    const messages = await app.History().catch(() => [] as HistoryMessage[]);
    dispatch({ type: "reset" });
    if (messages.length) dispatch({ type: "history", messages });
    app
      .ContextUsage()
      .then((context) => dispatch({ type: "context", context }))
      .catch(() => {});
  }, []);

  return {
    state,
    send,
    cancel,
    approve,
    answerQuestion,
    setPlan,
    setBypass,
    newSession,
    listSessions,
    resumeSession,
    deleteSession,
    renameSession,
    refreshMeta,
    pickWorkspace,
    compact,
    rewind,
    setModel,
    fetchMemory,
    remember,
    saveDoc,
  };
}
