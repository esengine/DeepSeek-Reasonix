// useController is the frontend's state machine over the agent's event stream. It
// maintains per-tab state so background tabs preserve their streaming output, tool
// states, and approvals when the user switches away and back. The active tab's state
// is what components render.

import { useCallback, useEffect, useRef, useState } from "react";
import { asArray } from "./array";
import { addBreadcrumb } from "./breadcrumbs";
import { app, onEvent, onReady } from "./bridge";
import { invalidateCache } from "./composerHistory";
import { createRafBatch } from "./rafBatch";
import { t } from "./i18n";
import { fileDiffFromWire, summarize, summarizeFileDiff, type ToolFileDiff } from "./tools";
import { modeHasAutoApproveTools } from "./types";
import type {
  BalanceInfo,
  CheckpointMeta,
  CollaborationMode,
  ContextInfo,
  EffortInfo,
  HistoryMessage,
  JobView,
  MemoryView,
  Meta,
  Mode,
  QuestionAnswer,
  SessionMeta,
  TabMeta,
  TokenMode,
  ToolApprovalMode,
  WireApproval,
  WireAsk,
  WireEvent,
  WireUsage,
} from "./types";

export type ToolStatus = "running" | "done" | "error" | "stopped";

export type LiveStream = { id: string; text: string; reasoning: string; reasoningComplete: boolean };
export type MessageActionScope = "fork" | "summ-from" | "summ-upto" | "conversation" | "code" | "both";
export type MessageActionState = { turn: number; scope: MessageActionScope };
export type HydrateReason = "switch-tab" | "new-session" | "resume-session" | "open-topic" | "startup";

export type Item =
  | { kind: "user"; id: string; text: string; submitText?: string; failed?: boolean; createdAt?: number }
  | { kind: "assistant"; id: string; text: string; reasoning: string; streaming: boolean; reasoningComplete?: boolean }
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
      dataArchived?: boolean; // args/output trimmed for memory; full data available via backend
      durationMs?: number;
      subject?: string; // stable collapsed subject from archived history payloads
      summary?: string; // stable collapsed readout kept even after args/output archive
      fileDiff?: ToolFileDiff; // previewed whole-file diff from writer dispatch
      isShell?: boolean; // true for !-prefix shell commands (controls default expand)
      parentId?: string; // a sub-agent call nests under the `task` call with this id
      profile?: { model?: string; effort?: string }; // subagent model/effort from tool event
    };

type ToolItem = Extract<Item, { kind: "tool" }>;

interface State {
  items: Item[];
  running: boolean;
  turnActive: boolean;
  pendingPrompt: boolean;
  backgroundJobs: number;
  cancelRequested: boolean;
  cancellable: boolean;
  approval?: WireApproval;
  ask?: WireAsk;
  usage?: WireUsage;
  context: ContextInfo;
  meta?: Meta;
  balance?: BalanceInfo;
  effort?: EffortInfo;
  jobs: JobView[];
  checkpoints: CheckpointMeta[];
  hydrating: boolean;
  hydrateReason?: HydrateReason;
  hydrateError?: string;
  backendActivationPending: boolean;
  messageAction?: MessageActionState;
  currentAssistant?: string;
  live?: LiveStream;
  pendingUser?: string;
  discardTurn?: boolean;
  turnStartAt: number;
  turnTokens: number;
  turnTotalTokens: number;
  turnCost: number;
  sessionTokens: number;
  sessionCost: number;
  sessionCurrency: string;
  retry?: { attempt: number; max: number };
  seq: number;
  sessionGen: number;
}

export const initialState: State = {
  items: [],
  running: false,
  turnActive: false,
  pendingPrompt: false,
  backgroundJobs: 0,
  cancelRequested: false,
  cancellable: false,
  context: { used: 0, window: 0, sessionTokens: 0 },
  jobs: [],
  checkpoints: [],
  hydrating: false,
  backendActivationPending: false,
  turnStartAt: 0,
  turnTokens: 0,
  turnTotalTokens: 0,
  turnCost: 0,
  sessionTokens: 0,
  sessionCost: 0,
  sessionCurrency: "¥",
  seq: 0,
  sessionGen: 0,
};

function usageTotalTokens(usage?: WireUsage): number {
  if (!usage) return 0;
  if (usage.totalTokens > 0) return usage.totalTokens;
  const promptTokens = usage.promptTokens || usage.cacheHitTokens + usage.cacheMissTokens;
  return Math.max(0, promptTokens + usage.completionTokens);
}

type RuntimeMetaSnapshot = {
  running: boolean;
  pendingPrompt?: boolean;
  backgroundJobs?: number;
  cancellable?: boolean;
};

export function foregroundRunningFromRuntimeMeta(meta: RuntimeMetaSnapshot): boolean {
  if (typeof meta.cancellable === "boolean") return meta.cancellable;
  if ((meta.backgroundJobs ?? 0) > 0 && !meta.pendingPrompt) return false;
  return Boolean(meta.running);
}

function updatesContextGauge(usage?: WireUsage): boolean {
  const source = usage?.source?.trim();
  return !source || source === "executor";
}

function metaFromTab(tab: TabMeta, existing?: Meta): Meta {
  const cwd = tab.cwd || tab.workspaceRoot || existing?.cwd || "";
  const autoApproveTools = existing?.autoApproveTools ?? modeHasAutoApproveTools(tab.mode);
  return {
    label: tab.label || existing?.label || "",
    ready: tab.ready,
    startupErr: tab.startupErr,
    eventChannel: existing?.eventChannel ?? "agent:event",
    cwd,
    workspaceRoot: tab.workspaceRoot || existing?.workspaceRoot || cwd,
    workspaceName: tab.workspaceName || existing?.workspaceName,
    workspacePath: tab.workspacePath || tab.workspaceRoot || existing?.workspacePath,
    gitBranch: tab.gitBranch || existing?.gitBranch,
    autoApproveTools,
    bypass: autoApproveTools,
    collaborationMode: tab.collaborationMode ?? existing?.collaborationMode ?? "normal",
    toolApprovalMode: tab.toolApprovalMode ?? existing?.toolApprovalMode ?? "ask",
    tokenMode: tab.tokenMode ?? existing?.tokenMode ?? "full",
    goal: tab.goal ?? existing?.goal,
    goalStatus: tab.goalStatus ?? existing?.goalStatus,
  };
}

function countsTowardCurrentTurn(state: State): boolean {
  return state.turnActive || state.running;
}

export function sameMeta(a?: Meta, b?: Meta): boolean {
  if (a === b) return true;
  if (!a || !b) return false;
  return (
    a.label === b.label &&
    a.ready === b.ready &&
    a.startupErr === b.startupErr &&
    a.eventChannel === b.eventChannel &&
    a.cwd === b.cwd &&
    a.workspaceRoot === b.workspaceRoot &&
    a.workspaceName === b.workspaceName &&
    a.workspacePath === b.workspacePath &&
    a.gitBranch === b.gitBranch &&
    a.autoApproveTools === b.autoApproveTools &&
    a.bypass === b.bypass &&
    a.collaborationMode === b.collaborationMode &&
    a.toolApprovalMode === b.toolApprovalMode &&
    a.tokenMode === b.tokenMode &&
    a.goal === b.goal &&
    a.goalStatus === b.goalStatus
  );
}

const STALE_TURN_RECONCILE_MS = 30_000;

export function shouldReconcileStaleTurn(
  state: Pick<State, "running" | "turnActive"> | undefined,
  lastTurnActivityAt: number,
  now = Date.now(),
  timeoutMs = STALE_TURN_RECONCILE_MS,
): boolean {
  if (!state?.running || !state.turnActive || lastTurnActivityAt <= 0) return false;
  return Math.max(0, now - lastTurnActivityAt) >= timeoutMs;
}

function hasCachedLiveTurn(state: State | undefined): boolean {
  if (!state?.running && !state?.turnActive) return false;
  if (state.live || state.currentAssistant || state.pendingUser !== undefined) return true;
  return state.items.some((item) =>
    (item.kind === "assistant" && item.streaming) ||
    (item.kind === "tool" && item.status === "running")
  );
}

/** Mirrors Go backend's ReadOnly() hints. */
export function isReadOnlyTool(name: string): boolean {
  switch (name) {
    case "read_file":
    case "ls":
    case "grep":
    case "glob":
    case "web_fetch":
    case "code_index":
    case "bash_output":
    case "waitJob":
    case "todo_write":
    case "read_skill":
      return true;
    default:
      return false;
  }
}

const ARCHIVED_TOOL_ARG_LIMIT = 200;

function archivedToolArgs(_name: string, args: string): string {
  return args && args.length > ARCHIVED_TOOL_ARG_LIMIT ? args.slice(0, ARCHIVED_TOOL_ARG_LIMIT) + "…" : args;
}

function isCanonicalTodoTool(tool: ToolItem): boolean {
  return tool.name === "todo_write" && !tool.parentId && tool.status === "done" && !tool.error;
}

function latestCanonicalTodoToolIndex(items: Item[]): number {
  for (let i = items.length - 1; i >= 0; i -= 1) {
    const item = items[i];
    if (item.kind === "tool" && isCanonicalTodoTool(item)) return i;
  }
  return -1;
}

function compactArchivedToolItems(items: Item[]): Item[] {
  const canonicalTodoIndex = latestCanonicalTodoToolIndex(items);
  return items.map((item, index) => {
    if (item.kind !== "tool" || item.status === "running") return item;
    const preserveArgs = index === canonicalTodoIndex;
    const nextArgs = preserveArgs ? item.args : archivedToolArgs(item.name, item.args);
    if (nextArgs === item.args && item.output === undefined && item.dataArchived === true) return item;
    return {
      ...item,
      args: nextArgs,
      output: undefined,
      dataArchived: true,
    };
  });
}

type Action =
  | { type: "event"; e: WireEvent }
  | { type: "user"; text: string; submitText?: string; seq: number }
  | { type: "unsend" }
  | { type: "send_failed"; error: string }
  | { type: "backend_status"; running: boolean; pendingPrompt?: boolean; backgroundJobs?: number; cancelRequested?: boolean; cancellable?: boolean }
  | { type: "cancel_requested" }
  | { type: "meta"; meta: Meta }
  | { type: "optimistic_meta"; meta: Meta }
  | { type: "context"; context: ContextInfo }
  | { type: "balance"; balance: BalanceInfo }
  | { type: "effort"; effort: EffortInfo }
  | { type: "jobs"; jobs: JobView[] }
  | { type: "checkpoints"; checkpoints: CheckpointMeta[] }
  | { type: "hydrate_start"; reason: HydrateReason }
  | { type: "hydrate_done" }
  | { type: "hydrate_error"; reason: HydrateReason; error: string }
  | { type: "backend_activation_start" }
  | { type: "backend_activation_done" }
  | { type: "message_action_start"; action: MessageActionState }
  | { type: "message_action_done" }
  | { type: "history"; messages: HistoryMessage[] }
  | { type: "local_notice"; level: "info" | "warn"; text: string }
  | { type: "clearApproval" }
  | { type: "clearAsk" }
  | { type: "reset" };

// ---- reducer helpers (unchanged logic) ----

export function historyMessagesToItems(messages: HistoryMessage[], idPrefix: string, startSeq = 0): { items: Item[]; seq: number } {
  const resultByID = new Map<string, HistoryMessage>();
  for (const m of messages) {
    if (m.role === "tool" && m.toolCallId && !resultByID.has(m.toolCallId)) {
      resultByID.set(m.toolCallId, m);
    }
  }
  const positionalResults = positionalToolResults(messages);
  const consumedPositionalToolIndexes = new Set(Array.from(positionalResults.values(), (result) => result.index));

  const items: Item[] = [];
  let seq = startSeq;
  const consumedToolIDs = new Set<string>();
  for (let messageIndex = 0; messageIndex < messages.length; messageIndex += 1) {
    const m = messages[messageIndex];
    if (m.role === "system") continue;
    if (m.role === "phase") {
      if (m.content.trim() !== "") {
        items.push({ kind: "phase", id: `${idPrefix}${seq}`, text: m.content });
        seq++;
      }
      continue;
    }
    if (m.role === "notice") {
      if (m.content.trim() !== "") {
        items.push({ kind: "notice", id: `${idPrefix}${seq}`, level: m.level === "warn" ? "warn" : "info", text: m.content });
        seq++;
      }
      continue;
    }
    if (m.role === "compaction") {
      items.push({
        kind: "compaction",
        id: `${idPrefix}${seq}`,
        pending: Boolean(m.pending),
        trigger: m.trigger ?? "",
        messages: m.messages ?? 0,
        summary: m.summary ?? "",
        archive: m.archive ?? "",
      });
      seq++;
      continue;
    }
    if (m.role === "user") {
      if (m.content.trim() === "") continue;
      items.push({ kind: "user", id: `${idPrefix}${seq}`, text: m.content, submitText: m.submitText, createdAt: m.createdAt });
      seq++;
      continue;
    }
    if (m.role === "assistant") {
      const hasText = m.content.trim() !== "" || (m.reasoning ?? "").trim() !== "";
      if (hasText) {
        items.push({ kind: "assistant", id: `${idPrefix}${seq}`, text: m.content, reasoning: m.reasoning ?? "", streaming: false });
        seq++;
      }
      const toolCalls = m.toolCalls ?? [];
      for (let callIndex = 0; callIndex < toolCalls.length; callIndex += 1) {
        const tc = toolCalls[callIndex];
        const positionalResult = tc.id ? undefined : positionalResults.get(positionalToolResultKey(messageIndex, callIndex));
        const result = tc.id ? resultByID.get(tc.id) : positionalResult?.message;
        if (tc.id) consumedToolIDs.add(tc.id);
        const archived = Boolean(tc.argumentsArchived || result?.toolResultArchived);
        const output = result?.toolResultArchived ? undefined : result?.content ?? "";
        const error = result?.toolResultError || (output ? historyToolError(output) : undefined);
        const fileDiff = fileDiffFromWire(tc);
        items.push({
          kind: "tool",
          id: tc.id || `${idPrefix}tool${seq}`,
          name: tc.name,
          args: tc.arguments ?? "",
          readOnly: isReadOnlyTool(tc.name),
          status: result ? (error ? "error" : "done") : "stopped",
          output,
          error,
          dataArchived: archived || undefined,
          subject: tc.subject,
          summary: summarizeFileDiff(fileDiff) || tc.summary,
          fileDiff,
          isShell: (tc.id || "").startsWith("shell-"),
        });
        seq++;
      }
      continue;
    }
    if (m.role === "tool") {
      if ((m.toolCallId && consumedToolIDs.has(m.toolCallId)) || consumedPositionalToolIndexes.has(messageIndex)) continue;
      const output = m.toolResultArchived ? undefined : m.content;
      const error = m.toolResultError || (output ? historyToolError(output) : undefined);
      items.push({
        kind: "tool",
        id: m.toolCallId || `${idPrefix}tool${seq}`,
        name: m.toolName || "tool",
        args: "",
        readOnly: isReadOnlyTool(m.toolName || "tool"),
        status: error ? "error" : "done",
        output,
        error,
        dataArchived: m.toolResultArchived || undefined,
        isShell: (m.toolCallId || "").startsWith("shell-"),
      });
      seq++;
      continue;
    }
  }
  return { items, seq };
}

function positionalToolResults(messages: HistoryMessage[]): Map<string, { message: HistoryMessage; index: number }> {
  const out = new Map<string, { message: HistoryMessage; index: number }>();
  const consumed = new Set<number>();
  for (let messageIndex = 0; messageIndex < messages.length; messageIndex += 1) {
    const message = messages[messageIndex];
    const toolCalls = message.role === "assistant" ? message.toolCalls ?? [] : [];
    if (toolCalls.length === 0) continue;
    let resultIndex = messageIndex + 1;
    for (let callIndex = 0; callIndex < toolCalls.length; callIndex += 1) {
      if (toolCalls[callIndex].id) continue;
      let matched = false;
      while (resultIndex < messages.length) {
        const candidate = messages[resultIndex];
        if (candidate.role !== "tool") break;
        const candidateIndex = resultIndex;
        resultIndex += 1;
        if (candidate.toolCallId || consumed.has(candidateIndex)) continue;
        consumed.add(candidateIndex);
        out.set(positionalToolResultKey(messageIndex, callIndex), { message: candidate, index: candidateIndex });
        matched = true;
        break;
      }
      if (!matched) break;
    }
  }
  return out;
}

function positionalToolResultKey(messageIndex: number, callIndex: number): string {
  return `${messageIndex}:${callIndex}`;
}

function historyToolError(output: string): string | undefined {
  const trimmed = output.trimStart();
  if (
    trimmed.startsWith("[error") ||
    trimmed.startsWith("Error:") ||
    trimmed.startsWith("error:") ||
    trimmed.startsWith("blocked:")
  ) {
    return output;
  }
  return undefined;
}

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
  const lastItem = s.items[s.items.length - 1];
  if (lastItem?.kind === "user" && lastItem.text === s.pendingUser) {
    return { ...s, pendingUser: undefined };
  }
  return {
    ...s,
    seq: s.seq + 1,
    items: [...s.items, { kind: "user", id: `u${s.seq}`, text: s.pendingUser, createdAt: Date.now() }],
    pendingUser: undefined,
  };
}

function applyEvent(s: State, e: WireEvent): State {
  if (s.discardTurn) {
    if (e.kind === "turn_done") return { ...s, discardTurn: false, running: false, turnActive: false, pendingPrompt: false, cancelRequested: false, cancellable: false, currentAssistant: undefined, live: undefined };
    return s;
  }
  if (s.pendingUser !== undefined && e.kind !== "turn_done") {
    s = flushPendingUser(s);
  }
  if (e.kind === "retrying") {
    return { ...s, retry: { attempt: e.retryAttempt ?? 0, max: e.retryMax ?? 0 } };
  }
  if (s.retry) s = { ...s, retry: undefined };
  switch (e.kind) {
    case "turn_started": {
      // Flush the user message and pre-create an empty assistant bubble
      // immediately so the user sees their message + a blinking cursor the
      // instant the backend acknowledges the turn — no dead gap waiting for
      // the first text/reasoning token.
      let cur: State = s;
      if (cur.pendingUser !== undefined) cur = flushPendingUser(cur);
      const { items, id, seq } = ensureAssistant(cur);
      return { ...cur, items, currentAssistant: id, seq, live: { id, text: "", reasoning: "", reasoningComplete: false }, running: true, turnActive: true, pendingPrompt: false, cancelRequested: false, cancellable: true, turnStartAt: Date.now(), turnTokens: 0, turnTotalTokens: 0, turnCost: 0 };
    }
    case "text":
    case "reasoning": {
      const { items, id, seq } = ensureAssistant(s);
      const delta = e.text ?? e.reasoning ?? "";
      const base = s.live?.id === id ? s.live : { id, text: "", reasoning: "", reasoningComplete: false };
      const live =
        e.kind === "text"
          ? { ...base, text: base.text + delta, reasoningComplete: base.reasoning !== "" || base.reasoningComplete }
          : { ...base, reasoning: base.reasoning + delta, reasoningComplete: false };
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
      // Skip partial dispatches (name-only, no args yet) — the full dispatch
      // with complete args follows from executeBatch. Waiting for the full
      // dispatch means the tool card appears with name + subject at once,
      // avoiding a "name → command" visual jump.
      if (t.partial) return s;
      const id = t.id || `tool${s.seq}`;
      const idx = s.items.findIndex((it) => it.kind === "tool" && it.id === id);
      if (idx >= 0) {
        const next = [...s.items];
        const it = next[idx];
        if (it.kind === "tool") {
          const args = t.args ? t.args : it.args;
          const fileDiff = fileDiffFromWire(t);
          const summary = summarizeFileDiff(fileDiff) || summarize(t.name, args) || (t.name === it.name && args === it.args ? it.summary : undefined);
          next[idx] = { ...it, name: t.name, args, readOnly: t.readOnly, profile: t.profile ?? it.profile, summary, fileDiff };
        }
        return { ...s, items: next };
      }
      const args = t.args ?? "";
      const fileDiff = fileDiffFromWire(t);
      return { ...s, seq: s.seq + 1, items: [...s.items, { kind: "tool", id, name: t.name, args, readOnly: t.readOnly, status: "running", summary: summarizeFileDiff(fileDiff) || summarize(t.name, args), fileDiff, isShell: id.startsWith("shell-"), parentId: t.parentId, profile: t.profile }] };
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
        if (it.kind === "tool") {
          // Archive immediately: collapsed cards only show tool name + command
          // subject (from args). Drop output entirely; full data is loaded on
          // demand via app.ToolResultForTab when the card is expanded.
          const existing = it;
          const summary = t.err ? undefined : existing.summary || summarize(existing.name, existing.args, t.output);
          next[idx] = {
            ...existing,
            status: t.err ? "error" : "done",
            output: t.output,
            error: t.err,
            truncated: t.truncated,
            durationMs: t.durationMs,
            summary,
          };
        }
      }
      return { ...s, items: compactArchivedToolItems(next) };
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
      if (!countsTowardCurrentTurn(s)) return s;
      const updateContextGauge = updatesContextGauge(e.usage);
      const used = e.usage && s.context.window && updateContextGauge ? e.usage.promptTokens : s.context.used;
      const turnTokens = s.turnTokens + (e.usage?.completionTokens ?? 0);
      const usageTokens = usageTotalTokens(e.usage);
      const turnTotalTokens = s.turnTotalTokens + usageTokens;
      const sessionTokens = s.sessionTokens + usageTokens;
      const usageCost = e.usage?.cost ?? e.usage?.costUsd ?? 0;
      const turnCost = s.turnCost + usageCost;
      const sessionCost = s.sessionCost + usageCost;
      const sessionCurrency = e.usage?.currency || s.sessionCurrency || "¥";
      const usage = updateContextGauge ? e.usage : s.usage ?? e.usage;
      return { ...s, usage, context: { ...s.context, used, sessionTokens }, turnTokens, turnTotalTokens, turnCost, sessionTokens, sessionCost, sessionCurrency };
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
    case "steer":
      return { ...s, seq: s.seq + 1, items: [...s.items, { kind: "notice", id: `s${s.seq}`, level: "info", text: `↪ ${e.text ?? ""}` }] };
    case "approval_request": {
      if (s.cancelRequested) return s;
      return { ...s, approval: e.approval, pendingPrompt: true, running: true, turnActive: true, cancellable: true };
    }
    case "ask_request": {
      if (s.cancelRequested) return s;
      return { ...s, ask: e.ask, pendingPrompt: true, running: true, turnActive: true, cancellable: true };
    }
    case "turn_done": {
      if (s.pendingUser !== undefined) s = flushPendingUser(s);
      const finalized = s.items.map((it) => {
        if (it.kind === "assistant" && s.live && it.id === s.live.id) return { ...it, text: s.live.text, reasoning: s.live.reasoning, streaming: false };
        if (it.kind === "assistant" && it.streaming) return { ...it, streaming: false };
        if (it.kind === "tool" && it.status === "running") return { ...it, status: "stopped" as const };
        return it;
      });
      let items: Item[] = e.err ? [...finalized, { kind: "notice", id: `e${s.seq}`, level: "warn", text: e.err }] : finalized;
      // Plan approval can arrive before turn_done on some Wails event paths.
      // Keep that gate visible instead of clearing the only UI that can answer it.
      const keepPlanApproval = s.approval?.tool === "exit_plan_mode";
      return {
        ...s,
        items,
        live: undefined,
        running: keepPlanApproval,
        turnActive: keepPlanApproval,
        pendingPrompt: keepPlanApproval,
        cancelRequested: false,
        cancellable: keepPlanApproval,
        currentAssistant: undefined,
        approval: keepPlanApproval ? s.approval : undefined,
        ask: undefined,
        seq: s.seq + 1,
      };
    }
    default: return s;
  }
}

export function reducer(s: State, a: Action): State {
  switch (a.type) {
    case "user": {
      const seq = a.seq !== undefined ? a.seq : s.seq;
      return {
        ...s,
        seq: seq + 1,
        items: [...s.items, { kind: "user", id: `u${seq}`, text: a.text, submitText: a.submitText, createdAt: Date.now() }],
        running: true,
        pendingPrompt: false,
        cancelRequested: false,
        cancellable: true,
        turnStartAt: Date.now(),
        turnTokens: 0,
        turnTotalTokens: 0,
        turnCost: 0,
        pendingUser: a.text,
        discardTurn: false,
      };
    }
    case "unsend": return { ...s, pendingUser: undefined, discardTurn: true, running: false, pendingPrompt: false, cancelRequested: true, cancellable: false, approval: undefined, ask: undefined, live: undefined };
    case "cancel_requested": return { ...s, pendingPrompt: false, cancelRequested: true, approval: undefined, ask: undefined, cancellable: s.running || s.turnActive };
    case "send_failed": {
      if (s.pendingUser === undefined) return s;
      let idx = -1;
      for (let i = s.items.length - 1; i >= 0; i--) {
        const it = s.items[i];
        if (it.kind === "user" && it.text === s.pendingUser) { idx = i; break; }
      }
      const items = idx >= 0 ? s.items.map((it, i) => (i === idx ? { ...it, failed: true } : it)) : s.items;
      const notice: Item = { kind: "notice", id: `n${s.seq}`, level: "warn", text: a.error };
      return { ...s, pendingUser: undefined, running: false, turnActive: false, pendingPrompt: false, cancelRequested: false, cancellable: false, live: undefined, seq: s.seq + 1, items: [...items, notice] };
    }
    case "backend_status": {
      const pendingPrompt = Boolean(a.pendingPrompt);
      const backgroundJobs = Math.max(0, a.backgroundJobs ?? s.backgroundJobs ?? 0);
      const cancelRequested = Boolean(a.cancelRequested);
      const foregroundRunning = foregroundRunningFromRuntimeMeta({ running: a.running, pendingPrompt, backgroundJobs, cancellable: a.cancellable });
      const cancellable = foregroundRunning;
      if (
        foregroundRunning === s.running &&
        pendingPrompt === s.pendingPrompt &&
        backgroundJobs === s.backgroundJobs &&
        cancelRequested === s.cancelRequested &&
        cancellable === s.cancellable
      ) return s;
      if (foregroundRunning) {
        return {
          ...s,
          running: true,
          turnActive: true,
          pendingPrompt,
          backgroundJobs,
          cancelRequested,
          cancellable,
          turnStartAt: s.turnStartAt || Date.now(),
        };
      }
      const finalized = s.items.map((it) => {
        if (it.kind === "assistant" && s.live && it.id === s.live.id) return { ...it, text: s.live.text, reasoning: s.live.reasoning, streaming: false };
        if (it.kind === "assistant" && it.streaming) return { ...it, streaming: false };
        if (it.kind === "tool" && it.status === "running") return { ...it, status: "stopped" as const };
        return it;
      });
      return { ...s, items: finalized, running: false, turnActive: false, pendingPrompt, backgroundJobs, cancelRequested, cancellable, live: undefined, currentAssistant: undefined, approval: undefined, ask: undefined };
    }
    case "meta": return sameMeta(s.meta, a.meta) ? s : { ...s, meta: a.meta };
    case "optimistic_meta": return sameMeta(s.meta, a.meta) ? s : { ...s, meta: a.meta, hydrateError: undefined };
    case "context": {
      const sessionTokens = typeof a.context.sessionTokens === "number"
        ? Math.max(0, a.context.sessionTokens)
        : s.sessionTokens;
      return { ...s, context: a.context, sessionTokens };
    }
    case "balance": return { ...s, balance: a.balance };
    case "effort": return { ...s, effort: a.effort };
    case "jobs": return { ...s, jobs: a.jobs };
    case "checkpoints": return { ...s, checkpoints: a.checkpoints };
    case "hydrate_start": return { ...s, hydrating: true, hydrateReason: a.reason, hydrateError: undefined };
    case "hydrate_done": return s.hydrating || s.hydrateReason || s.hydrateError ? { ...s, hydrating: false, hydrateReason: undefined, hydrateError: undefined } : s;
    case "hydrate_error": return { ...s, hydrating: false, hydrateReason: a.reason, hydrateError: a.error };
    case "backend_activation_start": return s.backendActivationPending ? s : { ...s, backendActivationPending: true };
    case "backend_activation_done": return s.backendActivationPending ? { ...s, backendActivationPending: false } : s;
    case "message_action_start": return { ...s, messageAction: a.action };
    case "message_action_done": return { ...s, messageAction: undefined };
    case "history": {
      const { items, seq } = historyMessagesToItems(a.messages, "h", s.seq);
      return { ...s, items: compactArchivedToolItems(items), seq };
    }
    case "local_notice": return { ...s, running: false, turnActive: false, seq: s.seq + 1, items: [...s.items, { kind: "notice", id: `n${s.seq}`, level: a.level, text: a.text }] };
    case "clearApproval": return { ...s, approval: undefined, pendingPrompt: false };
    case "clearAsk": return { ...s, ask: undefined, pendingPrompt: false };
    case "reset": return { ...initialState, meta: s.meta, context: { ...s.context, used: 0, sessionTokens: 0 }, balance: s.balance, effort: s.effort, jobs: s.jobs, hydrating: s.hydrating, hydrateReason: s.hydrateReason, hydrateError: s.hydrateError, backendActivationPending: s.backendActivationPending, sessionGen: s.sessionGen + 1 };
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

function messageActionBusyText(scope: MessageActionScope): string {
  switch (scope) {
    case "fork":
      return t("rewind.busyFork");
    case "summ-from":
      return t("rewind.busySummFrom");
    case "summ-upto":
      return t("rewind.busySummUpto");
    case "conversation":
      return t("rewind.busyConversation");
    case "code":
      return t("rewind.busyCode");
    default:
      return t("rewind.busyBoth");
  }
}

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  if (typeof err === "string") return err;
  return String(err || "");
}

async function refreshMetaForTab(tabId: string, dispatchTo: (tabId: string, action: Action) => void): Promise<void> {
  try {
    dispatchTo(tabId, { type: "meta", meta: await app.MetaForTab(tabId) });
    dispatchTo(tabId, { type: "context", context: await app.ContextUsageForTab(tabId) });
    dispatchTo(tabId, { type: "effort", effort: await app.EffortForTab(tabId) });
  } catch {
    /* ignore */
  }
}

export function replayPendingPromptsForActiveTab(activeTabId: string | undefined, replay: () => Promise<void> = () => app.ReplayPendingPrompts()): void {
  if (!activeTabId) return;
  void replay().catch(() => {});
}

export function useController() {
  const statesRef = useRef<TabStates>(new Map());
  const lastTurnActivityAtByTab = useRef(new Map<string, number>());
  const [activeTabId, setActiveTabId] = useState<string | undefined>();
  const activeTabIdRef = useRef<string | undefined>(undefined);
  // A render-triggering counter so that mutations to a non-active tab's state still
  // cause a re-render when that tab becomes active.
  const [, setVersion] = useState(0);
  const bump = useCallback(() => setVersion((v) => v + 1), []);

  // The active tab's current state, with a stable identity for cancel().
  const activeState = activeTabId ? getOrCreateState(statesRef.current, activeTabId) : initialState;
  const stateRef = useRef(activeState);
  const backendActiveTabIdRef = useRef<string | undefined>(undefined);
  const backendActivationPromises = useRef(new Map<string, Promise<boolean>>());
  activeTabIdRef.current = activeTabId;
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

  const confirmBackendActiveTab = useCallback((tabId: string) => {
    backendActiveTabIdRef.current = tabId;
    dispatchTo(tabId, { type: "backend_activation_done" });
  }, [dispatchTo]);

  const trackBackendActivation = useCallback((tabId: string, promise: Promise<boolean>) => {
    backendActivationPromises.current.set(tabId, promise);
    void promise.finally(() => {
      if (backendActivationPromises.current.get(tabId) === promise) {
        backendActivationPromises.current.delete(tabId);
      }
    });
  }, []);

  const waitForBackendActiveTab = useCallback(async (tabId: string): Promise<boolean> => {
    const pending = backendActivationPromises.current.get(tabId);
    if (pending) {
      const activated = await pending.catch(() => false);
      if (!activated) return false;
    }
    return backendActiveTabIdRef.current === tabId && activeTabIdRef.current === tabId;
  }, []);

  const checkpointRefreshSeq = useRef(new Map<string, number>());
  const sessionLoadSeq = useRef(new Map<string, number>());
  const bumpSessionLoadSeq = useCallback((tabId: string): number => {
    const seq = (sessionLoadSeq.current.get(tabId) ?? 0) + 1;
    sessionLoadSeq.current.set(tabId, seq);
    return seq;
  }, []);
  const sessionLoadCurrent = useCallback((tabId: string, seq: number): boolean => {
    return sessionLoadSeq.current.get(tabId) === seq;
  }, []);
  const bumpCheckpointRefreshSeq = useCallback((tabId: string): number => {
    const seq = (checkpointRefreshSeq.current.get(tabId) ?? 0) + 1;
    checkpointRefreshSeq.current.set(tabId, seq);
    return seq;
  }, []);
  const refreshCheckpoints = useCallback(async (tabId: string) => {
    const seq = bumpCheckpointRefreshSeq(tabId);
    const checkpoints = await app.CheckpointsForTab(tabId).catch(() => undefined);
    if (checkpointRefreshSeq.current.get(tabId) !== seq || checkpoints === undefined) return;
    dispatchTo(tabId, { type: "checkpoints", checkpoints: asArray(checkpoints) });
  }, [bumpCheckpointRefreshSeq, dispatchTo]);

  const loadSessionDataForTab = useCallback(async (
    tabId: string,
    reset = false,
    reason: HydrateReason = "startup",
    options: { skipHistory?: boolean } = {},
  ) => {
    const seq = bumpSessionLoadSeq(tabId);
    const hydrateStartedAt = Date.now();
    addBreadcrumb("tab.hydrate", `start ${reason} ${tabId}`);
    dispatchTo(tabId, { type: "hydrate_start", reason });
    if (reset && sessionLoadCurrent(tabId, seq)) dispatchTo(tabId, { type: "reset" });

    const stillCurrent = () => sessionLoadCurrent(tabId, seq);
    const noteFailure = (label: string, err: unknown) => {
      addBreadcrumb("tab.hydrate", `${label} failed ${tabId}: ${errorMessage(err)}`);
    };

    const metaTask = app.MetaForTab(tabId)
      .then((meta) => {
        if (stillCurrent()) dispatchTo(tabId, { type: "meta", meta });
      })
      .catch((err) => noteFailure("meta", err));
    const effortTask = app.EffortForTab(tabId)
      .then((effort) => {
        if (stillCurrent()) dispatchTo(tabId, { type: "effort", effort });
      })
      .catch((err) => noteFailure("effort", err));
    const jobsTask = app.JobsForTab(tabId)
      .then((jobs) => {
        if (stillCurrent()) dispatchTo(tabId, { type: "jobs", jobs: asArray(jobs) });
      })
      .catch((err) => noteFailure("jobs", err));

    const historyStartedAt = Date.now();
    const historyTask = options.skipHistory
      ? Promise.resolve().then(() => {
        addBreadcrumb("tab.hydrate", `history skipped ${tabId} reason=cached-live-turn`);
        if (reason === "switch-tab") {
          addBreadcrumb("tab.switch", `history-done ${tabId} skipped ms=${Date.now() - historyStartedAt}`);
        }
      })
      : app.HistoryForTab(tabId)
        .then((history) => {
          if (!stillCurrent()) return;
          const messages = asArray(history);
          const reduceStartedAt = Date.now();
          if (messages.length) dispatchTo(tabId, { type: "history", messages });
          addBreadcrumb(
            "tab.hydrate",
            `history done ${tabId} count=${messages.length} apiMs=${Date.now() - historyStartedAt} reduceMs=${Date.now() - reduceStartedAt}`,
          );
          if (reason === "switch-tab") {
            addBreadcrumb(
              "tab.switch",
              `history-done ${tabId} count=${messages.length} ms=${Date.now() - historyStartedAt}`,
            );
          }
        })
        .catch((err) => noteFailure("history", err));
    const checkpointsTask = app.CheckpointsForTab(tabId)
      .then((checkpoints) => {
        if (stillCurrent()) dispatchTo(tabId, { type: "checkpoints", checkpoints: asArray(checkpoints) });
      })
      .catch((err) => noteFailure("checkpoints", err));
    const contextTask = app.ContextUsageForTab(tabId)
      .then((context) => {
        if (stillCurrent()) dispatchTo(tabId, { type: "context", context });
      })
      .catch((err) => noteFailure("context", err));
    const balanceTask = app.BalanceForTab(tabId)
      .then((balance) => {
        if (stillCurrent()) dispatchTo(tabId, { type: "balance", balance });
      })
      .catch((err) => noteFailure("balance", err));

    await Promise.all([metaTask, effortTask, jobsTask, historyTask, checkpointsTask, contextTask, balanceTask]);
    if (!stillCurrent()) return;
    dispatchTo(tabId, { type: "hydrate_done" });
    addBreadcrumb("tab.hydrate", `done ${reason} ${tabId} ms=${Date.now() - hydrateStartedAt}`);
  }, [bumpSessionLoadSeq, dispatchTo, sessionLoadCurrent]);

  const activeTabFromBackend = useCallback(async (): Promise<TabMeta | undefined> => {
    const tabs = asArray(await app.ListTabs().catch(() => [] as TabMeta[]));
    return tabs.find((tab) => tab.active) ?? tabs[0];
  }, []);

  const waitForTabReady = useCallback(async (tabId: string): Promise<void> => {
    for (let attempt = 0; attempt < 60; attempt += 1) {
      const tabs = asArray(await app.ListTabs().catch(() => [] as TabMeta[]));
      const tab = tabs.find((candidate) => candidate.id === tabId);
      if (!tab || tab.ready || tab.startupErr) return;
      await new Promise((resolve) => window.setTimeout(resolve, 100));
    }
  }, []);

  const syncActiveTabFromBackend = useCallback(async (reset = false): Promise<string | undefined> => {
    const active = await activeTabFromBackend();
    if (!active) return undefined;
    setActiveTabId(active.id);
    activeTabIdRef.current = active.id;
    confirmBackendActiveTab(active.id);
    dispatchTo(active.id, { type: "optimistic_meta", meta: metaFromTab(active, statesRef.current.get(active.id)?.meta) });
    await loadSessionDataForTab(active.id, reset, "startup");
    return active.id;
  }, [activeTabFromBackend, confirmBackendActiveTab, dispatchTo, loadSessionDataForTab]);

  const reconcileTabRuntime = useCallback(async (
    tabId: string,
    options: { hydrateSessionData?: boolean } = {},
  ): Promise<TabMeta[] | undefined> => {
    const hydrateSessionData = options.hydrateSessionData ?? true;
    const tabs = asArray(await app.ListTabs().catch(() => [] as TabMeta[]));
    const tab = tabs.find((candidate) => candidate.id === tabId);
    if (!tab) return undefined;
    const local = statesRef.current.get(tabId);
    const needsInitialLoad = !local?.meta;
    const foregroundRunning = foregroundRunningFromRuntimeMeta(tab);
    const missedTurnDone = Boolean(local?.running && !foregroundRunning);
    dispatchTo(tabId, {
      type: "backend_status",
      running: foregroundRunning,
      pendingPrompt: Boolean(tab.pendingPrompt),
      backgroundJobs: tab.backgroundJobs ?? 0,
      cancelRequested: Boolean(tab.cancelRequested),
      cancellable: foregroundRunning,
    });
    // backend_status reconciliation can clear a live prompt from frontend state.
    // If the backend is still blocked, ask it to replay the approval/ask event.
    if (tab.pendingPrompt) replayPendingPromptsForActiveTab(tabId);
    if (hydrateSessionData && (needsInitialLoad || missedTurnDone)) {
      await loadSessionDataForTab(tabId, missedTurnDone, "startup");
      return tabs;
    }
    const [jobs, effort, balance] = await Promise.all([
      app.JobsForTab(tabId).catch(() => undefined),
      app.EffortForTab(tabId).catch(() => undefined),
      app.BalanceForTab(tabId).catch(() => undefined),
    ]);
    if (jobs) dispatchTo(tabId, { type: "jobs", jobs: asArray(jobs) });
    if (effort) dispatchTo(tabId, { type: "effort", effort });
    if (balance) dispatchTo(tabId, { type: "balance", balance });
    return tabs;
  }, [dispatchTo, loadSessionDataForTab]);

  useEffect(() => {
    const textBatch = createRafBatch<{ tabId: string; e: WireEvent }>((batch) => {
      for (const { tabId, e } of batch) dispatchTo(tabId, { type: "event", e });
    });
    const off = onEvent((e) => {
      const targetTabId = e.tabId || activeTabIdRef.current;
      if (!targetTabId) return;
      if (
        e.kind === "turn_started" ||
        e.kind === "text" ||
        e.kind === "reasoning" ||
        e.kind === "message" ||
        e.kind === "tool_dispatch" ||
        e.kind === "tool_progress" ||
        e.kind === "tool_result"
      ) {
        lastTurnActivityAtByTab.current.set(targetTabId, Date.now());
      }
      if (e.kind === "text" || e.kind === "reasoning") {
        textBatch.push({ tabId: targetTabId, e });
      } else {
        textBatch.drain();
        dispatchTo(targetTabId, { type: "event", e });
      }
      if (e.kind === "turn_done") {
        void refreshMetaForTab(targetTabId, dispatchTo);
        app
          .ContextUsageForTab(targetTabId)
          .then((context) => dispatchTo(targetTabId, { type: "context", context }))
          .catch(() => {});
        app.BalanceForTab(targetTabId).then((balance) => dispatchTo(targetTabId, { type: "balance", balance })).catch(() => {});
        app.EffortForTab(targetTabId).then((effort) => dispatchTo(targetTabId, { type: "effort", effort })).catch(() => {});
        void refreshCheckpoints(targetTabId);
      }
      if (e.kind === "turn_done" || e.kind === "notice") {
        app.JobsForTab(targetTabId).then((jobs) => dispatchTo(targetTabId, { type: "jobs", jobs: asArray(jobs) })).catch(() => {});
      }
    });

    const offReady = onReady(() => {
      const readyTabId = activeTabIdRef.current;
      if (readyTabId) {
        void loadSessionDataForTab(readyTabId, false, "startup");
        return;
      }
      void syncActiveTabFromBackend();
    });

    void syncActiveTabFromBackend();
    // The event subscription is live now, so ask the backend to re-emit any
    // approval/ask prompt that was already blocking a tab before this load —
    // otherwise a session left mid-confirmation shows "waiting" with no modal
    // and no way to stop (#3844).
    void app.ReplayPendingPrompts().catch(() => {});

    return () => { textBatch.drain(); off(); offReady(); };
  }, [dispatchTo, loadSessionDataForTab, refreshCheckpoints, syncActiveTabFromBackend]);

  // Stale-turn watchdog: if the frontend thinks the agent is running but the
  // turn stream has gone quiet, reconcile with the backend. This catches cases
  // where the Wails event channel silently drops turn_done after the final
  // message or synthetic todo update has already closed the live stream.
  useEffect(() => {
    if (!activeTabId) return;
    const s = statesRef.current.get(activeTabId);
    const now = Date.now();
    const lastTurnActivityAt = lastTurnActivityAtByTab.current.get(activeTabId) ?? 0;
    if (!s?.running || !s.turnActive || lastTurnActivityAt <= 0) return;
    const since = Math.max(0, now - lastTurnActivityAt);
    if (shouldReconcileStaleTurn(s, lastTurnActivityAt, now)) {
      void reconcileTabRuntime(activeTabId);
      return;
    }
    const timer = window.setTimeout(() => {
      const cur = statesRef.current.get(activeTabId);
      const lastActivity = lastTurnActivityAtByTab.current.get(activeTabId) ?? 0;
      if (shouldReconcileStaleTurn(cur, lastActivity)) {
        void reconcileTabRuntime(activeTabId);
      }
    }, STALE_TURN_RECONCILE_MS - since);
    return () => window.clearTimeout(timer);
  }, [activeTabId, reconcileTabRuntime, activeState.running, activeState.turnActive]);

  // Replay any pending approval/ask prompts when switching tabs, so a
  // plan-mode session left awaiting confirmation rebuilds its modal (#4275).
  useEffect(() => {
    replayPendingPromptsForActiveTab(activeTabId);
  }, [activeTabId]);

  const sendToTab = useCallback((tabId: string, displayText: string, submitText = displayText) => {
    if (!tabId) return;
    const seq = getOrCreateState(statesRef.current, tabId).seq;
    const display = displayText.trim();
    const submit = submitText.trim();
    dispatchTo(tabId, { type: "user", text: displayText, submitText: display !== submit ? submit : undefined, seq });
    invalidateCache();
    (display !== submit ? app.SubmitDisplayToTab(tabId, display, submit) : app.SubmitToTab(tabId, submit)).catch((error) => {
      dispatchTo(tabId, { type: "send_failed", error: `Send failed: ${error instanceof Error ? error.message : String(error)}` });
    });
  }, [dispatchTo]);

  const send = useCallback((displayText: string, submitText = displayText) => {
    const tabId = activeTabIdRef.current ?? activeTabId;
    if (tabId) {
      sendToTab(tabId, displayText, submitText);
      return;
    }
    void activeTabFromBackend().then((active) => {
      if (!active?.id) return;
      setActiveTabId(active.id);
      activeTabIdRef.current = active.id;
      confirmBackendActiveTab(active.id);
      sendToTab(active.id, displayText, submitText);
    });
  }, [activeTabFromBackend, activeTabId, confirmBackendActiveTab, sendToTab]);

  const runShell = useCallback((command: string) => {
    if (!activeTabId) return;
    dispatchTo(activeTabId, { type: "user", text: `!${command}`, seq: getOrCreateState(statesRef.current, activeTabId).seq });
    app.RunShellForTab(activeTabId, command).catch(() => {});
  }, [activeTabId, dispatchTo]);

  const steer = useCallback((text: string) => {
    if (!activeTabId) return;
    // No optimistic user bubble: rewind/fork map turns by counting user items,
    // and a steer is not a backend turn — the Steer event's ↪ notice is the
    // visible confirmation (#3660).
    app.SteerForTab(activeTabId, text).catch(() => {});
  }, [activeTabId]);

  const notice = useCallback((text: string, level: "info" | "warn" = "info") => {
    if (!activeTabId) return;
    dispatchTo(activeTabId, { type: "local_notice", level, text });
  }, [activeTabId, dispatchTo]);

  const cancel = useCallback((): string | undefined => {
    const cur = stateRef.current;
    const tabId = activeTabId;
    if (cur.running && cur.pendingUser !== undefined) {
      const text = cur.pendingUser;
      if (tabId) {
        dispatchTo(tabId, { type: "unsend" });
        app.CancelTab(tabId).catch(() => {});
      }
      return text;
    }
    if (tabId) {
      dispatchTo(tabId, { type: "cancel_requested" });
      app.CancelTab(tabId).catch(() => {});
    }
    return undefined;
  }, [activeTabId, dispatchTo]);

  const approve = useCallback((id: string, allow: boolean, session: boolean, persist: boolean) => {
    if (!activeTabId) return;
    dispatchTo(activeTabId, { type: "clearApproval" });
    app.ApproveTab(activeTabId, id, allow, session, persist).catch(() => {});
  }, [activeTabId, dispatchTo]);

  const answerQuestion = useCallback((id: string, answers: QuestionAnswer[]) => {
    if (!activeTabId) return;
    dispatchTo(activeTabId, { type: "clearAsk" });
    app.AnswerQuestionForTab(activeTabId, id, answers).catch(() => {});
  }, [activeTabId, dispatchTo]);

  const setControllerMode = useCallback((mode: Mode): Promise<void> => {
    if (!activeTabId) return Promise.resolve();
    return app.SetModeForTab(activeTabId, mode).then(() => {
      if (modeHasAutoApproveTools(mode) && activeTabId) dispatchTo(activeTabId, { type: "clearApproval" });
    }).catch(() => {});
  }, [activeTabId, dispatchTo]);

  const setCollaborationMode = useCallback(async (mode: CollaborationMode): Promise<void> => {
    if (!activeTabId) return;
    await app.SetCollaborationModeForTab(activeTabId, mode).catch(() => {});
    await refreshMetaForTab(activeTabId, dispatchTo);
  }, [activeTabId, dispatchTo]);

  const setToolApprovalMode = useCallback(async (mode: ToolApprovalMode): Promise<void> => {
    if (!activeTabId) return;
    await app.SetToolApprovalModeForTab(activeTabId, mode).catch(() => {});
    if (mode === "auto" || mode === "yolo") dispatchTo(activeTabId, { type: "clearApproval" });
    await refreshMetaForTab(activeTabId, dispatchTo);
  }, [activeTabId, dispatchTo]);

  const setGoal = useCallback(async (goal: string): Promise<void> => {
    if (!activeTabId) return;
    await app.SetGoalForTab(activeTabId, goal).catch(() => {});
    await refreshMetaForTab(activeTabId, dispatchTo);
  }, [activeTabId, dispatchTo]);

  const clearGoal = useCallback(async (): Promise<void> => {
    if (!activeTabId) return;
    await app.ClearGoalForTab(activeTabId).catch(() => {});
    await refreshMetaForTab(activeTabId, dispatchTo);
  }, [activeTabId, dispatchTo]);

  const newSession = useCallback(async () => {
    const tabId = activeTabId;
    if (tabId && !(await waitForBackendActiveTab(tabId))) return;
    if (tabId) {
      addBreadcrumb("session.new", `click ${tabId}`);
      bumpCheckpointRefreshSeq(tabId);
      bumpSessionLoadSeq(tabId);
      dispatchTo(tabId, { type: "reset" });
      dispatchTo(tabId, { type: "hydrate_start", reason: "new-session" });
      addBreadcrumb("session.new", `visible-reset ${tabId}`);
    }
    try {
      await app.NewSession();
      addBreadcrumb("session.new", `backend-done ${tabId ?? ""}`);
    } catch (err) {
      if (tabId) {
        dispatchTo(tabId, { type: "hydrate_error", reason: "new-session", error: errorMessage(err) });
        void loadSessionDataForTab(tabId, true, "new-session").then(() => {
          dispatchTo(tabId, { type: "local_notice", level: "warn", text: `New session failed: ${errorMessage(err)}` });
        });
      }
      return; // backend refused (workspace starting / failed) — keep the transcript
    }
    invalidateCache();
    if (tabId) {
      dispatchTo(tabId, { type: "hydrate_done" });
      void refreshMetaForTab(tabId, dispatchTo);
      app.ContextUsageForTab(tabId).then((context) => dispatchTo(tabId, { type: "context", context })).catch(() => {});
      void refreshCheckpoints(tabId);
    }
  }, [activeTabId, bumpCheckpointRefreshSeq, bumpSessionLoadSeq, dispatchTo, loadSessionDataForTab, refreshCheckpoints, waitForBackendActiveTab]);

  const clearSession = useCallback(async () => {
    const tabId = activeTabId;
    if (tabId && !(await waitForBackendActiveTab(tabId))) {
      throw new Error("Tab activation has not completed");
    }
    if (tabId) {
      bumpCheckpointRefreshSeq(tabId);
      bumpSessionLoadSeq(tabId);
    }
    try {
      await app.ClearSession();
    } catch {
      if (tabId) void loadSessionDataForTab(tabId);
      return;
    }
    if (tabId) bumpSessionLoadSeq(tabId);
    invalidateCache();
    if (tabId) dispatchTo(tabId, { type: "reset" });
  }, [activeTabId, bumpCheckpointRefreshSeq, bumpSessionLoadSeq, dispatchTo, loadSessionDataForTab, waitForBackendActiveTab]);

  const listSessions = useCallback(async (): Promise<SessionMeta[]> => asArray<SessionMeta>(await app.ListSessions().catch(() => [])), []);
  const listTrashedSessions = useCallback(async (): Promise<SessionMeta[]> => asArray<SessionMeta>(await app.ListTrashedSessions().catch(() => [])), []);
  const resumeSession = useCallback(async (path: string, tabId?: string) => {
    const targetTabId = tabId || activeTabId;
    if (!targetTabId) return;
    if (tabId) await waitForTabReady(tabId);
    else if (!(await waitForBackendActiveTab(targetTabId))) return;
    const seq = bumpSessionLoadSeq(targetTabId);
    dispatchTo(targetTabId, { type: "hydrate_start", reason: "resume-session" });
    let messages: HistoryMessage[];
    try {
      messages = asArray(await (tabId ? app.ResumeSessionForTab(tabId, path) : app.ResumeSession(path)));
    } catch (err) {
      if (sessionLoadCurrent(targetTabId, seq)) {
        dispatchTo(targetTabId, { type: "hydrate_error", reason: "resume-session", error: errorMessage(err) });
        dispatchTo(targetTabId, { type: "local_notice", level: "warn", text: `${t("history.failedOpenSession")}: ${errorMessage(err)}` });
      }
      return;
    }
    if (!sessionLoadCurrent(targetTabId, seq)) return;
    dispatchTo(targetTabId, { type: "reset" });
    if (messages.length) dispatchTo(targetTabId, { type: "history", messages });
    dispatchTo(targetTabId, { type: "hydrate_done" });
    app.ContextUsageForTab(targetTabId).then((context) => dispatchTo(targetTabId, { type: "context", context })).catch(() => {});
    void refreshCheckpoints(targetTabId);
  }, [activeTabId, bumpSessionLoadSeq, dispatchTo, refreshCheckpoints, sessionLoadCurrent, waitForBackendActiveTab, waitForTabReady]);

  const openChannelSession = useCallback(async (path: string, tabId: string) => {
    if (!tabId) return;
    await waitForTabReady(tabId);
    const seq = bumpSessionLoadSeq(tabId);
    dispatchTo(tabId, { type: "hydrate_start", reason: "resume-session" });
    let messages: HistoryMessage[];
    try {
      messages = asArray(await app.OpenChannelSessionForTab(tabId, path));
    } catch (err) {
      if (sessionLoadCurrent(tabId, seq)) {
        dispatchTo(tabId, { type: "hydrate_error", reason: "resume-session", error: errorMessage(err) });
        dispatchTo(tabId, { type: "local_notice", level: "warn", text: `${t("history.failedOpenSession")}: ${errorMessage(err)}` });
      }
      return;
    }
    if (!sessionLoadCurrent(tabId, seq)) return;
    dispatchTo(tabId, { type: "reset" });
    if (messages.length) dispatchTo(tabId, { type: "history", messages });
    dispatchTo(tabId, { type: "hydrate_done" });
    app.ContextUsageForTab(tabId).then((context) => dispatchTo(tabId, { type: "context", context })).catch(() => {});
    void refreshCheckpoints(tabId);
  }, [bumpSessionLoadSeq, dispatchTo, refreshCheckpoints, sessionLoadCurrent, waitForTabReady]);

  const previewSession = useCallback(async (path: string): Promise<HistoryMessage[]> => asArray<HistoryMessage>(await app.PreviewSession(path).catch(() => [])), []);
  const deleteSession = useCallback((path: string) => app.DeleteSession(path).finally(() => invalidateCache()), []);
  const restoreSession = useCallback((path: string) => app.RestoreSession(path).catch(() => {}).finally(() => invalidateCache()), []);
  const purgeTrashedSession = useCallback((path: string) => app.PurgeTrashedSession(path).catch(() => {}).finally(() => invalidateCache()), []);
  const renameSession = useCallback((path: string, title: string) => app.RenameSession(path, title).catch(() => {}).finally(() => invalidateCache()), []);

  const refreshMeta = useCallback(async () => {
    if (!activeTabId) return;
    try {
      dispatchTo(activeTabId, { type: "meta", meta: await app.MetaForTab(activeTabId) });
      dispatchTo(activeTabId, { type: "context", context: await app.ContextUsageForTab(activeTabId) });
      dispatchTo(activeTabId, { type: "effort", effort: await app.EffortForTab(activeTabId) });
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

  const compact = useCallback(() => {
    const tabId = activeTabIdRef.current;
    if (!tabId) return;
    void waitForBackendActiveTab(tabId).then((active) => {
      if (active) app.Compact().catch(() => {});
    });
  }, [waitForBackendActiveTab]);

  const setModel = useCallback(async (name: string) => {
    if (!activeTabId) return;
    try {
      await app.SetModelForTab(activeTabId, name);
    } catch (err) {
      dispatchTo(activeTabId, { type: "local_notice", level: "warn", text: t("status.modelSwitchFailed", { err: errorMessage(err) }) });
      return;
    }
    try {
      dispatchTo(activeTabId, { type: "meta", meta: await app.MetaForTab(activeTabId) });
      dispatchTo(activeTabId, { type: "context", context: await app.ContextUsageForTab(activeTabId) });
      dispatchTo(activeTabId, { type: "effort", effort: await app.EffortForTab(activeTabId) });
    } catch { /* ignore */ }
  }, [activeTabId, dispatchTo]);

  const setEffort = useCallback(async (level: string) => {
    if (!activeTabId) return;
    await app.SetEffortForTab(activeTabId, level).catch(() => {});
    try {
      dispatchTo(activeTabId, { type: "meta", meta: await app.MetaForTab(activeTabId) });
      dispatchTo(activeTabId, { type: "context", context: await app.ContextUsageForTab(activeTabId) });
      dispatchTo(activeTabId, { type: "effort", effort: await app.EffortForTab(activeTabId) });
    } catch { /* ignore */ }
  }, [activeTabId, dispatchTo]);

  const setTokenMode = useCallback(async (mode: TokenMode) => {
    if (!activeTabId) return;
    try {
      await app.SetTokenModeForTab(activeTabId, mode);
    } catch (err) {
      dispatchTo(activeTabId, { type: "local_notice", level: "warn", text: t("status.tokenModeSwitchFailed", { err: errorMessage(err) }) });
      return;
    }
    try {
      dispatchTo(activeTabId, { type: "meta", meta: await app.MetaForTab(activeTabId) });
      dispatchTo(activeTabId, { type: "context", context: await app.ContextUsageForTab(activeTabId) });
      dispatchTo(activeTabId, { type: "effort", effort: await app.EffortForTab(activeTabId) });
    } catch { /* ignore */ }
  }, [activeTabId, dispatchTo]);

  const fetchMemory = useCallback((): Promise<MemoryView> =>
    app.Memory().catch(() => ({ docs: [], facts: [], archives: [], scopes: [], storeDir: "", available: false })), []);
  const remember = useCallback(async (scope: string, note: string) => { await app.Remember(scope, note).catch(() => {}); }, []);
  const forget = useCallback(async (name: string) => { await app.Forget(name).catch(() => {}); }, []);
  const saveDoc = useCallback(async (path: string, body: string) => { await app.SaveDoc(path, body).catch(() => {}); }, []);

  const rewind = useCallback(async (turn: number, scope: string): Promise<boolean> => {
    const sourceTabId = activeTabId;
    if (!sourceTabId) return false;
    if (!(await waitForBackendActiveTab(sourceTabId))) return false;
    const actionScope = (["fork", "summ-from", "summ-upto", "conversation", "code", "both"].includes(scope) ? scope : "both") as MessageActionScope;
    dispatchTo(sourceTabId, { type: "message_action_start", action: { turn, scope: actionScope } });
    dispatchTo(sourceTabId, { type: "local_notice", level: "info", text: messageActionBusyText(actionScope) });
    try {
      if (actionScope === "fork") {
        const tab = await app.Fork(turn);
        if (tab?.id) {
          setActiveTabId(tab.id);
          activeTabIdRef.current = tab.id;
          confirmBackendActiveTab(tab.id);
          // The fork's controller builds in a background goroutine: an immediate
          // load reads empty history, and the ready-event fallback can still
          // target the source tab, leaving the fork blank (#3742).
          await waitForTabReady(tab.id);
          await loadSessionDataForTab(tab.id, true);
        } else {
          await syncActiveTabFromBackend(true);
        }
        return true;
      }

      if (actionScope === "summ-from") await app.SummarizeFrom(turn);
      else if (actionScope === "summ-upto") await app.SummarizeUpTo(turn);
      else await app.Rewind(turn, actionScope);

      const messages = asArray(await app.HistoryForTab(sourceTabId).catch(() => [] as HistoryMessage[]));
      dispatchTo(sourceTabId, { type: "reset" });
      if (messages.length) dispatchTo(sourceTabId, { type: "history", messages });
      dispatchTo(sourceTabId, { type: "context", context: await app.ContextUsageForTab(sourceTabId) });
      dispatchTo(sourceTabId, { type: "checkpoints", checkpoints: asArray(await app.CheckpointsForTab(sourceTabId)) });
      return true;
    } catch {
      /* The controller emits a warning notice with the specific failure reason. */
      return false;
    } finally {
      dispatchTo(sourceTabId, { type: "message_action_done" });
    }
  }, [activeTabId, confirmBackendActiveTab, dispatchTo, loadSessionDataForTab, syncActiveTabFromBackend, waitForBackendActiveTab, waitForTabReady]);

  // Tab management: switch preserves per-tab state; open creates it.
  const switchTab = useCallback(async (tabId: string, optimisticTab?: TabMeta): Promise<TabMeta[] | undefined> => {
    const startedAt = Date.now();
    addBreadcrumb("tab.switch", `click ${tabId}`);
    setActiveTabId(tabId);
    activeTabIdRef.current = tabId;
    dispatchTo(tabId, { type: "backend_activation_start" });
    if (optimisticTab) {
      dispatchTo(tabId, { type: "optimistic_meta", meta: metaFromTab(optimisticTab, statesRef.current.get(tabId)?.meta) });
    }
    dispatchTo(tabId, { type: "hydrate_start", reason: "switch-tab" });
    addBreadcrumb("tab.switch", `active-rendered ${tabId} ms=${Date.now() - startedAt}`);
    const backendActivation = app.SetActiveTab(tabId)
      .then(() => {
        confirmBackendActiveTab(tabId);
        addBreadcrumb("tab.switch", `set-active-done ${tabId} ms=${Date.now() - startedAt}`);
        return true;
      })
      .catch((err) => {
        dispatchTo(tabId, { type: "backend_activation_done" });
        dispatchTo(tabId, { type: "hydrate_error", reason: "switch-tab", error: errorMessage(err) });
        return false;
      });
    trackBackendActivation(tabId, backendActivation);
    const backendSwitch = backendActivation
      .then(async (activated) => {
        if (!activated) return undefined;
        const tabs = await reconcileTabRuntime(tabId, { hydrateSessionData: false });
        void loadSessionDataForTab(tabId, false, "switch-tab", {
          skipHistory: hasCachedLiveTurn(statesRef.current.get(tabId)),
        });
        return tabs;
      })
      .catch((err) => {
        dispatchTo(tabId, { type: "hydrate_error", reason: "switch-tab", error: errorMessage(err) });
        return undefined;
      });
    return backendSwitch;
  }, [confirmBackendActiveTab, dispatchTo, loadSessionDataForTab, reconcileTabRuntime, trackBackendActivation]);

  const openProjectTab = useCallback(async (workspaceRoot: string, topicId: string): Promise<TabMeta> => {
    const meta = await app.OpenProjectTab(workspaceRoot, topicId);
    setActiveTabId(meta.id);
    activeTabIdRef.current = meta.id;
    confirmBackendActiveTab(meta.id);
    dispatchTo(meta.id, { type: "optimistic_meta", meta: metaFromTab(meta, statesRef.current.get(meta.id)?.meta) });
    void loadSessionDataForTab(meta.id, !statesRef.current.has(meta.id), "open-topic");
    return meta;
  }, [confirmBackendActiveTab, dispatchTo, loadSessionDataForTab]);

  const openGlobalTab = useCallback(async (topicId: string): Promise<TabMeta> => {
    const meta = await app.OpenGlobalTab(topicId);
    setActiveTabId(meta.id);
    activeTabIdRef.current = meta.id;
    confirmBackendActiveTab(meta.id);
    dispatchTo(meta.id, { type: "optimistic_meta", meta: metaFromTab(meta, statesRef.current.get(meta.id)?.meta) });
    void loadSessionDataForTab(meta.id, !statesRef.current.has(meta.id), "open-topic");
    return meta;
  }, [confirmBackendActiveTab, dispatchTo, loadSessionDataForTab]);

  const openTopicSession = useCallback(async (scope: string, workspaceRoot: string, topicId: string, sessionPath: string): Promise<TabMeta> => {
    const meta = await app.OpenTopicSession(scope, workspaceRoot, topicId, sessionPath);
    setActiveTabId(meta.id);
    activeTabIdRef.current = meta.id;
    confirmBackendActiveTab(meta.id);
    dispatchTo(meta.id, { type: "optimistic_meta", meta: metaFromTab(meta, statesRef.current.get(meta.id)?.meta) });
    void loadSessionDataForTab(meta.id, !statesRef.current.has(meta.id), "open-topic");
    return meta;
  }, [confirmBackendActiveTab, dispatchTo, loadSessionDataForTab]);

  const activateTopic = useCallback(async (scope: string, workspaceRoot: string, topicId: string, sessionPath = ""): Promise<TabMeta> => {
    const meta = await app.ActivateTopic(scope, workspaceRoot, topicId, sessionPath);
    for (const id of Array.from(statesRef.current.keys())) {
      if (id !== meta.id) statesRef.current.delete(id);
    }
    setActiveTabId(meta.id);
    activeTabIdRef.current = meta.id;
    confirmBackendActiveTab(meta.id);
    dispatchTo(meta.id, { type: "optimistic_meta", meta: metaFromTab(meta, statesRef.current.get(meta.id)?.meta) });
    void loadSessionDataForTab(meta.id, true, "open-topic");
    return meta;
  }, [confirmBackendActiveTab, dispatchTo, loadSessionDataForTab]);

  // Ensure a blank tab exists for the given scope — reuses an existing one
  // or creates a new tab, then loads its session data.
  const ensureBlankTab = useCallback(async (scope: string, workspaceRoot: string): Promise<TabMeta> => {
    const meta = await app.EnsureBlankTab(scope, workspaceRoot);
    setActiveTabId(meta.id);
    activeTabIdRef.current = meta.id;
    confirmBackendActiveTab(meta.id);
    dispatchTo(meta.id, { type: "optimistic_meta", meta: metaFromTab(meta, statesRef.current.get(meta.id)?.meta) });
    void loadSessionDataForTab(meta.id, !statesRef.current.has(meta.id), "open-topic");
    return meta;
  }, [confirmBackendActiveTab, dispatchTo, loadSessionDataForTab]);

  const ensureBlankSurface = useCallback(async (scope: string, workspaceRoot: string): Promise<TabMeta> => {
    const meta = await app.EnsureBlankSurface(scope, workspaceRoot);
    for (const id of Array.from(statesRef.current.keys())) {
      if (id !== meta.id) statesRef.current.delete(id);
    }
    setActiveTabId(meta.id);
    activeTabIdRef.current = meta.id;
    confirmBackendActiveTab(meta.id);
    dispatchTo(meta.id, { type: "optimistic_meta", meta: metaFromTab(meta, statesRef.current.get(meta.id)?.meta) });
    void loadSessionDataForTab(meta.id, true, "open-topic");
    return meta;
  }, [confirmBackendActiveTab, dispatchTo, loadSessionDataForTab]);

  const closeTab = useCallback(async (tabId: string) => {
    try {
      await app.CloseTab(tabId);
      statesRef.current.delete(tabId);
      bump();
      if (tabId === activeTabId) await syncActiveTabFromBackend(true);
    } catch { /* ignore */ }
  }, [activeTabId, bump, syncActiveTabFromBackend]);

  const reorderTabs = useCallback(async (tabIds: string[]) => {
    try {
      await app.ReorderTabs(tabIds);
    } catch { /* ignore */ }
  }, []);

  return {
    state: activeState,
    activeTabId,
    send, sendToTab, runShell, steer, notice, cancel, approve, answerQuestion, setControllerMode, setCollaborationMode, setToolApprovalMode, setGoal, clearGoal,
    newSession, clearSession, listSessions, listTrashedSessions, resumeSession, openChannelSession, previewSession, deleteSession, restoreSession, purgeTrashedSession, renameSession,
    refreshMeta, pickWorkspace, switchWorkspace, compact, rewind, setModel, setEffort, setTokenMode,
    fetchMemory, remember, forget, saveDoc,
    switchTab, openProjectTab, openGlobalTab, openTopicSession, ensureBlankTab, activateTopic, ensureBlankSurface, closeTab, reorderTabs,
    syncActiveTab: syncActiveTabFromBackend,
  };
}
