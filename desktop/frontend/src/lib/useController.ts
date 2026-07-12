// useController is the frontend's state machine over the agent's event stream. It
// maintains per-tab state so background tabs preserve their streaming output, tool
// states, and approvals when the user switches away and back. The active tab's state
// is what components render.

import { useCallback, useEffect, useRef, useState } from "react";
import { asArray } from "./array";
import { addBreadcrumb } from "./breadcrumbs";
import { app, onEvent, onReady } from "./bridge";
import { invalidateCache } from "./composerHistory";
import { formatGuardianAssessmentNotice } from "./guardianEvents";
import { createRafBatch } from "./rafBatch";
import { t, type DictKey } from "./i18n";
import { fileDiffFromWire, summarize, summarizeFileDiff, type ToolFileDiff } from "./tools";
import { modeHasAutoApproveTools, normalizeMode, normalizeToolApprovalMode } from "./types";
import type {
  BalanceInfo,
  CheckpointMeta,
  CollaborationMode,
  ContextInfo,
  EffortInfo,
  HistoryMessage,
  HistoryPage,
  JobView,
  MemoryCitation,
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

export type LiveStream = {
  id: string;
  text: string;
  reasoning: string;
  reasoningComplete: boolean;
  reasoningStartedAt?: number;
  reasoningCompletedAt?: number;
};
export type MessageActionScope = "fork" | "summ-from" | "summ-upto" | "conversation" | "code" | "both";
export type MessageActionState = { turn: number; scope: MessageActionScope };
export type HydrateReason = "switch-tab" | "new-session" | "resume-session" | "open-topic" | "startup";
type SyncActiveTabOptions = {
  preserveCachedHistory?: boolean;
};

const HISTORY_PAGE_TURNS = 60;

export type Item =
  | { kind: "user"; id: string; text: string; submitText?: string; failed?: boolean; createdAt?: number; checkpointTurn?: number }
  | { kind: "assistant"; id: string; text: string; reasoning: string; streaming: boolean; reasoningComplete?: boolean; reasoningDurationMs?: number; workDurationMs?: number; memoryCitations?: MemoryCitation[] }
  | { kind: "phase"; id: string; text: string }
  | { kind: "notice"; id: string; level: "info" | "warn"; text: string; detail?: string }
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
  hydrateHistoryLoaded?: boolean;
  hydratePlaceholderItems?: Item[];
  historyStartTurn: number;
  historyTotalTurns: number;
  historyHasOlder: boolean;
  historyOlderLoading: boolean;
  backendActivationPending: boolean;
  messageAction?: MessageActionState;
  currentAssistant?: string;
  live?: LiveStream;
  pendingUser?: string;
  discardTurn?: boolean;
  turnStartAt: number;
  // Time spent waiting on the user (approval/ask) within the current turn.
  // Closed intervals accumulate here; an open interval uses promptWaitStartedAt
  // so background tabs keep counting while not rendered by Composer.
  turnWaitAccumMs: number;
  promptWaitStartedAt?: number;
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
  historyStartTurn: 0,
  historyTotalTurns: 0,
  historyHasOlder: false,
  historyOlderLoading: false,
  backendActivationPending: false,
  turnStartAt: 0,
  turnWaitAccumMs: 0,
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
  cancelRequested?: boolean;
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

export function metaFromTab(tab: TabMeta, existing?: Meta): Meta {
  const cwd = tab.cwd || tab.workspaceRoot || existing?.cwd || "";
  const toolApprovalMode = normalizeToolApprovalMode(
    tab.toolApprovalMode,
    normalizeMode(tab.mode),
    modeHasAutoApproveTools(tab.mode),
    (tab.toolApprovalMode ?? "").trim() === "" ? existing?.toolApprovalMode : undefined,
  );
  const autoApproveTools = toolApprovalMode === "yolo";
  return {
    label: tab.label || existing?.label || "",
    ready: tab.ready,
    startupErr: tab.startupErr,
    eventChannel: existing?.eventChannel ?? "agent:event",
    cwd,
    workspaceRoot: tab.workspaceRoot || existing?.workspaceRoot || cwd,
    workspaceName: tab.workspaceName || existing?.workspaceName,
    workspacePath: tab.workspacePath || tab.workspaceRoot || existing?.workspacePath,
    sessionPath: tab.sessionPath !== undefined ? tab.sessionPath : existing?.sessionPath,
    gitBranch: tab.gitBranch || existing?.gitBranch,
    autoApproveTools,
    bypass: autoApproveTools,
    collaborationMode: tab.collaborationMode ?? existing?.collaborationMode ?? "normal",
    toolApprovalMode,
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
    a.sessionPath === b.sessionPath &&
    a.gitBranch === b.gitBranch &&
    a.imageInputEnabled === b.imageInputEnabled &&
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
const CANCEL_RECONCILE_DELAYS_MS = [0, 100, 300, 1_000] as const;
const STARTUP_READY_META_RECONCILE_MS = 250;
const STARTUP_READY_META_RECONCILE_ATTEMPTS = 60;

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

function hasReusableCachedTranscript(state: State | undefined, sessionPath?: string): boolean {
  if (!state || state.items.length === 0) return false;
  const expectedSessionPath = (sessionPath ?? "").trim();
  if (!expectedSessionPath) return true;
  return (state.meta?.sessionPath ?? "").trim() === expectedSessionPath;
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
  | { type: "hydrate_start"; reason: HydrateReason; placeholderItems?: Item[] }
  | { type: "hydrate_done" }
  | { type: "hydrate_error"; reason: HydrateReason; error: string }
  | { type: "backend_activation_start" }
  | { type: "backend_activation_done" }
  | { type: "message_action_start"; action: MessageActionState }
  | { type: "message_action_done" }
  | { type: "history"; messages: HistoryMessage[] }
  | { type: "history_page"; page: HistoryPage; mode: "replace" | "prepend" }
  | { type: "history_older_start" }
  | { type: "history_older_error" }
  | { type: "history_checkpoint_turns"; turns: number[] }
  | { type: "local_notice"; level: "info" | "warn"; text: string }
  | { type: "clearApproval" }
  | { type: "clearAsk" }
  | { type: "reset" };

function backendStatusFromRuntimeMeta(meta: RuntimeMetaSnapshot): Extract<Action, { type: "backend_status" }> {
  const foregroundRunning = foregroundRunningFromRuntimeMeta(meta);
  return {
    type: "backend_status",
    running: foregroundRunning,
    pendingPrompt: Boolean(meta.pendingPrompt),
    backgroundJobs: meta.backgroundJobs ?? 0,
    cancelRequested: Boolean(meta.cancelRequested),
    cancellable: foregroundRunning,
  };
}

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

  let items: Item[] = [];
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
        const next = appendNoticeItem(items, seq, `${idPrefix}${seq}`, m.level === "warn" ? "warn" : "info", m.content, m.detail);
        items = next.items;
        seq = next.seq;
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
      items.push({ kind: "user", id: `${idPrefix}${seq}`, text: m.content, submitText: m.submitText, createdAt: m.createdAt, checkpointTurn: m.checkpointTurn });
      seq++;
      continue;
    }
    if (m.role === "assistant") {
      const hasText = m.content.trim() !== "" || (m.reasoning ?? "").trim() !== "";
      if (hasText) {
        const memoryCitations = asArray<MemoryCitation>(m.memoryCitations);
        items.push({
          kind: "assistant",
          id: `${idPrefix}${seq}`,
          text: m.content,
          reasoning: m.reasoning ?? "",
          streaming: false,
          workDurationMs: m.workDurationMs,
          memoryCitations: memoryCitations.length > 0 ? memoryCitations : undefined,
        });
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

function mergeHistoryCheckpointTurns(items: Item[], turns: number[], startTurn = 0): Item[] {
  if (!turns.some((turn) => turn >= 0)) return items;
  const offset = Math.max(0, Math.floor(startTurn));
  let userIndex = 0;
  let changed = false;
  const next = items.map((item) => {
    if (item.kind !== "user") return item;
    const turn = turns[offset + userIndex];
    userIndex += 1;
    if (turn == null || turn < 0 || item.checkpointTurn === turn) return item;
    changed = true;
    return { ...item, checkpointTurn: turn };
  });
  return changed ? next : items;
}

function historyPageItems(page: HistoryPage): { items: Item[]; seq: number } {
  return historyMessagesToItems(asArray(page.messages), `h${page.startTurn}-`, 0);
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

function liveReasoningDurationMs(live?: LiveStream): number | undefined {
  if (!live?.reasoningStartedAt || !live.reasoning) return undefined;
  const completedAt = live.reasoningCompletedAt;
  if (!completedAt || completedAt < live.reasoningStartedAt) return undefined;
  return completedAt - live.reasoningStartedAt;
}

function completeLiveReasoning(live: LiveStream, now = Date.now()): LiveStream {
  if (!live.reasoning || live.reasoningCompletedAt) {
    return { ...live, reasoningComplete: live.reasoning !== "" || live.reasoningComplete };
  }
  return {
    ...live,
    reasoningComplete: true,
    reasoningCompletedAt: now,
  };
}

/** Closed + open user-wait ms for the active turn (approval/ask). */
export function currentTurnWaitMs(
  s: Pick<State, "turnWaitAccumMs" | "promptWaitStartedAt">,
  now = Date.now(),
): number {
  const closed = Math.max(0, s.turnWaitAccumMs || 0);
  const open = s.promptWaitStartedAt && s.promptWaitStartedAt > 0
    ? Math.max(0, now - s.promptWaitStartedAt)
    : 0;
  return closed + open;
}

function currentTurnDurationMs(
  s: Pick<State, "turnStartAt" | "turnWaitAccumMs" | "promptWaitStartedAt">,
  now = Date.now(),
): number | undefined {
  if (!Number.isFinite(s.turnStartAt) || s.turnStartAt <= 0 || now < s.turnStartAt) return undefined;
  return Math.max(1, now - s.turnStartAt - currentTurnWaitMs(s, now));
}

function beginPromptWait(s: State, now = Date.now()): State {
  if (s.promptWaitStartedAt && s.promptWaitStartedAt > 0) return s;
  return { ...s, promptWaitStartedAt: now };
}

function endPromptWait(s: State, now = Date.now()): State {
  if (!s.promptWaitStartedAt || s.promptWaitStartedAt <= 0) {
    return s.promptWaitStartedAt === undefined ? s : { ...s, promptWaitStartedAt: undefined };
  }
  const delta = Math.max(0, now - s.promptWaitStartedAt);
  return {
    ...s,
    turnWaitAccumMs: Math.max(0, s.turnWaitAccumMs || 0) + delta,
    promptWaitStartedAt: undefined,
  };
}

function endPromptWaitIfIdle(s: State, now = Date.now()): State {
  if (s.approval || s.ask) return s;
  return endPromptWait(s, now);
}

function resetTurnTiming(now = Date.now()): Pick<State, "turnStartAt" | "turnWaitAccumMs" | "promptWaitStartedAt" | "turnTokens" | "turnTotalTokens" | "turnCost"> {
  return {
    turnStartAt: now,
    turnWaitAccumMs: 0,
    promptWaitStartedAt: undefined,
    turnTokens: 0,
    turnTotalTokens: 0,
    turnCost: 0,
  };
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
  if (e.kind === "memory_compiler_stats" || e.kind === "mcp_surface_ready") {
    // Background-only events must not confirm an optimistic user bubble.
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
      return {
        ...cur,
        items,
        currentAssistant: id,
        seq,
        live: { id, text: "", reasoning: "", reasoningComplete: false },
        running: true,
        turnActive: true,
        pendingPrompt: false,
        cancelRequested: false,
        cancellable: true,
        ...resetTurnTiming(),
      };
    }
    case "text":
    case "reasoning": {
      const { items, id, seq } = ensureAssistant(s);
      const delta = e.text ?? e.reasoning ?? "";
      const base = s.live?.id === id ? s.live : { id, text: "", reasoning: "", reasoningComplete: false };
      const now = Date.now();
      const live =
        e.kind === "text"
          ? { ...completeLiveReasoning(base, now), text: base.text + delta }
          : {
              ...base,
              reasoning: base.reasoning + delta,
              reasoningComplete: false,
              reasoningStartedAt: base.reasoningStartedAt ?? (delta ? now : undefined),
              reasoningCompletedAt: undefined,
            };
      return { ...s, items, live, currentAssistant: id, seq };
    }
    case "message": {
      const existingAssistant =
        s.currentAssistant === undefined
          ? undefined
          : s.items.find((it): it is Extract<Item, { kind: "assistant" }> => it.kind === "assistant" && it.id === s.currentAssistant);
      const text = e.text ?? s.live?.text ?? existingAssistant?.text ?? "";
      const reasoning = e.reasoning ?? s.live?.reasoning ?? existingAssistant?.reasoning ?? "";
      if (text.trim() === "" && reasoning.trim() === "") {
        const items =
          existingAssistant && existingAssistant.text.trim() === "" && existingAssistant.reasoning.trim() === "" && !existingAssistant.memoryCitations?.length
            ? s.items.filter((it) => !(it.kind === "assistant" && it.id === existingAssistant.id))
            : s.items;
        return { ...s, items, live: undefined, currentAssistant: undefined };
      }
      const { items, id, seq } = ensureAssistant(s);
      const now = Date.now();
      const completedLive = s.live?.id === id ? completeLiveReasoning({ ...s.live, text, reasoning }, now) : undefined;
      const reasoningDurationMs = liveReasoningDurationMs(completedLive);
      const workDurationMs = currentTurnDurationMs(s, now);
      const next = items.map((it) =>
        it.kind === "assistant" && it.id === id
          ? (() => {
              const memoryCitations = asArray<MemoryCitation>(e.memoryCitations ?? it.memoryCitations);
              return {
                ...it,
                text,
                reasoning,
                streaming: false,
                reasoningComplete: reasoning !== "" || it.reasoningComplete,
                reasoningDurationMs: reasoningDurationMs ?? it.reasoningDurationMs,
                workDurationMs: Math.max(it.workDurationMs ?? 0, workDurationMs ?? 0) || undefined,
                memoryCitations: memoryCitations.length > 0 ? memoryCitations : undefined,
              };
            })()
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
      const usage = updateContextGauge ? e.usage : s.usage;
      return { ...s, usage, context: { ...s.context, used, sessionTokens }, turnTokens, turnTotalTokens, turnCost, sessionTokens, sessionCost, sessionCurrency };
    }
    case "notice":
      return appendNoticeToState(s, e.level ?? "info", e.text ?? "", e.detail);
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
      return beginPromptWait({
        ...s,
        approval: e.approval,
        pendingPrompt: true,
        running: true,
        turnActive: true,
        cancellable: true,
      });
    }
    case "ask_request": {
      if (s.cancelRequested) return s;
      return beginPromptWait({
        ...s,
        ask: e.ask,
        pendingPrompt: true,
        running: true,
        turnActive: true,
        cancellable: true,
      });
    }
    case "guardian_assessment": {
      if (!e.guardian) return s;
      const level = e.guardian.outcome === "deny" ? "warn" : "info";
      return { ...s, seq: s.seq + 1, items: [...s.items, { kind: "notice", id: `g${s.seq}`, level, text: formatGuardianAssessmentNotice(e.guardian) }] };
    }
    case "turn_done": {
      if (s.pendingUser !== undefined) s = flushPendingUser(s);
      const now = Date.now();
      const workDurationMs = currentTurnDurationMs(s, now);
      let lastUserIndex = -1;
      let lastAssistantIndex = -1;
      for (let i = 0; i < s.items.length; i++) {
        if (s.items[i].kind === "user") {
          lastUserIndex = i;
          lastAssistantIndex = -1;
        } else if (i > lastUserIndex && s.items[i].kind === "assistant") {
          lastAssistantIndex = i;
        }
      }
      const finalized = s.items.map((it, index) => {
        if (it.kind === "assistant" && s.live && it.id === s.live.id) {
          const completedLive = completeLiveReasoning(s.live, now);
          return {
            ...it,
            text: completedLive.text,
            reasoning: completedLive.reasoning,
            streaming: false,
            reasoningComplete: completedLive.reasoning !== "" || completedLive.reasoningComplete,
            reasoningDurationMs: liveReasoningDurationMs(completedLive) ?? it.reasoningDurationMs,
            workDurationMs: index === lastAssistantIndex
              ? Math.max(it.workDurationMs ?? 0, workDurationMs ?? 0) || undefined
              : it.workDurationMs,
          };
        }
        if (it.kind === "assistant") {
          return {
            ...it,
            streaming: false,
            workDurationMs: index === lastAssistantIndex
              ? Math.max(it.workDurationMs ?? 0, workDurationMs ?? 0) || undefined
              : it.workDurationMs,
          };
        }
        if (it.kind === "tool" && it.status === "running") return { ...it, status: "stopped" as const };
        return it;
      });
      let items: Item[] = e.err ? [...finalized, { kind: "notice", id: `e${s.seq}`, level: "warn", text: e.err }] : finalized;
      // Plan approval can arrive before turn_done on some Wails event paths.
      // Keep that gate visible instead of clearing the only UI that can answer it.
      const keepPlanApproval = s.approval?.tool === "exit_plan_mode";
      let next: State = {
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
      // Close user-wait unless the plan approval gate remains open.
      if (!keepPlanApproval) next = endPromptWait(next, now);
      return next;
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
        ...resetTurnTiming(),
        pendingUser: a.text,
        discardTurn: false,
      };
    }
    case "unsend": {
      const cleared = endPromptWait({
        ...s,
        pendingUser: undefined,
        discardTurn: true,
        running: false,
        pendingPrompt: false,
        cancelRequested: true,
        cancellable: false,
        approval: undefined,
        ask: undefined,
        live: undefined,
      });
      return cleared;
    }
    case "cancel_requested": {
      return endPromptWait({
        ...s,
        pendingPrompt: false,
        cancelRequested: true,
        approval: undefined,
        ask: undefined,
        cancellable: s.running || s.turnActive,
      });
    }
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
      return endPromptWait({
        ...s,
        items: finalized,
        running: false,
        turnActive: false,
        pendingPrompt,
        backgroundJobs,
        cancelRequested,
        cancellable,
        live: undefined,
        currentAssistant: undefined,
        approval: undefined,
        ask: undefined,
      });
    }
    case "meta": {
      const meta = a.meta.sessionPath === undefined && s.meta?.sessionPath !== undefined ? { ...a.meta, sessionPath: s.meta.sessionPath } : a.meta;
      return sameMeta(s.meta, meta) ? s : { ...s, meta };
    }
    case "optimistic_meta": return sameMeta(s.meta, a.meta) ? s : { ...s, meta: a.meta, hydrateError: undefined };
    case "context": {
      const sessionTokens = typeof a.context.sessionTokens === "number"
        ? Math.max(0, a.context.sessionTokens)
        : s.sessionTokens;
      const sessionCost = typeof a.context.sessionCost === "number" && a.context.sessionCost > 0
        ? a.context.sessionCost
        : s.sessionCost;
      const sessionCurrency = a.context.sessionCurrency || s.sessionCurrency;
      return { ...s, context: a.context, sessionTokens, sessionCost, sessionCurrency };
    }
    case "balance": return { ...s, balance: a.balance };
    case "effort": return { ...s, effort: a.effort };
    case "jobs": return { ...s, jobs: a.jobs };
    case "checkpoints": return { ...s, checkpoints: a.checkpoints };
    case "hydrate_start": return {
      ...s,
      hydrating: true,
      hydrateReason: a.reason,
      hydrateError: undefined,
      hydrateHistoryLoaded: false,
      hydratePlaceholderItems: a.placeholderItems?.length ? a.placeholderItems : undefined,
    };
    case "hydrate_done": return s.hydrating || s.hydrateReason || s.hydrateError || s.hydrateHistoryLoaded || s.hydratePlaceholderItems
      ? { ...s, hydrating: false, hydrateReason: undefined, hydrateError: undefined, hydrateHistoryLoaded: undefined, hydratePlaceholderItems: undefined }
      : s;
    case "hydrate_error": return { ...s, hydrating: false, hydrateReason: a.reason, hydrateError: a.error, hydrateHistoryLoaded: undefined, hydratePlaceholderItems: undefined };
    case "backend_activation_start": return {
      ...s,
      // The target tab may contain a prompt event that was routed there while
      // frontend selection was ahead of backend activation. Reset that
      // uncertain lifecycle first; optimistic backend metadata is applied
      // immediately afterwards and restores a genuinely running target.
      backendActivationPending: true,
      pendingPrompt: false,
      approval: undefined,
      ask: undefined,
      running: false,
      turnActive: false,
      cancellable: false,
    };
    case "backend_activation_done": return s.backendActivationPending ? { ...s, backendActivationPending: false } : s;
    case "message_action_start": return { ...s, messageAction: a.action };
    case "message_action_done": return { ...s, messageAction: undefined };
    case "history": {
      const { items, seq } = historyMessagesToItems(a.messages, "h", s.seq);
      return { ...s, items: compactArchivedToolItems(items), seq, hydrateHistoryLoaded: true, hydratePlaceholderItems: undefined, historyStartTurn: 0, historyTotalTurns: 0, historyHasOlder: false, historyOlderLoading: false };
    }
    case "history_page": {
      const { items, seq } = historyPageItems(a.page);
      const nextItems = a.mode === "prepend" ? [...items, ...s.items] : items;
      return {
        ...s,
        items: compactArchivedToolItems(nextItems),
        seq: Math.max(s.seq, seq),
        hydrateHistoryLoaded: true,
        hydratePlaceholderItems: undefined,
        historyStartTurn: a.page.startTurn,
        historyTotalTurns: a.page.totalTurns,
        historyHasOlder: a.page.hasOlder,
        historyOlderLoading: false,
      };
    }
    case "history_older_start": return s.historyOlderLoading ? s : { ...s, historyOlderLoading: true };
    case "history_older_error": return s.historyOlderLoading ? { ...s, historyOlderLoading: false } : s;
    case "history_checkpoint_turns":
      return { ...s, items: mergeHistoryCheckpointTurns(s.items, a.turns, s.historyStartTurn) };
    case "local_notice": return { ...s, running: false, turnActive: false, seq: s.seq + 1, items: [...s.items, { kind: "notice", id: `n${s.seq}`, level: a.level, text: a.text }] };
    case "clearApproval": {
      const next = { ...s, approval: undefined, pendingPrompt: Boolean(s.ask) };
      return endPromptWaitIfIdle(next);
    }
    case "clearAsk": {
      const next = { ...s, ask: undefined, pendingPrompt: Boolean(s.approval) };
      return endPromptWaitIfIdle(next);
    }
    case "reset": return { ...initialState, meta: s.meta, context: { used: 0, window: s.context.window, sessionTokens: 0, compactRatio: s.context.compactRatio }, balance: s.balance, effort: s.effort, jobs: s.jobs, hydrating: s.hydrating, hydrateReason: s.hydrateReason, hydrateError: s.hydrateError, hydrateHistoryLoaded: s.hydrateHistoryLoaded, hydratePlaceholderItems: s.hydratePlaceholderItems, backendActivationPending: s.backendActivationPending, sessionGen: s.sessionGen + 1 };
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

export function effortSwitchNoticeText(err: unknown): string {
  return settingSwitchNoticeText(err, "effort", {
    busy: "status.effortSwitchBusy",
    leaseHeld: "status.effortSwitchLeaseHeld",
    starting: "status.effortSwitchStarting",
    startupFailed: "status.effortSwitchStartupFailed",
    retry: "status.effortSwitchRetry",
    failed: "status.effortSwitchFailed",
  });
}

export function modelSwitchNoticeText(err: unknown): string {
  const msg = errorMessage(err).trim() || "unknown error";
  const unknownModel = /^unknown model (.+)$/i.exec(msg);
  if (unknownModel) {
    return t("status.modelSwitchUnknown", { model: unknownModel[1] });
  }
  const unavailable = /^model (.+) is not available because provider (.+) is not added$/i.exec(msg);
  if (unavailable) {
    return t("status.modelSwitchProviderUnavailable", { model: unavailable[1], provider: unavailable[2] });
  }
  return settingSwitchNoticeText(msg, "model", {
    busy: "status.modelSwitchBusy",
    leaseHeld: "status.modelSwitchLeaseHeld",
    starting: "status.modelSwitchStarting",
    startupFailed: "status.modelSwitchStartupFailed",
    retry: "status.modelSwitchRetry",
    failed: "status.modelSwitchFailed",
  });
}

export function tokenModeSwitchNoticeText(err: unknown): string {
  return settingSwitchNoticeText(err, "token mode", {
    busy: "status.tokenModeSwitchBusy",
    leaseHeld: "status.tokenModeSwitchLeaseHeld",
    starting: "status.tokenModeSwitchStarting",
    startupFailed: "status.tokenModeSwitchStartupFailed",
    retry: "status.tokenModeSwitchRetry",
    failed: "status.tokenModeSwitchFailed",
  });
}

export function localizedBackendNoticeText(text: string): string {
  const msg = text.trim();
  const autosave = /^Session autosave failed: (.+)$/s.exec(msg);
  if (autosave) {
    return t("status.sessionAutosaveFailed", { err: autosave[1] });
  }
  const saveBefore = /^Session save failed before (.+?): (.+)$/s.exec(msg);
  if (saveBefore) {
    return t("status.sessionSaveFailedBefore", { action: localizedSessionAction(saveBefore[1]), err: saveBefore[2] });
  }
  const modelFallback = /^model (.+) is no longer available; switched to (.+)$/s.exec(msg);
  if (modelFallback) {
    return t("status.modelFallbackSwitched", { model: modelFallback[1], fallback: modelFallback[2] });
  }
  const backgroundJob = /^background (.+) failed: needs attention$/s.exec(msg);
  if (backgroundJob) {
    return t("notice.backgroundJobFailed", { kind: backgroundJob[1] });
  }
  const canonicalNoticeKey = backendNoticeKey(msg);
  if (canonicalNoticeKey) {
    return t(canonicalNoticeKey);
  }
  if (
    /^session changed on disk; unsaved local transcript was saved as a conflict copy$/i.test(msg) ||
    /^session changed on disk; unsaved local transcript was saved as recovery branch\b/i.test(msg)
  ) {
    return t("recovery.noticeSavedCopy");
  }
  if (
    /^repeated save conflicts were detected; saved the current conflict copy in place$/i.test(msg) ||
    /^session conflicts kept recurring; kept the transcript on the current recovery branch$/i.test(msg)
  ) {
    return t("recovery.noticeKeptCurrent");
  }
  if (/^session changed on disk; adopted the newer transcript \(local changes already covered\)$/i.test(msg)) {
    return t("recovery.noticeAdoptedCovered");
  }
  if (/^session changed on disk; adopted the newer transcript$/i.test(msg)) {
    return t("recovery.noticeAdopted");
  }
  return msg;
}

function backendNoticeKey(msg: string): DictKey | "" {
  switch (msg) {
    case "Task status needs one more check; asking the assistant to finish or explain what is blocking it.":
      return "notice.finalReadiness";
    case "No visible answer was produced; asking the assistant to respond again.":
      return "notice.emptyFinal";
    case "The assistant answered before taking action; asking it to use the required tools.":
      return "notice.executorHandoff";
    case "Tool round limit reached; asking the assistant to summarize progress.":
      return "notice.toolBudget";
    case "The assistant is stuck retrying a blocked action; asking it to change approach.":
      return "notice.loopGuard";
    case "Context is getting large; preserving cache until cleanup is needed.":
      return "notice.contextLarge";
    case "Context cleanup skipped for now.":
      return "notice.contextCleanupSkipped";
    case "Automatic context cleanup paused because the context window is too small.":
      return "notice.contextCleanupPaused";
    case "Context was compacted without a generated summary.":
      return "notice.compactionNoSummary";
    case "Planning mode enabled for this multi-step task.":
      return "notice.autoPlanEnabled";
    case "Plan detection requested a plan.":
      return "notice.autoPlanRequested";
    case "Plan detection was uncertain; using the fallback planner heuristic.":
      return "notice.autoPlanFallback";
    case "Goal is not ready to complete yet; continuing the remaining work.":
      return "notice.goalNotReady";
    case "Goal still has unfinished task state; continuing the remaining work.":
      return "notice.goalUnfinished";
    case "AutoResearch status update failed.":
      return "notice.autoresearchStatusFailed";
    case "AutoResearch task marked blocked.":
      return "notice.autoresearchBlocked";
    case "Job artifact migration failed.":
      return "notice.jobArtifactMigrationFailed";
    case "Background job teardown timed out.":
      return "notice.jobTeardownTimeout";
    case "Some plan-mode tool settings were ignored.":
      return "notice.planModeToolSettingsIgnored";
    case "Some plan-mode command settings were ignored.":
      return "notice.planModeCommandSettingsIgnored";
    case "Config migration did not complete.":
      return "notice.configMigrationIncomplete";
    case "Selected model is missing its API key.":
      return "notice.modelMissingApiKey";
    case "An MCP server failed to start.":
      return "notice.mcpServerFailed";
    case "Some MCP servers failed to start; run /mcp for details.":
      return "notice.mcpServersFailed";
    case "Guardian was disabled because its model was not found.":
      return "notice.guardianModelMissing";
    case "Guardian was disabled because it could not start.":
      return "notice.guardianStartFailed";
    default:
      return "";
  }
}

function recoveryNoticeDedupeKey(text: string): string {
  const msg = text.trim();
  if (
    /^session changed on disk; unsaved local transcript was saved as a conflict copy$/i.test(msg) ||
    /^session changed on disk; unsaved local transcript was saved as recovery branch\b/i.test(msg) ||
    msg === t("recovery.noticeSavedCopy")
  ) {
    return "recovery:saved-copy";
  }
  if (
    /^repeated save conflicts were detected; saved the current conflict copy in place$/i.test(msg) ||
    /^session conflicts kept recurring; kept the transcript on the current recovery branch$/i.test(msg) ||
    msg === t("recovery.noticeKeptCurrent")
  ) {
    return "recovery:kept-current";
  }
  if (
    /^session changed on disk; adopted the newer transcript \(local changes already covered\)$/i.test(msg) ||
    msg === t("recovery.noticeAdoptedCovered")
  ) {
    return "recovery:adopted-covered";
  }
  if (
    /^session changed on disk; adopted the newer transcript$/i.test(msg) ||
    msg === t("recovery.noticeAdopted")
  ) {
    return "recovery:adopted";
  }
  return "";
}

function quietTranscriptNoticeKey(text: string): string {
  const recovery = recoveryNoticeDedupeKey(text);
  if (recovery) return recovery;

  const msg = text.trim();
  if (/^guardian enabled · model=.+$/i.test(msg)) {
    return "startup:guardian-enabled";
  }
  if (/^\d+ MCP server\(s\) failed to start: .+ \u2014 run \/mcp for details$/i.test(msg)) {
    return "startup:mcp-failures";
  }
  const directMCPFailure = /^mcp\s+([A-Za-z0-9._-]+):\s+.+$/i.exec(msg);
  if (directMCPFailure) {
    const name = directMCPFailure[1].toLowerCase();
    if (!["add", "auth", "config", "connect", "import", "mode", "remove"].includes(name)) {
      return "startup:mcp-failure";
    }
  }
  if (/^plugin ".+" has been slow \d+ startups in a row \(last \d+ms, budget \d+ms\); demoting to background startup this session$/i.test(msg)) {
    return "startup:plugin-demote";
  }
  if (/^.+ applied: session refreshed after the lease was released$/i.test(msg)) {
    return "settings:deferred-refresh-applied";
  }
  return "";
}

function appendNoticeItem(items: Item[], seq: number, id: string, level: "info" | "warn", rawText: string, detail?: string): { items: Item[]; seq: number } {
  if (quietTranscriptNoticeKey(rawText)) {
    return { items, seq };
  }
  const text = localizedBackendNoticeText(rawText);
  if (quietTranscriptNoticeKey(text)) {
    return { items, seq };
  }
  const trimmedDetail = detail?.trim();
  return { items: [...items, { kind: "notice", id, level, text, ...(trimmedDetail ? { detail: trimmedDetail } : {}) }], seq: seq + 1 };
}

function appendNoticeToState(s: State, level: "info" | "warn", text: string, detail?: string): State {
  const next = appendNoticeItem(s.items, s.seq, `n${s.seq}`, level, text, detail);
  return { ...s, running: s.turnActive ? s.running : false, seq: next.seq, items: next.items };
}

function localizedSessionAction(action: string): string {
  switch (action.trim()) {
    case "changing model":
      return t("status.actionChangingModel");
    case "changing effort":
      return t("status.actionChangingEffort");
    case "changing token mode":
      return t("status.actionChangingTokenMode");
    case "rebuilding settings":
      return t("status.actionRebuildingSettings");
    case "switching sessions":
      return t("status.actionSwitchingSessions");
    case "switching tabs":
      return t("status.actionSwitchingTabs");
    case "autosave":
      return t("status.actionAutosave");
    default:
      return action.trim() || t("status.actionCurrentSession");
  }
}

function settingSwitchNoticeText(
  err: unknown,
  setting: "effort" | "model" | "token mode",
  keys: {
    busy: DictKey;
    leaseHeld: DictKey;
    starting: DictKey;
    startupFailed: DictKey;
    retry: DictKey;
    failed: DictKey;
  },
): string {
  const msg = errorMessage(err).trim() || "unknown error";
  const lower = msg.toLowerCase();
  if (lower.includes("finish or cancel") && lower.includes(`before changing ${setting}`)) {
    return t(keys.busy);
  }
  if (lower.includes("already open in another reasonix window") || lower.includes("session lease held")) {
    return t(keys.leaseHeld);
  }
  if (lower.includes("workspace is still starting")) {
    return t(keys.starting);
  }
  if (lower.startsWith("workspace failed to start")) {
    return t(keys.startupFailed, { err: msg });
  }
  if (lower.includes(`changed while switching ${setting}`) || (lower.includes("tab ") && lower.includes("not found"))) {
    return t(keys.retry);
  }
  return t(keys.failed, { err: msg });
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

async function refreshMetaOnlyForTab(tabId: string, dispatchTo: (tabId: string, action: Action) => void): Promise<Meta | undefined> {
  try {
    const meta = await app.MetaForTab(tabId);
    dispatchTo(tabId, { type: "meta", meta });
    return meta;
  } catch {
    return undefined;
  }
}

export function replayPendingPromptsForActiveTab(activeTabId: string | undefined, replay: () => Promise<void> = () => app.ReplayPendingPrompts()): void {
  if (!activeTabId) return;
  void replay().catch(() => {});
}

export function useController() {
  const statesRef = useRef<TabStates>(new Map());
  const lastTurnActivityAtByTab = useRef(new Map<string, number>());
  const cancelReconcileTimers = useRef(new Map<string, number>());
  const [activeTabId, setActiveTabId] = useState<string | undefined>();
  const activeTabIdRef = useRef<string | undefined>(undefined);
  // Invalidates async navigation completions even for ABA switches where the
  // visible tab ID eventually returns to the original value.
  const activeNavigationSeqRef = useRef(0);
  // A render-triggering counter so that mutations to a non-active tab's state still
  // cause a re-render when that tab becomes active.
  const [, setVersion] = useState(0);
  const bump = useCallback(() => setVersion((v) => v + 1), []);
  const beginActiveNavigation = useCallback(() => {
    activeNavigationSeqRef.current += 1;
    return activeNavigationSeqRef.current;
  }, []);

  // The active tab's current state, with a stable identity for cancel().
  const activeState = activeTabId ? getOrCreateState(statesRef.current, activeTabId) : initialState;
  const stateRef = useRef(activeState);
  const backendActiveTabIdRef = useRef<string | undefined>(undefined);
  const backendActivationPromises = useRef(new Map<string, Promise<boolean>>());
  const readyMetaReconcileSeq = useRef(0);
  const readyMetaReconcileActive = useRef<{ tabId: string; seq: number } | undefined>(undefined);
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
  const sessionLoadInFlight = useRef(new Map<string, { sessionPath: string; promise: Promise<void> }>());
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
    options: { skipHistory?: boolean; placeholderItems?: Item[]; preserveCachedHistory?: boolean; sessionPath?: string } = {},
  ) => {
    const sessionPath = (options.sessionPath ?? statesRef.current.get(tabId)?.meta?.sessionPath ?? "").trim();
    const canJoinInFlight = !reset && !options.skipHistory;
    const shouldTrackInFlight = !options.skipHistory;
    if (canJoinInFlight) {
      const existing = sessionLoadInFlight.current.get(tabId);
      if (existing?.sessionPath === sessionPath) return existing.promise;
    } else {
      sessionLoadInFlight.current.delete(tabId);
    }

    const promise = (async () => {
      const seq = bumpSessionLoadSeq(tabId);
      const hydrateStartedAt = Date.now();
      const skipHistory = Boolean(
        options.skipHistory ||
        (options.preserveCachedHistory && !reset && hasReusableCachedTranscript(statesRef.current.get(tabId), options.sessionPath)),
      );
      addBreadcrumb("tab.hydrate", `start ${reason} ${tabId}`);
      dispatchTo(tabId, { type: "hydrate_start", reason, placeholderItems: options.placeholderItems });
      if (reset && sessionLoadCurrent(tabId, seq)) dispatchTo(tabId, { type: "reset" });

      const stillCurrent = () => sessionLoadCurrent(tabId, seq);
      const requiresVisibleTab = reason === "startup" || reason === "switch-tab" || reason === "open-topic";
      const stillVisible = () => !requiresVisibleTab || activeTabIdRef.current === tabId;
      const noteFailure = (label: string, err: unknown) => {
        addBreadcrumb("tab.hydrate", `${label} failed ${tabId}: ${errorMessage(err)}`);
      };

      const loadTimed = async <T,>(label: string, load: () => Promise<T>): Promise<T | undefined> => {
        const startedAt = Date.now();
        addBreadcrumb("tab.hydrate", `${label} start ${reason} ${tabId}`);
        try {
          const value = await load();
          addBreadcrumb("tab.hydrate", `${label} done ${reason} ${tabId} ms=${Date.now() - startedAt}`);
          return value;
        } catch (err) {
          noteFailure(label, err);
          return undefined;
        }
      };

      const historyStartedAt = Date.now();
      const historyPage = skipHistory
        ? undefined
        : await loadTimed("history", () => app.HistoryPageForTab(tabId, 0, HISTORY_PAGE_TURNS));

      if (!stillCurrent()) return;
      if (!skipHistory && historyPage !== undefined) {
        const messages = asArray(historyPage.messages);
        dispatchTo(tabId, { type: "history_page", page: historyPage, mode: "replace" });
        addBreadcrumb(
          "tab.hydrate",
          `history page ${tabId} messages=${messages.length} turns=${historyPage.startTurn}-${historyPage.endTurn}/${historyPage.totalTurns} ms=${Date.now() - historyStartedAt}`,
        );
        if (reason === "switch-tab") {
          addBreadcrumb(
            "tab.switch",
            `history-done ${tabId} messages=${messages.length} turns=${historyPage.startTurn}-${historyPage.endTurn}/${historyPage.totalTurns} ms=${Date.now() - historyStartedAt}`,
          );
        }
      } else if (skipHistory) {
        const skipReason = options.skipHistory ? "cached-live-turn" : "cached-transcript";
        addBreadcrumb("tab.hydrate", `history skipped ${tabId} reason=${skipReason}`);
        if (reason === "switch-tab") {
          addBreadcrumb("tab.switch", `history-done ${tabId} skipped ms=${Date.now() - historyStartedAt}`);
        }
      }

      dispatchTo(tabId, { type: "hydrate_done" });
      addBreadcrumb("tab.hydrate", `done ${reason} ${tabId} ms=${Date.now() - hydrateStartedAt}`);

      // Phase 2: local ancillary data. It stays inside the same in-flight
      // promise so duplicate ready/startup hydrations coalesce, but it runs
      // after hydrate_done so slow Wails calls don't keep the visible transcript
      // in a loading state.
      await new Promise<void>((resolve) => window.setTimeout(resolve, 0));
      if (!stillCurrent()) return;
      if (!stillVisible()) {
        addBreadcrumb("tab.hydrate", `ancillary skipped inactive ${reason} ${tabId}`);
        return;
      }
      const meta = await loadTimed("meta", () => app.MetaForTab(tabId));
      if (!stillCurrent()) return;
      if (!stillVisible()) {
        addBreadcrumb("tab.hydrate", `meta ignored inactive ${reason} ${tabId}`);
        return;
      }
      if (meta !== undefined) dispatchTo(tabId, { type: "meta", meta });
      const ancillaryStartedAt = Date.now();
      const loadAncillary = async <T,>(label: string, load: () => Promise<T>): Promise<T | undefined> => {
        return loadTimed(`ancillary ${label}`, load);
      };
      const [effort, jobs, context] = await Promise.all([
        loadAncillary("effort", () => app.EffortForTab(tabId)),
        loadAncillary("jobs", () => app.JobsForTab(tabId)),
        loadAncillary("context", () => app.ContextUsageForTab(tabId)),
      ]);
      if (!stillCurrent()) return;
      if (!stillVisible()) {
        addBreadcrumb("tab.hydrate", `ancillary ignored inactive ${reason} ${tabId}`);
        return;
      }
      if (effort !== undefined) dispatchTo(tabId, { type: "effort", effort });
      if (jobs !== undefined) dispatchTo(tabId, { type: "jobs", jobs: asArray(jobs) });
      if (context !== undefined) dispatchTo(tabId, { type: "context", context });
      await new Promise<void>((resolve) => window.setTimeout(resolve, 0));
      if (!stillCurrent()) return;
      if (!stillVisible()) {
        addBreadcrumb("tab.hydrate", `checkpoints skipped inactive ${reason} ${tabId}`);
        return;
      }
      const checkpoints = await loadAncillary("checkpoints", () => app.CheckpointsForTab(tabId));
      if (!stillCurrent()) return;
      if (!stillVisible()) {
        addBreadcrumb("tab.hydrate", `checkpoints ignored inactive ${reason} ${tabId}`);
        return;
      }
      if (checkpoints !== undefined) dispatchTo(tabId, { type: "checkpoints", checkpoints: asArray(checkpoints) });
      addBreadcrumb("tab.hydrate", `ancillary ${reason} ${tabId} ms=${Date.now() - ancillaryStartedAt}`);
      app.BalanceForTab(tabId)
        .then((balance) => {
          if (sessionLoadCurrent(tabId, seq) && stillVisible()) dispatchTo(tabId, { type: "balance", balance });
        })
        .catch((err) => { noteFailure("balance", err); });
    })();
    if (shouldTrackInFlight) {
      sessionLoadInFlight.current.set(tabId, { sessionPath, promise });
    }
    try {
      await promise;
    } finally {
      if (sessionLoadInFlight.current.get(tabId)?.promise === promise) {
        sessionLoadInFlight.current.delete(tabId);
      }
    }
  }, [bumpSessionLoadSeq, dispatchTo, sessionLoadCurrent]);

  const loadOlderHistory = useCallback(async (tabId?: string): Promise<void> => {
    const targetTabId = tabId || activeTabIdRef.current;
    if (!targetTabId) return;
    const state = statesRef.current.get(targetTabId);
    if (!state?.historyHasOlder || state.historyOlderLoading || state.running) return;
    const beforeTurn = state.historyStartTurn;
    const sessionPath = state.meta?.sessionPath ?? "";
    dispatchTo(targetTabId, { type: "history_older_start" });
    const startedAt = Date.now();
    try {
      const page = await app.HistoryPageForTab(targetTabId, beforeTurn, HISTORY_PAGE_TURNS);
      const current = statesRef.current.get(targetTabId);
      if (!current || current.historyStartTurn !== beforeTurn || (current.meta?.sessionPath ?? "") !== sessionPath) {
        dispatchTo(targetTabId, { type: "history_older_error" });
        return;
      }
      dispatchTo(targetTabId, { type: "history_page", page, mode: "prepend" });
      addBreadcrumb(
        "tab.hydrate",
        `history older ${targetTabId} messages=${asArray(page.messages).length} turns=${page.startTurn}-${page.endTurn}/${page.totalTurns} ms=${Date.now() - startedAt}`,
      );
    } catch (err) {
      dispatchTo(targetTabId, { type: "history_older_error" });
      addBreadcrumb("tab.hydrate", `history older failed ${targetTabId}: ${errorMessage(err)}`);
    }
  }, [dispatchTo]);

  const activeTabFromBackend = useCallback(async (): Promise<TabMeta | undefined> => {
    const tabs = asArray(await app.ListTabs().catch(() => [] as TabMeta[]));
    return tabs.find((tab) => tab.active) ?? tabs[0];
  }, []);

  const dispatchRuntimeStatusForTab = useCallback((tabId: string, tab: RuntimeMetaSnapshot) => {
    const foregroundRunning = foregroundRunningFromRuntimeMeta(tab);
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
    return foregroundRunning;
  }, [dispatchTo]);

  const waitForTabReady = useCallback(async (tabId: string): Promise<void> => {
    for (let attempt = 0; attempt < 60; attempt += 1) {
      const tabs = asArray(await app.ListTabs().catch(() => [] as TabMeta[]));
      const tab = tabs.find((candidate) => candidate.id === tabId);
      if (!tab || tab.ready || tab.startupErr) return;
      await new Promise((resolve) => window.setTimeout(resolve, 100));
    }
  }, []);

  const syncActiveTabFromBackend = useCallback(async (reset = false, guard = false, options: SyncActiveTabOptions = {}): Promise<string | undefined> => {
    const active = await activeTabFromBackend();
    if (!active) return undefined;
    // When guard is true, skip if the frontend already settled on a
    // different tab while we were fetching — this prevents fire-and-forget
    // calls from mount/onReady from overwriting a user-initiated tab switch
    // (e.g. handleNewTab → ensureBlankSurface / switchTab).
    if (guard && activeTabIdRef.current && activeTabIdRef.current !== active.id) {
      return active.id;
    }
    if (activeTabIdRef.current !== active.id) beginActiveNavigation();
    setActiveTabId(active.id);
    activeTabIdRef.current = active.id;
    confirmBackendActiveTab(active.id);
    dispatchTo(active.id, { type: "optimistic_meta", meta: metaFromTab(active, statesRef.current.get(active.id)?.meta) });
    const preserveCachedHistory = options.preserveCachedHistory ?? !reset;
    if (!reset) dispatchRuntimeStatusForTab(active.id, active);
    await loadSessionDataForTab(active.id, reset, "startup", {
      preserveCachedHistory,
      sessionPath: active.sessionPath,
    });
    if (reset) dispatchRuntimeStatusForTab(active.id, active);
    return active.id;
  }, [activeTabFromBackend, beginActiveNavigation, confirmBackendActiveTab, dispatchRuntimeStatusForTab, dispatchTo, loadSessionDataForTab]);

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
    const foregroundRunning = dispatchRuntimeStatusForTab(tabId, tab);
    const missedTurnDone = Boolean(local?.running && !foregroundRunning);
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
  }, [dispatchRuntimeStatusForTab, loadSessionDataForTab]);

  const clearCancelReconcileTimer = useCallback((tabId: string) => {
    const timer = cancelReconcileTimers.current.get(tabId);
    if (timer === undefined) return;
    window.clearTimeout(timer);
    cancelReconcileTimers.current.delete(tabId);
  }, []);

  const scheduleCancelReconcile = useCallback((tabId: string, attempt = 0) => {
    clearCancelReconcileTimer(tabId);
    const delay = CANCEL_RECONCILE_DELAYS_MS[Math.min(attempt, CANCEL_RECONCILE_DELAYS_MS.length - 1)];
    const timer = window.setTimeout(() => {
      cancelReconcileTimers.current.delete(tabId);
      void reconcileTabRuntime(tabId, { hydrateSessionData: false }).then((tabs) => {
        const tab = tabs?.find((candidate) => candidate.id === tabId);
        if (!tab) return;
        const stillReconciling = foregroundRunningFromRuntimeMeta(tab) || Boolean(tab.cancelRequested);
        if (stillReconciling && attempt + 1 < CANCEL_RECONCILE_DELAYS_MS.length) {
          scheduleCancelReconcile(tabId, attempt + 1);
        }
      }).catch(() => {});
    }, delay);
    cancelReconcileTimers.current.set(tabId, timer);
  }, [clearCancelReconcileTimer, reconcileTabRuntime]);

  useEffect(() => {
    const textBatch = createRafBatch<{ tabId: string; e: WireEvent }>((batch) => {
      for (const { tabId, e } of batch) dispatchTo(tabId, { type: "event", e });
    });
    const off = onEvent((e) => {
      // Untagged compatibility events belong to the tab that the backend has
      // actually activated, not the frontend's optimistic selection. During a
      // slow SetActiveTab these can differ, and routing to the optimistic tab
      // leaks the previous session's approval/ask gate into the new composer.
      const targetTabId = e.tabId || backendActiveTabIdRef.current || activeTabIdRef.current;
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
        if (!e.err) {
          app.HistoryCheckpointTurnsForTab(targetTabId)
            .then((turns) => dispatchTo(targetTabId, { type: "history_checkpoint_turns", turns: asArray(turns) }))
            .catch(() => {});
        }
        app
          .ContextUsageForTab(targetTabId)
          .then((context) => dispatchTo(targetTabId, { type: "context", context }))
          .catch(() => {});
        app.BalanceForTab(targetTabId).then((balance) => dispatchTo(targetTabId, { type: "balance", balance })).catch(() => {});
        app.EffortForTab(targetTabId).then((effort) => dispatchTo(targetTabId, { type: "effort", effort })).catch(() => {});
        void refreshCheckpoints(targetTabId);
        void refreshMetaForTab(targetTabId, dispatchTo);
      }
      if (e.kind === "turn_done" || e.kind === "notice") {
        app.JobsForTab(targetTabId).then((jobs) => dispatchTo(targetTabId, { type: "jobs", jobs: asArray(jobs) })).catch(() => {});
      }
    });

    const offReady = onReady((readyTabId) => {
      const activeId = activeTabIdRef.current;
      if (readyTabId && activeId && readyTabId !== activeId) {
        addBreadcrumb("tab.hydrate", `ready ignored ${readyTabId}`);
        return;
      }
      // A ready event can race the initial hydrate. Refresh the tab metadata
      // first so a stale ready=false snapshot does not keep the composer locked.
      void syncActiveTabFromBackend(false, true, { preserveCachedHistory: true });
    });

    void syncActiveTabFromBackend(false, true);
    // The event subscription is live now, so ask the backend to re-emit any
    // approval/ask prompt that was already blocking a tab before this load —
    // otherwise a session left mid-confirmation shows "waiting" with no modal
    // and no way to stop (#3844).
    void app.ReplayPendingPrompts().catch(() => {});

    return () => {
      textBatch.drain();
      for (const timer of cancelReconcileTimers.current.values()) {
        window.clearTimeout(timer);
      }
      cancelReconcileTimers.current.clear();
      off();
      offReady();
    };
  }, [dispatchTo, loadSessionDataForTab, refreshCheckpoints, syncActiveTabFromBackend]);

  // If the startup ready event is missed, keep the composer lock in sync with
  // the active tab's backend metadata without kicking off tab activation work.
  useEffect(() => {
    const tabId = activeTabId;
    const meta = activeState.meta;
    if (!tabId || !meta || meta.ready || meta.startupErr || activeState.backendActivationPending) {
      readyMetaReconcileSeq.current += 1;
      readyMetaReconcileActive.current = undefined;
      return;
    }

    let cancelled = false;
    let timer: number | undefined;
    const seq = readyMetaReconcileSeq.current + 1;
    readyMetaReconcileSeq.current = seq;
    readyMetaReconcileActive.current = { tabId, seq };

    const stillCurrent = () => {
      const active = readyMetaReconcileActive.current;
      return !cancelled && active?.tabId === tabId && active.seq === seq && activeTabIdRef.current === tabId;
    };

    const schedule = (attempt: number) => {
      timer = window.setTimeout(() => {
        void tick(attempt);
      }, STARTUP_READY_META_RECONCILE_MS);
    };

    const tick = async (attempt: number) => {
      if (!stillCurrent()) return;
      const current = statesRef.current.get(tabId);
      if (!current?.meta || current.meta.ready || current.meta.startupErr || current.backendActivationPending) return;
      const nextMeta = await refreshMetaOnlyForTab(tabId, dispatchTo);
      if (!stillCurrent()) return;
      if (nextMeta?.ready || nextMeta?.startupErr || attempt + 1 >= STARTUP_READY_META_RECONCILE_ATTEMPTS) return;
      schedule(attempt + 1);
    };

    schedule(0);
    return () => {
      cancelled = true;
      if (timer !== undefined) window.clearTimeout(timer);
    };
  }, [activeTabId, activeState.meta?.ready, activeState.meta?.startupErr, activeState.backendActivationPending, dispatchTo]);

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

  const sendToTab = useCallback(async (tabId: string, displayText: string, submitText = displayText, originalText?: string, structured?: import("./invocationDisplay").StructuredInvocationSubmit) => {
    if (!tabId) throw new Error(t("composer.workspaceStarting"));
    const seq = getOrCreateState(statesRef.current, tabId).seq;
    const display = displayText.trim();
    const submit = submitText.trim();
    const original = originalText?.trim() ?? "";
    dispatchTo(tabId, { type: "user", text: displayText, submitText: display !== submit ? submit : undefined, seq });
    invalidateCache();
    try {
      const submitPromise = structured
        ? app.SubmitInvocationsToTab(tabId, structured.display.trim(), structured.input.trim(), structured.invocations)
        : original
        ? app.SubmitEditedDisplayToTab(tabId, display, submit, original)
        : display !== submit ? app.SubmitDisplayToTab(tabId, display, submit) : app.SubmitToTab(tabId, submit);
      void submitPromise.catch((error) => {
        dispatchTo(tabId, { type: "send_failed", error: `Send failed: ${error instanceof Error ? error.message : String(error)}` });
      });
    } catch (error) {
      dispatchTo(tabId, { type: "send_failed", error: `Send failed: ${error instanceof Error ? error.message : String(error)}` });
      throw error;
    }
  }, [dispatchTo]);

  const send = useCallback((displayText: string, submitText = displayText) => {
    const tabId = activeTabIdRef.current ?? activeTabId;
    if (tabId) {
      return sendToTab(tabId, displayText, submitText);
    }
    return activeTabFromBackend().then((active) => {
      if (!active?.id) throw new Error(t("composer.workspaceStarting"));
      setActiveTabId(active.id);
      activeTabIdRef.current = active.id;
      confirmBackendActiveTab(active.id);
      dispatchRuntimeStatusForTab(active.id, active);
      return sendToTab(active.id, displayText, submitText);
    });
  }, [activeTabFromBackend, activeTabId, confirmBackendActiveTab, dispatchRuntimeStatusForTab, sendToTab]);

  const runShellForTab = useCallback(async (tabId: string, command: string) => {
    if (!tabId) throw new Error(t("composer.workspaceStarting"));
    dispatchTo(tabId, { type: "user", text: `!${command}`, seq: getOrCreateState(statesRef.current, tabId).seq });
    try {
      await app.RunShellForTab(tabId, command);
    } catch (error) {
      dispatchTo(tabId, { type: "send_failed", error: `Command failed: ${error instanceof Error ? error.message : String(error)}` });
      throw error;
    }
  }, [dispatchTo]);

  const runShell = useCallback(async (command: string) => {
    if (!activeTabId) throw new Error(t("composer.workspaceStarting"));
    await runShellForTab(activeTabId, command);
  }, [activeTabId, runShellForTab]);

  const steerForTab = useCallback(async (tabId: string, text: string) => {
    if (!tabId) throw new Error(t("composer.workspaceStarting"));
    // No optimistic user bubble: rewind/fork map turns by counting user items,
    // and a steer is not a backend turn — the Steer event's ↪ notice is the
    // visible confirmation (#3660).
    try {
      await app.SteerForTab(tabId, text);
    } catch (error) {
      dispatchTo(tabId, { type: "local_notice", level: "warn", text: `Steer failed: ${error instanceof Error ? error.message : String(error)}` });
      throw error;
    }
  }, [dispatchTo]);

  const steer = useCallback(async (text: string) => {
    if (!activeTabId) throw new Error(t("composer.workspaceStarting"));
    await steerForTab(activeTabId, text);
  }, [activeTabId, steerForTab]);

  const notice = useCallback((text: string, level: "info" | "warn" = "info") => {
    if (!activeTabId) return;
    dispatchTo(activeTabId, { type: "local_notice", level, text });
  }, [activeTabId, dispatchTo]);

  const cancelTab = useCallback((tabId: string) => {
    app.CancelTab(tabId)
      .then(() => scheduleCancelReconcile(tabId))
      .catch((error) => {
        dispatchTo(tabId, { type: "local_notice", level: "warn", text: `Cancel failed: ${errorMessage(error)}` });
      });
  }, [dispatchTo, scheduleCancelReconcile]);

  const cancel = useCallback((): string | undefined => {
    const cur = stateRef.current;
    const tabId = activeTabId;
    if (cur.running && cur.pendingUser !== undefined) {
      const text = cur.pendingUser;
      if (tabId) {
        dispatchTo(tabId, { type: "unsend" });
        cancelTab(tabId);
      }
      return text;
    }
    if (tabId) {
      dispatchTo(tabId, { type: "cancel_requested" });
      cancelTab(tabId);
    }
    return undefined;
  }, [activeTabId, cancelTab, dispatchTo]);

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

  const setCollaborationModeForTab = useCallback(async (tabId: string, mode: CollaborationMode): Promise<void> => {
    if (!tabId) return;
    await app.SetCollaborationModeForTab(tabId, mode).catch(() => {});
    await refreshMetaForTab(tabId, dispatchTo);
  }, [dispatchTo]);

  const setCollaborationMode = useCallback(async (mode: CollaborationMode): Promise<void> => {
    if (!activeTabId) return;
    await setCollaborationModeForTab(activeTabId, mode);
  }, [activeTabId, setCollaborationModeForTab]);

  const setToolApprovalModeForTab = useCallback(async (tabId: string, mode: ToolApprovalMode): Promise<void> => {
    if (!tabId) return;
    await app.SetToolApprovalModeForTab(tabId, mode).catch(() => {});
    if (mode === "auto" || mode === "yolo") dispatchTo(tabId, { type: "clearApproval" });
    await refreshMetaForTab(tabId, dispatchTo);
  }, [dispatchTo]);

  const setToolApprovalMode = useCallback(async (mode: ToolApprovalMode): Promise<void> => {
    if (!activeTabId) return;
    await setToolApprovalModeForTab(activeTabId, mode);
  }, [activeTabId, setToolApprovalModeForTab]);

  const setGoalForTab = useCallback(async (tabId: string, goal: string): Promise<void> => {
    if (!tabId) return;
    await app.SetGoalForTab(tabId, goal).catch(() => {});
    await refreshMetaForTab(tabId, dispatchTo);
  }, [dispatchTo]);

  const setGoal = useCallback(async (goal: string): Promise<void> => {
    if (!activeTabId) return;
    await setGoalForTab(activeTabId, goal);
  }, [activeTabId, setGoalForTab]);

  const clearGoalForTab = useCallback(async (tabId: string): Promise<void> => {
    if (!tabId) return;
    await app.ClearGoalForTab(tabId).catch(() => {});
    await refreshMetaForTab(tabId, dispatchTo);
  }, [dispatchTo]);

  const clearGoal = useCallback(async (): Promise<void> => {
    if (!activeTabId) return;
    await clearGoalForTab(activeTabId);
  }, [activeTabId, clearGoalForTab]);

  const newSession = useCallback(async () => {
    const tabId = activeTabId;
    if (tabId) await waitForTabReady(tabId);
    if (tabId) {
      addBreadcrumb("session.new", `click ${tabId}`);
      bumpCheckpointRefreshSeq(tabId);
      bumpSessionLoadSeq(tabId);
      dispatchTo(tabId, { type: "reset" });
      dispatchTo(tabId, { type: "hydrate_start", reason: "new-session" });
      addBreadcrumb("session.new", `visible-reset ${tabId}`);
    }
    try {
      if (tabId) await app.NewSessionForTab(tabId);
      else await app.NewSession();
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
      dispatchTo(tabId, { type: "history", messages: [] });
      dispatchTo(tabId, { type: "hydrate_done" });
      void refreshMetaForTab(tabId, dispatchTo);
      app.ContextUsageForTab(tabId).then((context) => dispatchTo(tabId, { type: "context", context })).catch(() => {});
      void refreshCheckpoints(tabId);
    }
  }, [activeTabId, bumpCheckpointRefreshSeq, bumpSessionLoadSeq, dispatchTo, loadSessionDataForTab, refreshCheckpoints, waitForTabReady]);

  const clearSession = useCallback(async () => {
    const tabId = activeTabId;
    if (tabId) await waitForTabReady(tabId);
    if (tabId) {
      bumpCheckpointRefreshSeq(tabId);
      bumpSessionLoadSeq(tabId);
    }
    try {
      if (tabId) await app.ClearSessionForTab(tabId);
      else await app.ClearSession();
    } catch {
      if (tabId) void loadSessionDataForTab(tabId);
      return;
    }
    if (tabId) bumpSessionLoadSeq(tabId);
    invalidateCache();
    if (tabId) {
      dispatchTo(tabId, { type: "reset" });
      // Clear placeholder items since no history action follows.
      dispatchTo(tabId, { type: "history", messages: [] });
    }
  }, [activeTabId, bumpCheckpointRefreshSeq, bumpSessionLoadSeq, dispatchTo, loadSessionDataForTab, waitForTabReady]);

  const listSessions = useCallback(async (): Promise<SessionMeta[]> => asArray<SessionMeta>(await app.ListSessions().catch(() => [])), []);
  const listTrashedSessions = useCallback(async (): Promise<SessionMeta[]> => asArray<SessionMeta>(await app.ListTrashedSessions().catch(() => [])), []);
  const resumeSession = useCallback(async (path: string, tabId?: string) => {
    const targetTabId = tabId || activeTabId;
    if (!targetTabId) return;
    beginActiveNavigation();
    if (tabId) await waitForTabReady(tabId);
    else if (!(await waitForBackendActiveTab(targetTabId))) return;
    const seq = bumpSessionLoadSeq(targetTabId);
    dispatchTo(targetTabId, { type: "hydrate_start", reason: "resume-session" });
    let page: HistoryPage;
    try {
      page = tabId
        ? await app.ResumeSessionPageForTab(tabId, path, HISTORY_PAGE_TURNS)
        : await app.ResumeSessionPage(path, HISTORY_PAGE_TURNS);
    } catch (err) {
      if (sessionLoadCurrent(targetTabId, seq)) {
        dispatchTo(targetTabId, { type: "hydrate_error", reason: "resume-session", error: errorMessage(err) });
        dispatchTo(targetTabId, { type: "local_notice", level: "warn", text: `${t("history.failedOpenSession")}: ${errorMessage(err)}` });
      }
      return;
    }
    if (!sessionLoadCurrent(targetTabId, seq)) return;
    dispatchTo(targetTabId, { type: "reset" });
    dispatchTo(targetTabId, { type: "history_page", page, mode: "replace" });
    dispatchTo(targetTabId, { type: "hydrate_done" });
    app.ContextUsageForTab(targetTabId).then((context) => dispatchTo(targetTabId, { type: "context", context })).catch(() => {});
    void refreshCheckpoints(targetTabId);
  }, [activeTabId, beginActiveNavigation, bumpSessionLoadSeq, dispatchTo, refreshCheckpoints, sessionLoadCurrent, waitForBackendActiveTab, waitForTabReady]);

  const openChannelSession = useCallback(async (path: string, tabId: string) => {
    if (!tabId) return;
    beginActiveNavigation();
    await waitForTabReady(tabId);
    const seq = bumpSessionLoadSeq(tabId);
    dispatchTo(tabId, { type: "hydrate_start", reason: "resume-session" });
    let page: HistoryPage;
    try {
      page = await app.OpenChannelSessionPageForTab(tabId, path, HISTORY_PAGE_TURNS);
    } catch (err) {
      if (sessionLoadCurrent(tabId, seq)) {
        dispatchTo(tabId, { type: "hydrate_error", reason: "resume-session", error: errorMessage(err) });
        dispatchTo(tabId, { type: "local_notice", level: "warn", text: `${t("history.failedOpenSession")}: ${errorMessage(err)}` });
      }
      return;
    }
    if (!sessionLoadCurrent(tabId, seq)) return;
    dispatchTo(tabId, { type: "reset" });
    dispatchTo(tabId, { type: "history_page", page, mode: "replace" });
    dispatchTo(tabId, { type: "hydrate_done" });
    app.ContextUsageForTab(tabId).then((context) => dispatchTo(tabId, { type: "context", context })).catch(() => {});
    void refreshCheckpoints(tabId);
  }, [beginActiveNavigation, bumpSessionLoadSeq, dispatchTo, refreshCheckpoints, sessionLoadCurrent, waitForTabReady]);

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
    beginActiveNavigation();
    const path = await app.PickWorkspace().catch(() => "");
    return refreshWorkspaceState(path);
  }, [beginActiveNavigation, refreshWorkspaceState]);
  const switchWorkspace = useCallback(async (path: string): Promise<string> => {
    beginActiveNavigation();
    const next = await app.SwitchWorkspace(path).catch(() => "");
    return refreshWorkspaceState(next);
  }, [beginActiveNavigation, refreshWorkspaceState]);

  const compact = useCallback(() => {
    const tabId = activeTabIdRef.current;
    if (!tabId) return;
    void waitForTabReady(tabId).then(() => app.CompactForTab(tabId).catch(() => {}));
  }, [waitForTabReady]);

  const setModel = useCallback(async (name: string) => {
    if (!activeTabId) return;
    try {
      await app.SetModelForTab(activeTabId, name);
    } catch (err) {
      dispatchTo(activeTabId, { type: "local_notice", level: "warn", text: modelSwitchNoticeText(err) });
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
    try {
      await app.SetEffortForTab(activeTabId, level);
    } catch (err) {
      dispatchTo(activeTabId, { type: "local_notice", level: "warn", text: effortSwitchNoticeText(err) });
      return;
    }
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
      dispatchTo(activeTabId, { type: "local_notice", level: "warn", text: tokenModeSwitchNoticeText(err) });
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

  const rewindForTab = useCallback(async (sourceTabId: string, turn: number, scope: string): Promise<boolean> => {
    if (!sourceTabId) return false;
    const forkNavigationSeq = activeNavigationSeqRef.current;
    await waitForTabReady(sourceTabId);
    const actionScope = (["fork", "summ-from", "summ-upto", "conversation", "code", "both"].includes(scope) ? scope : "both") as MessageActionScope;
    dispatchTo(sourceTabId, { type: "message_action_start", action: { turn, scope: actionScope } });
    dispatchTo(sourceTabId, { type: "local_notice", level: "info", text: messageActionBusyText(actionScope) });
    try {
      if (actionScope === "fork") {
        const tab = await app.ForkForTab(sourceTabId, turn);
        if (tab?.id) {
          const navigationUnchanged = activeNavigationSeqRef.current === forkNavigationSeq;
          const activateFork = tab.active && navigationUnchanged && activeTabIdRef.current === sourceTabId;
          if (!activateFork) {
            dispatchTo(tab.id, { type: "optimistic_meta", meta: metaFromTab(tab, statesRef.current.get(tab.id)?.meta) });
            dispatchRuntimeStatusForTab(tab.id, tab);
            const currentTabId = activeTabIdRef.current;
            if (tab.active && currentTabId) {
              await app.SetActiveTab(currentTabId).then(() => {
                if (activeTabIdRef.current === currentTabId) confirmBackendActiveTab(currentTabId);
              }).catch((err) => {
                addBreadcrumb("tab.fork", `stale reassert failed ${currentTabId}: ${errorMessage(err)}`);
              });
            } else if (!tab.active && navigationUnchanged && currentTabId === sourceTabId) {
              await syncActiveTabFromBackend(false, true);
            }
            addBreadcrumb("tab.fork", `stale completion ${tab.id} current=${currentTabId ?? ""}`);
            return true;
          }
          beginActiveNavigation();
          setActiveTabId(tab.id);
          activeTabIdRef.current = tab.id;
          confirmBackendActiveTab(tab.id);
          dispatchRuntimeStatusForTab(tab.id, tab);
          // The fork's controller builds in a background goroutine: an immediate
          // load reads empty history, and the ready-event fallback can still
          // target the source tab, leaving the fork blank (#3742).
          await waitForTabReady(tab.id);
          await loadSessionDataForTab(tab.id, true);
          await reconcileTabRuntime(tab.id, { hydrateSessionData: false });
        } else {
          await syncActiveTabFromBackend(true);
        }
        return true;
      }

      if (actionScope === "summ-from") await app.SummarizeFromForTab(sourceTabId, turn);
      else if (actionScope === "summ-upto") await app.SummarizeUpToForTab(sourceTabId, turn);
      else await app.RewindForTab(sourceTabId, turn, actionScope);

      const messages = asArray(await app.HistoryForTab(sourceTabId).catch(() => [] as HistoryMessage[]));
      dispatchTo(sourceTabId, { type: "reset" });
      dispatchTo(sourceTabId, { type: "history", messages });
      dispatchTo(sourceTabId, { type: "context", context: await app.ContextUsageForTab(sourceTabId) });
      dispatchTo(sourceTabId, { type: "checkpoints", checkpoints: asArray(await app.CheckpointsForTab(sourceTabId)) });
      return true;
    } catch {
      /* The controller emits a warning notice with the specific failure reason. */
      return false;
    } finally {
      dispatchTo(sourceTabId, { type: "message_action_done" });
    }
  }, [beginActiveNavigation, confirmBackendActiveTab, dispatchRuntimeStatusForTab, dispatchTo, loadSessionDataForTab, reconcileTabRuntime, syncActiveTabFromBackend, waitForTabReady]);

  const rewind = useCallback(async (turn: number, scope: string): Promise<boolean> => {
    if (!activeTabId) return false;
    return rewindForTab(activeTabId, turn, scope);
  }, [activeTabId, rewindForTab]);

  // Tab management: switch preserves per-tab state; open creates it.
  const switchTab = useCallback(async (tabId: string, optimisticTab?: TabMeta): Promise<TabMeta[] | undefined> => {
    beginActiveNavigation();
    const startedAt = Date.now();
    const previousTabId = activeTabIdRef.current;
    const targetSessionPath = optimisticTab?.sessionPath ?? statesRef.current.get(tabId)?.meta?.sessionPath;
    const preserveCachedHistory = hasReusableCachedTranscript(statesRef.current.get(tabId), targetSessionPath);
    addBreadcrumb("tab.switch", `click ${tabId}`);
    setActiveTabId(tabId);
    activeTabIdRef.current = tabId;
    dispatchTo(tabId, { type: "backend_activation_start" });
    if (optimisticTab) {
      dispatchTo(tabId, { type: "optimistic_meta", meta: metaFromTab(optimisticTab, statesRef.current.get(tabId)?.meta) });
      const optimisticStatus = backendStatusFromRuntimeMeta(optimisticTab);
      if (optimisticStatus.running) dispatchTo(tabId, optimisticStatus);
    }
    dispatchTo(tabId, { type: "hydrate_start", reason: "switch-tab" });
    addBreadcrumb("tab.switch", `active-rendered ${tabId} ms=${Date.now() - startedAt}`);
    const backendActivation = app.SetActiveTab(tabId)
      .then(async () => {
        if (activeTabIdRef.current !== tabId) {
          const currentTabId = activeTabIdRef.current;
          if (currentTabId) {
            await app.SetActiveTab(currentTabId).then(() => {
              if (activeTabIdRef.current === currentTabId) confirmBackendActiveTab(currentTabId);
            }).catch((err) => {
              addBreadcrumb("tab.switch", `set-active-stale-reassert-failed ${currentTabId}: ${errorMessage(err)}`);
            });
          }
          addBreadcrumb("tab.switch", `set-active-stale ${tabId} current=${currentTabId ?? ""} ms=${Date.now() - startedAt}`);
          return false;
        }
        confirmBackendActiveTab(tabId);
        addBreadcrumb("tab.switch", `set-active-done ${tabId} ms=${Date.now() - startedAt}`);
        return true;
      })
      .catch((err) => {
        dispatchTo(tabId, { type: "backend_activation_done" });
        dispatchTo(tabId, { type: "hydrate_error", reason: "switch-tab", error: errorMessage(err) });
        if (previousTabId && activeTabIdRef.current === tabId) {
          setActiveTabId(previousTabId);
          activeTabIdRef.current = previousTabId;
          addBreadcrumb("tab.switch", `set-active-failed-reverted ${tabId} -> ${previousTabId} ms=${Date.now() - startedAt}`);
        }
        return false;
      });
    trackBackendActivation(tabId, backendActivation);
    const backendSwitch = backendActivation
      .then(async (activated) => {
        if (!activated) return undefined;
        const tabs = await reconcileTabRuntime(tabId, { hydrateSessionData: false });
        void loadSessionDataForTab(tabId, false, "switch-tab", {
          skipHistory: hasCachedLiveTurn(statesRef.current.get(tabId)),
          preserveCachedHistory,
          sessionPath: targetSessionPath,
        });
        return tabs;
      })
      .catch((err) => {
        dispatchTo(tabId, { type: "hydrate_error", reason: "switch-tab", error: errorMessage(err) });
        return undefined;
      });
    return backendSwitch;
  }, [beginActiveNavigation, confirmBackendActiveTab, dispatchTo, loadSessionDataForTab, reconcileTabRuntime, trackBackendActivation]);

  const openProjectTab = useCallback(async (workspaceRoot: string, topicId: string): Promise<TabMeta> => {
    beginActiveNavigation();
    const meta = await app.OpenProjectTab(workspaceRoot, topicId);
    const prevItems = activeTabIdRef.current ? statesRef.current.get(activeTabIdRef.current)?.items : undefined;
    const prevState = statesRef.current.get(meta.id);
    const isNewTab = !prevState;
    const preserveCachedHistory = hasReusableCachedTranscript(prevState, meta.sessionPath);
    setActiveTabId(meta.id);
    activeTabIdRef.current = meta.id;
    confirmBackendActiveTab(meta.id);
    dispatchTo(meta.id, { type: "optimistic_meta", meta: metaFromTab(meta, statesRef.current.get(meta.id)?.meta) });
    dispatchRuntimeStatusForTab(meta.id, meta);
    const load = loadSessionDataForTab(meta.id, isNewTab, "open-topic", {
      placeholderItems: isNewTab ? prevItems : undefined,
      preserveCachedHistory,
      sessionPath: meta.sessionPath,
    });
    if (isNewTab) void load.then(() => reconcileTabRuntime(meta.id, { hydrateSessionData: false })).catch(() => {});
    else void load;
    return meta;
  }, [beginActiveNavigation, confirmBackendActiveTab, dispatchRuntimeStatusForTab, dispatchTo, loadSessionDataForTab, reconcileTabRuntime]);

  const openGlobalTab = useCallback(async (topicId: string): Promise<TabMeta> => {
    beginActiveNavigation();
    const meta = await app.OpenGlobalTab(topicId);
    const prevItems = activeTabIdRef.current ? statesRef.current.get(activeTabIdRef.current)?.items : undefined;
    const prevState = statesRef.current.get(meta.id);
    const isNewTab = !prevState;
    const preserveCachedHistory = hasReusableCachedTranscript(prevState, meta.sessionPath);
    setActiveTabId(meta.id);
    activeTabIdRef.current = meta.id;
    confirmBackendActiveTab(meta.id);
    dispatchTo(meta.id, { type: "optimistic_meta", meta: metaFromTab(meta, statesRef.current.get(meta.id)?.meta) });
    dispatchRuntimeStatusForTab(meta.id, meta);
    const load = loadSessionDataForTab(meta.id, isNewTab, "open-topic", {
      placeholderItems: isNewTab ? prevItems : undefined,
      preserveCachedHistory,
      sessionPath: meta.sessionPath,
    });
    if (isNewTab) void load.then(() => reconcileTabRuntime(meta.id, { hydrateSessionData: false })).catch(() => {});
    else void load;
    return meta;
  }, [beginActiveNavigation, confirmBackendActiveTab, dispatchRuntimeStatusForTab, dispatchTo, loadSessionDataForTab, reconcileTabRuntime]);

  const openTopicSession = useCallback(async (scope: string, workspaceRoot: string, topicId: string, sessionPath: string): Promise<TabMeta> => {
    beginActiveNavigation();
    const meta = await app.OpenTopicSession(scope, workspaceRoot, topicId, sessionPath);
    const prevItems = activeTabIdRef.current ? statesRef.current.get(activeTabIdRef.current)?.items : undefined;
    const prevState = statesRef.current.get(meta.id);
    const isNewTab = !prevState;
    const preserveCachedHistory = hasReusableCachedTranscript(prevState, meta.sessionPath);
    setActiveTabId(meta.id);
    activeTabIdRef.current = meta.id;
    confirmBackendActiveTab(meta.id);
    dispatchTo(meta.id, { type: "optimistic_meta", meta: metaFromTab(meta, statesRef.current.get(meta.id)?.meta) });
    dispatchRuntimeStatusForTab(meta.id, meta);
    const load = loadSessionDataForTab(meta.id, isNewTab, "open-topic", {
      placeholderItems: isNewTab ? prevItems : undefined,
      preserveCachedHistory,
      sessionPath: meta.sessionPath,
    });
    if (isNewTab) void load.then(() => reconcileTabRuntime(meta.id, { hydrateSessionData: false })).catch(() => {});
    else void load;
    return meta;
  }, [beginActiveNavigation, confirmBackendActiveTab, dispatchRuntimeStatusForTab, dispatchTo, loadSessionDataForTab, reconcileTabRuntime]);

  const activateTopic = useCallback(async (scope: string, workspaceRoot: string, topicId: string, sessionPath = ""): Promise<TabMeta> => {
    beginActiveNavigation();
    const meta = await app.ActivateTopic(scope, workspaceRoot, topicId, sessionPath);
    // Save previous tab's items so the new tab can use them as a placeholder
    // during loading, avoiding a blank/Welcome flash before history arrives.
    const prevItems = activeTabIdRef.current ? statesRef.current.get(activeTabIdRef.current)?.items : undefined;
    for (const id of Array.from(statesRef.current.keys())) {
      if (id !== meta.id) statesRef.current.delete(id);
    }
    setActiveTabId(meta.id);
    activeTabIdRef.current = meta.id;
    confirmBackendActiveTab(meta.id);
    dispatchTo(meta.id, { type: "optimistic_meta", meta: metaFromTab(meta, statesRef.current.get(meta.id)?.meta) });
    dispatchRuntimeStatusForTab(meta.id, meta);
    void loadSessionDataForTab(meta.id, true, "open-topic", { placeholderItems: prevItems })
      .then(() => reconcileTabRuntime(meta.id, { hydrateSessionData: false }))
      .catch(() => {});
    return meta;
  }, [beginActiveNavigation, confirmBackendActiveTab, dispatchRuntimeStatusForTab, dispatchTo, loadSessionDataForTab, reconcileTabRuntime]);

  // Ensure a blank tab exists for the given scope — reuses an existing one
  // or creates a new tab, then loads its session data.
  const ensureBlankTab = useCallback(async (scope: string, workspaceRoot: string): Promise<TabMeta> => {
    beginActiveNavigation();
    const meta = await app.EnsureBlankTab(scope, workspaceRoot);
    const isNewTab = !statesRef.current.has(meta.id);
    setActiveTabId(meta.id);
    activeTabIdRef.current = meta.id;
    confirmBackendActiveTab(meta.id);
    dispatchTo(meta.id, { type: "optimistic_meta", meta: metaFromTab(meta, statesRef.current.get(meta.id)?.meta) });
    dispatchRuntimeStatusForTab(meta.id, meta);
    const load = loadSessionDataForTab(meta.id, isNewTab, "open-topic");
    if (isNewTab) void load.then(() => reconcileTabRuntime(meta.id, { hydrateSessionData: false })).catch(() => {});
    else void load;
    return meta;
  }, [beginActiveNavigation, confirmBackendActiveTab, dispatchRuntimeStatusForTab, dispatchTo, loadSessionDataForTab, reconcileTabRuntime]);

  const ensureBlankSurface = useCallback(async (scope: string, workspaceRoot: string): Promise<TabMeta> => {
    beginActiveNavigation();
    const meta = await app.EnsureBlankSurface(scope, workspaceRoot);
    for (const id of Array.from(statesRef.current.keys())) {
      if (id !== meta.id) statesRef.current.delete(id);
    }
    setActiveTabId(meta.id);
    activeTabIdRef.current = meta.id;
    confirmBackendActiveTab(meta.id);
    dispatchTo(meta.id, { type: "optimistic_meta", meta: metaFromTab(meta, statesRef.current.get(meta.id)?.meta) });
    dispatchRuntimeStatusForTab(meta.id, meta);
    void loadSessionDataForTab(meta.id, true, "open-topic")
      .then(() => reconcileTabRuntime(meta.id, { hydrateSessionData: false }))
      .catch(() => {});
    return meta;
  }, [beginActiveNavigation, confirmBackendActiveTab, dispatchRuntimeStatusForTab, dispatchTo, loadSessionDataForTab, reconcileTabRuntime]);

  const closeTab = useCallback(async (tabId: string) => {
    if (tabId === activeTabIdRef.current) beginActiveNavigation();
    try {
      await app.CloseTab(tabId);
      statesRef.current.delete(tabId);
      bump();
      if (tabId === activeTabId) await syncActiveTabFromBackend(false);
    } catch { /* ignore */ }
  }, [activeTabId, beginActiveNavigation, bump, syncActiveTabFromBackend]);

  const reorderTabs = useCallback(async (tabIds: string[]) => {
    try {
      await app.ReorderTabs(tabIds);
    } catch { /* ignore */ }
  }, []);

  return {
    state: activeState,
    activeTabId,
    send, sendToTab, runShell, runShellForTab, steer, steerForTab, notice, cancel, approve, answerQuestion, setControllerMode,
    setCollaborationMode, setCollaborationModeForTab, setToolApprovalMode, setToolApprovalModeForTab, setGoal, setGoalForTab, clearGoal, clearGoalForTab,
    newSession, clearSession, listSessions, listTrashedSessions, resumeSession, openChannelSession, previewSession, deleteSession, restoreSession, purgeTrashedSession, renameSession,
    loadOlderHistory,
    refreshMeta, pickWorkspace, switchWorkspace, compact, rewind, rewindForTab, setModel, setEffort, setTokenMode,
    fetchMemory, remember, forget, saveDoc,
    switchTab, openProjectTab, openGlobalTab, openTopicSession, ensureBlankTab, activateTopic, ensureBlankSurface, closeTab, reorderTabs,
    syncActiveTab: syncActiveTabFromBackend,
  };
}
