// useController is the frontend's state machine over the agent event stream. Per-tab
// state preserves background streaming output, tools, and approvals across tab switches;
// components render only the active tab's state.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { asArray } from "./array";
import { addBreadcrumb } from "./breadcrumbs";
import { app, onEvent, onReady, onRuntimeRebuilt, onTabMeta, onTopicActivation } from "./bridge";
import { invalidateCache } from "./composerHistory";
import { formatInboxCancelError, inboxSteerQueuedMessage } from "./inboxError";
import { formatContextMaintenanceNotice, isNewMaintenanceOperation, rememberMaintenanceOperation } from "./contextMaintenanceTypes";
import { formatGuardianAssessmentNotice } from "./guardianEvents";
import { invalidateSharedQuery } from "./queryCoalesce";
import { createRafBatch } from "./rafBatch";
import { aliasActivationRequest, noteActivationRequested, noteActivationSettled, noteActivationStarted } from "./sessionDiagnostics";
import { applyLiveSegments, coalesceStreamDeltas, completeLiveReasoning, type StreamDeltaEntry, type StreamSegment } from "./streamDeltaBatch";
import { getTranscriptStore } from "./transcriptStore";
import { uiPerfTracker } from "./uiPerf";
import { getLocale, t, type DictKey } from "./i18n";
import { sameTodoList } from "./todoVisibility";
import { fileDiffFromWire, summarize, summarizeFileDiff, type ToolFileDiff } from "./tools";
import { modeHasAutoApproveTools, normalizeMode, normalizeToolApprovalMode } from "./types";
import type {
  BalanceInfo,
  CheckpointMeta,
  CollaborationMode,
  ContextInfo,
  DeliveryWorktreeOpenResult,
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
  TopicActivationEvent,
  WireApproval,
  WireAsk,
  WireDecisionReceipt,
  WireEvent,
  WireExtensionCard,
  WireExtensionForm,
  WireExtensionStatus,
  WireExtensionSurface,
  WireFinalReadiness,
  WireTool,
  WireUsage,
  WireShellExecution,
} from "./types";

export type ToolStatus = "running" | "done" | "error" | "stopped";

// Reserved ToolProgress channel names for sub-agent progress previews (the Go
// tracker emits these; ordinary tool progress must never use them).
export const SUBAGENT_PROGRESS_STATUS = "reasonix.subagent.status";
export const SUBAGENT_PROGRESS_REASONING = "reasonix.subagent.reasoning";
export const SUBAGENT_PROGRESS_TEXT = "reasonix.subagent.text";
export const SUBAGENT_PROGRESS_NOTICE = "reasonix.subagent.notice";

// Reserved names are matched by prefix so a future channel never falls back
// to ordinary tool output on older frontends.
const SUBAGENT_PROGRESS_PREFIX = "reasonix.subagent.";
const TURN_ACTIVITY_KINDS = new Set(["turn_started", "text", "reasoning", "message", "tool_dispatch", "tool_progress", "tool_result"]);

const SUBAGENT_PROGRESS_PHASES = new Set([
  "queued", "running", "reasoning", "responding", "tool", "retrying", "completed", "failed", "cancelled",
]);

// Tool names that initialize a sub-agent progress card. parallel_tasks/fleet
// are group cards: they settle when their whole child progress tree is
// terminal, since they never receive a terminal status of their own.
const SUBAGENT_PROGRESS_TOOLS = new Set(["task", "read_only_task", "parallel_tasks", "fleet"]);

// Per-channel preview retention. The backend already bounds what it sends
// (8 KiB pending per child); these caps keep one hot card from dominating the
// live conversation memory.
const SUBAGENT_PREVIEW_REASONING_LIMIT = 8 << 10;
const SUBAGENT_PREVIEW_TEXT_LIMIT = 8 << 10;
const SUBAGENT_PREVIEW_NOTICE_LIMIT = 2 << 10;

export type SubagentPhase =
  | "queued" | "running" | "reasoning" | "responding" | "tool" | "retrying"
  | "completed" | "failed" | "cancelled";

// In-memory-only sub-agent progress preview. Never persisted: history
// hydration rebuilds tool items from the transcript without these fields, and
// the full sub-agent transcript stays the source of truth after a restart.
export type SubagentProgress = {
  phase: SubagentPhase;
  reasoning: string;
  text: string;
  notice: string;
  lastActivityAt: number;
  truncated: boolean;
  durationMs?: number;
  startedAt: number;
};

export function isSubagentProgressName(name: string | undefined): boolean {
  return !!name && name.startsWith(SUBAGENT_PROGRESS_PREFIX);
}

export function isTerminalSubagentPhase(phase: string | undefined): boolean {
  return phase === "completed" || phase === "failed" || phase === "cancelled";
}

function isGroupSubagentTool(name: string): boolean {
  return name === "parallel_tasks" || name === "fleet";
}

function terminalStatusOf(phase: string): ToolStatus {
  switch (phase) {
    case "completed": return "done";
    case "failed": return "error";
    case "cancelled": return "stopped";
  }
  return "running";
}

function freshSubagentProgress(): SubagentProgress {
  const now = Date.now();
  return { phase: "running", reasoning: "", text: "", notice: "", lastActivityAt: now, truncated: false, startedAt: now };
}

/** Keeps the most recent `limit` code points; surrogate pairs stay intact. */
function tailPreview(text: string, limit: number): string {
  if (text.length <= limit) return text;
  const pts = Array.from(text);
  return pts.slice(pts.length - limit).join("");
}

// --- Sub-agent progress reducer helpers --------------------------------------

// Applies one reserved ToolProgress event to the target card's in-memory
// preview. The card must exist (its dispatch always precedes progress events)
// and have been initialized by the dispatch. Never writes tool.output, never
// touches the parent LiveStream, and never produces history data.
function applySubagentProgress(s: State, t: WireTool): State {
  if (!t.id) return s;
  const idx = s.items.findIndex((it) => it.kind === "tool" && it.id === t.id);
  if (idx < 0) return s;
  const next = [...s.items];
  const it = next[idx];
  if (it.kind !== "tool" || !it.subagentProgress) return s;
  const sp: SubagentProgress = { ...it.subagentProgress, lastActivityAt: Date.now() };
  switch (t.name) {
    case SUBAGENT_PROGRESS_STATUS: {
      const phase = t.output ?? "";
      if (!SUBAGENT_PROGRESS_PHASES.has(phase)) return s; // unknown phase: ignore
      sp.phase = phase as SubagentPhase;
      if (isTerminalSubagentPhase(phase) && typeof t.durationMs === "number") sp.durationMs = t.durationMs;
      break;
    }
    case SUBAGENT_PROGRESS_REASONING:
      sp.reasoning = tailPreview(sp.reasoning + (t.output ?? ""), SUBAGENT_PREVIEW_REASONING_LIMIT);
      sp.truncated = sp.truncated || !!t.truncated;
      break;
    case SUBAGENT_PROGRESS_TEXT:
      sp.text = tailPreview(sp.text + (t.output ?? ""), SUBAGENT_PREVIEW_TEXT_LIMIT);
      sp.truncated = sp.truncated || !!t.truncated;
      break;
    case SUBAGENT_PROGRESS_NOTICE:
      sp.notice = tailPreview(sp.notice + (t.output ?? ""), SUBAGENT_PREVIEW_NOTICE_LIMIT);
      sp.truncated = sp.truncated || !!t.truncated;
      break;
    default:
      return s;
  }
  const status = isTerminalSubagentPhase(sp.phase) ? terminalStatusOf(sp.phase) : it.status;
  next[idx] = { ...it, subagentProgress: sp, status };
  return { ...s, items: next };
}

// Nested real tool activity refreshes its sub-agent parent's recent activity
// and switches the phase to "tool". Terminal parents are left untouched.
function touchSubagentParent(next: Item[], parentId: string): void {
  const idx = next.findIndex((it) => it.kind === "tool" && it.id === parentId && it.subagentProgress);
  if (idx < 0) return;
  const it = next[idx];
  if (it.kind !== "tool" || !it.subagentProgress || isTerminalSubagentPhase(it.subagentProgress.phase)) return;
  next[idx] = { ...it, subagentProgress: { ...it.subagentProgress, phase: "tool", lastActivityAt: Date.now() } };
}

export type LiveStream = {
  id: string;
  text: string;
  reasoning: string;
  reasoningComplete: boolean;
  reasoningStartedAt?: number;
  reasoningCompletedAt?: number;
};

/** Speculative journal for one sampling attempt — rolled back on discard. */
type StreamAttemptJournal = {
  id: string;
  baselineLive?: LiveStream;
  baselineTurnArgChars: number;
  /** Tool cards created by this attempt (running, no result yet). */
  createdToolIds: string[];
  /** Prior state of tools that existed before this attempt and were patched. */
  priorTools: Record<string, Extract<Item, { kind: "tool" }>>;
};
export type ControllerLiveStore = {
  subscribe: (tabId: string | undefined, listener: () => void) => () => void;
  getSnapshot: (tabId: string | undefined) => LiveStream | undefined;
  getModelActiveAt?: (tabId: string | undefined) => number | undefined;
};
export type MessageActionScope = "fork" | "summ-from" | "summ-upto" | "conversation" | "code" | "both";
export type MessageActionState = { turn: number; scope: MessageActionScope };
export type HydrateReason = "switch-tab" | "new-session" | "resume-session" | "open-topic" | "startup" | "rewind";
type SyncActiveTabOptions = {
  preserveCachedHistory?: boolean;
};

// A ticketed StartTopicActivation in flight. Only the latest one is tracked:
// superseded requests get "cancelled" from the backend and are ignored.
type PendingTopicActivation = {
  requestId: string;
  navigationSeq: number;
  tabId?: string;
  placeholderItems?: Item[];
  /** Terminal event that arrived before the ticket resolved. */
  terminal?: TopicActivationEvent;
};

type ModelSwitchQueueResult = "applied" | "superseded";

type ModelSwitchQueueRequest = {
  name: string;
  resolve: (result: ModelSwitchQueueResult) => void;
  reject: (err: unknown) => void;
};

type ModelSwitchQueueState = {
  running: boolean;
  pending?: ModelSwitchQueueRequest;
  fallbackBalance?: BalanceInfo;
};

const HISTORY_PAGE_TURNS = 60;

export type Item =
  | { kind: "user"; id: string; submissionId?: string; text: string; submitText?: string; failed?: boolean; createdAt?: number; checkpointTurn?: number }
  | { kind: "assistant"; id: string; text: string; reasoning: string; streaming: boolean; reasoningComplete?: boolean; reasoningDurationMs?: number; workDurationMs?: number; memoryCitations?: MemoryCitation[] }
  | { kind: "phase"; id: string; text: string }
  | { kind: "notice"; id: string; level: "info" | "warn"; text: string; detail?: string; title?: string; variant?: "delivery"; action?: "continue_delivery"; decisionReceipt?: WireDecisionReceipt }
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
      resolvedName?: string;
      capabilityId?: string;
      status: ToolStatus;
      output?: string;
      error?: string;
      truncated?: boolean;
      dataArchived?: boolean; // args/output trimmed for memory; full data available via backend
      durationMs?: number;
      subject?: string; // stable collapsed subject from archived history payloads
      summary?: string; // stable collapsed readout kept even after args/output archive
      fileDiff?: ToolFileDiff; // previewed whole-file diff from writer dispatch
      isShell?: boolean; // bash tool or !command — structured shell card presentation
      execution?: WireShellExecution; // local shell metadata
      parentId?: string; // a sub-agent call nests under the `task` call with this id
      profile?: { model?: string; effort?: string }; // subagent model/effort from tool event
      argChars?: number; // args still streaming from the model: cumulative chars received
      subagentProgress?: SubagentProgress; // in-memory-only preview, never hydrated from history
    }
  | {
      kind: "extension";
      id: string;
      // surfaceKey is "<pluginId>:<surfaceId>"; a re-published card replaces the
      // previous one in place instead of appending a duplicate transcript entry.
      surfaceKey: string;
      pluginId: string;
      surfaceId: string;
      generation?: number;
      card: WireExtensionCard;
    };

type ToolItem = Extract<Item, { kind: "tool" }>;
export type ExtensionItem = Extract<Item, { kind: "extension" }>;

// Extension UI surfaces (stage 8b2) — per-tab state fed by extension_surface /
// extension_status wire events. Statuses and generations key on
// "<pluginId>:<surfaceId>"; the form is the single pending form surface (a new
// form replaces the old, matching the backend's one-blocking-prompt model);
// notifications queue until the App drains them into the toast system.
export interface ExtensionStatusEntry {
  pluginId: string;
  surfaceId: string;
  label: string;
  detail?: string;
  severity?: string;
  progress?: number;
  generation?: number;
}

export interface ExtensionFormState {
  pluginId: string;
  surfaceId: string;
  generation?: number;
  form: WireExtensionForm;
}

export interface ExtensionNotificationEntry {
  id: string;
  pluginId: string;
  title: string;
  body?: string;
  severity?: string;
}

// extensionSurfaceKey is the identity a sidecar re-publishes under to replace
// one of its surfaces.
export function extensionSurfaceKey(surface: Pick<WireExtensionSurface, "pluginId" | "surfaceId">): string {
  return `${surface.pluginId}:${surface.surfaceId}`;
}

// acceptsExtensionGeneration drops a re-ordered surface publication: within
// one runtime, a sidecar's generation is monotonic, so anything older than the
// last accepted generation for the same surface is stale. Events without a
// generation always pass (they carry no ordering claim).
export function acceptsExtensionGeneration(stored: number | undefined, incoming: number | undefined): boolean {
  return incoming === undefined || stored === undefined || incoming >= stored;
}

// Mid-turn steer messages are recorded as info notices carrying this prefix —
// both live (the "steer" event below) and in replayed history (desktop/app.go
// prefixes persisted steers the same way). The prefix is the only durable
// marker, so display code identifies steers by it.
export const STEER_NOTICE_PREFIX = "↪ ";

export function isSteerNoticeText(text: string): boolean {
  return text.startsWith(STEER_NOTICE_PREFIX);
}

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
  historyRevision?: number;
  historyDigest?: string;
  backendActivationPending: boolean;
  messageAction?: MessageActionState;
  currentAssistant?: string;
  live?: LiveStream;
  pendingUser?: string;
  pendingSubmissionId?: string;
  deliveryRecoveryActive: boolean;
  discardTurn?: boolean;
  turnStartAt: number;
  turnDoneAt: number;
  // Completion tokens accumulated across executor usage events within the
  // current turn. ReasoningTokens is a subset of CompletionTokens.
  turnOutputTokens: number;
  turnOutputChars: number;
  // Live text/reasoning characters already covered by the accumulated usage.
  // This lets the composer estimate only the in-flight provider request.
  turnOutputCharsAtUsage: number;
  // True when any output-token count in the current turn is estimated.
  turnOutputEstimated: boolean;
  // Active provider-output intervals for the current turn. Tool execution and
  // gaps between provider requests are intentionally excluded from TPS.
  turnModelActiveAt?: number;
  turnModelActiveMs: number;
  // Time spent waiting on the user (approval/ask) within the current turn.
  // Closed intervals accumulate here; an open interval uses promptWaitStartedAt
  // so background tabs keep counting while not rendered by Composer.
  turnWaitAccumMs: number;
  // Last completed turn's values — preserved across turn boundaries so the
  // status bar can display the most recent completed turn's TPS and token
  // counts until the current turn finishes and overwrites them.
  lastTurnOutputTokens: number;
  lastTurnStartAt: number;
  lastTurnDoneAt: number;
  lastTurnWaitAccumMs: number;
  lastTurnModelMs: number;
  lastTurnOutputEstimated: boolean;
  // Per-request rate (null when unmeasurable) and pending interval are tab-local.
  lastRequestTps?: number | null;
  pendingRequestModelMs?: number;
  promptWaitStartedAt?: number;
  // promptEventClock() reading taken when the CURRENT pending prompt first
  // arrived. Orders the prompt against reconciliation snapshots so a snapshot
  // fetched before the event cannot clear the prompt it never knew about
  // (#6429). Anchored to the prompt's first arrival and NOT advanced by a
  // same-id replay, so an authoritative idle snapshot taken after the user
  // answered is never mistaken for stale (#6432 reverse race).
  promptArrivedAt?: number;
  // Id of the prompt promptArrivedAt is anchored to. A replay re-emitting the
  // same id keeps the original arrival time; only a genuinely new prompt id
  // (backend ids are monotonic within a controller) re-anchors it.
  promptArrivedId?: string;
  // Id of the most recently user-resolved approval/ask (explicit answer,
  // cancel-through-mode-switch, etc). A replay carrying this same id is a
  // stale re-delivery of an already-answered prompt, not a new one — arming
  // it would resurrect a zombie no downstream snapshot may ever get a chance
  // to reject (#6432 round 2: idle-applied-before-replay, and
  // running=true/pendingPrompt=false snapshots that never clear approval/ask).
  resolvedPromptId?: string;
  // Monotonic per-tab prompt-id namespace generation. Approval/ask ids restart
  // from "1" whenever the backend controller is rebuilt, so any id captured
  // before the bump (an in-flight prompt answer or mode-switch RPC) must not
  // touch bookkeeping written after it. Late callbacks from the old controller
  // otherwise act on a different prompt that reused the same numeric id.
  promptEpoch: number;
  turnTokens: number;
  turnTotalTokens: number;
  turnCost: number;
  // Cumulative argument characters of the tool call currently streaming its
  // args (partial dispatch progress). Folded into the composer pill as an
  // estimated-token tail; cleared when the round's usage arrives (which then
  // includes those tokens for real) and on turn start.
  turnArgChars: number;
  sessionTokens: number;
  sessionCost: number;
  sessionCurrency: string;
  retry?: { attempt: number; max: number; observedAt: number };
  seq: number;
  sessionGen: number;
  // Per-session counter bumped after hydration ancillary data (context, effort,
  // jobs) arrives. ContextPanel reads this (merged into refreshKey) so the
  // right-side panel re-fetches after a session rebind instead of showing stale
  // RequestCount / ElapsedMs / SessionCost from before the swap.
  contextPanelSeq: number;
  // Monotonic count of usage events from ANY source (executor, subagent,
  // title…). Drives right-panel snapshot refreshes so sub-agent activity keeps
  // the session metrics live; state.usage stays executor-gated for the gauge.
  usageSeq: number;
  // Bounded set of context_maintenance operationIds already shown as notices
  // so reconnect/replay does not insert duplicate timeline cards.
  seenMaintenanceOps: string[];
  // Extension UI surfaces (stage 8b2). See the ExtensionStatusEntry block
  // above for the keying/lifecycle rules.
  extensionStatuses: Record<string, ExtensionStatusEntry>;
  extensionForm?: ExtensionFormState;
  extensionNotifications: ExtensionNotificationEntry[];
  // Last accepted generation per extension surface key; guards against
  // re-ordered publications (acceptsExtensionGeneration).
  extensionGenerations: Record<string, number>;
  // Speculative sampling-attempt journal for Codex-style stream replay.
  // Host-local only; never hydrated from history.
  streamAttemptJournal?: StreamAttemptJournal;
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
  deliveryRecoveryActive: false,
  promptEpoch: 0,
  turnStartAt: 0,
  turnDoneAt: 0,
  turnOutputTokens: 0,
  turnOutputChars: 0,
  turnOutputCharsAtUsage: 0,
  turnOutputEstimated: false,
  turnModelActiveMs: 0,
  turnWaitAccumMs: 0,
  lastTurnOutputTokens: 0,
  lastTurnStartAt: 0,
  lastTurnDoneAt: 0,
  lastTurnWaitAccumMs: 0,
  lastTurnModelMs: 0,
  lastTurnOutputEstimated: false,
  turnTokens: 0,
  turnTotalTokens: 0,
  turnCost: 0,
  turnArgChars: 0,
  sessionTokens: 0,
  sessionCost: 0,
  sessionCurrency: "¥",
  seq: 0,
  sessionGen: 0,
  contextPanelSeq: 0,
  usageSeq: 0,
  seenMaintenanceOps: [],
  extensionStatuses: {},
  extensionNotifications: [],
  extensionGenerations: {},
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

// Clock used to order live prompt events against runtime snapshot fetches.
// Monotonic (immune to wall-clock jumps) with sub-millisecond resolution, so
// an event and a snapshot initiated in the same millisecond still order
// correctly. Only ever compared against itself.
export function promptEventClock(): number {
  return typeof performance !== "undefined" ? performance.now() : Date.now();
}

// True when a runtime snapshot was fetched before the tab's live approval/ask
// event arrived. Such a snapshot reports the tab idle only because it predates
// the prompt (pre-attach ListTabs, activation-time metas); applying it would
// clear the only UI able to answer the prompt — and, since it also carries
// pendingPrompt=false, skip the compensating replay (#6429, #5561, #5481).
// Ties count as stale: keeping a prompt one extra round is recoverable, while
// clearing a live prompt is the bug this guards against.
export function runtimeSnapshotPredatesPrompt(
  state: { approval?: unknown; ask?: unknown; promptArrivedAt?: number } | undefined,
  snapshotAt: number | undefined,
): boolean {
  if (!state || (!state.approval && !state.ask)) return false;
  if (snapshotAt === undefined || state.promptArrivedAt === undefined) return false;
  return snapshotAt <= state.promptArrivedAt;
}

function runtimeSnapshotPredatesRetry(
  state: Pick<State, "retry"> | undefined,
  snapshotAt: number | undefined,
): boolean {
  if (snapshotAt === undefined || state?.retry?.observedAt === undefined) return false;
  return snapshotAt <= state.retry.observedAt;
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
    runtime: tab.runtime,
    startupErr: tab.startupErr,
    eventChannel: existing?.eventChannel ?? "agent:event",
    cwd,
    workspaceRoot: tab.workspaceRoot || existing?.workspaceRoot || cwd,
    workspaceName: tab.workspaceName || existing?.workspaceName,
    workspacePath: tab.workspacePath || tab.workspaceRoot || existing?.workspacePath,
    sessionPath: tab.sessionPath !== undefined ? tab.sessionPath : existing?.sessionPath,
    sessionRevision: tab.sessionRevision !== undefined ? tab.sessionRevision : existing?.sessionRevision,
    sessionDigest: tab.sessionDigest !== undefined ? tab.sessionDigest : existing?.sessionDigest,
    gitBranch: tab.gitBranch || existing?.gitBranch,
    autoApproveTools,
    bypass: autoApproveTools,
    collaborationMode: tab.collaborationMode ?? existing?.collaborationMode ?? "normal",
    toolApprovalMode,

    tokenMode: tab.tokenMode ?? existing?.tokenMode ?? "full",
    goal: tab.goal ?? existing?.goal,
    goalStatus: tab.goalStatus ?? existing?.goalStatus,
    canonicalTodos: existing?.canonicalTodos,
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
    a.runtime?.phase === b.runtime?.phase &&
    a.runtime?.epoch === b.runtime?.epoch &&
    a.runtime?.issue?.code === b.runtime?.issue?.code &&
    a.runtime?.issue?.message === b.runtime?.issue?.message &&
    a.runtime?.issue?.retryable === b.runtime?.issue?.retryable &&
    a.runtime?.issue?.holderPid === b.runtime?.issue?.holderPid &&
    a.runtime?.issue?.holderHost === b.runtime?.issue?.holderHost &&
    a.runtime?.issue?.acquiredAt === b.runtime?.issue?.acquiredAt &&
    a.startupErr === b.startupErr &&
    a.eventChannel === b.eventChannel &&
    a.cwd === b.cwd &&
    a.workspaceRoot === b.workspaceRoot &&
    a.workspaceName === b.workspaceName &&
    a.workspacePath === b.workspacePath &&
    a.sessionPath === b.sessionPath &&
    a.sessionRevision === b.sessionRevision &&
    a.sessionDigest === b.sessionDigest &&
    a.gitBranch === b.gitBranch &&
    a.imageInputEnabled === b.imageInputEnabled &&
    a.autoApproveTools === b.autoApproveTools &&
    a.bypass === b.bypass &&
    a.collaborationMode === b.collaborationMode &&
    a.toolApprovalMode === b.toolApprovalMode &&

    a.tokenMode === b.tokenMode &&
    a.goal === b.goal &&
    a.goalStatus === b.goalStatus &&
    sameTodoList(a.canonicalTodos, b.canonicalTodos)
  );
}

export function runtimeReadyForSubmit(meta?: Meta): boolean {
  if (!meta || meta.ready !== true || meta.startupErr) return false;
  return !meta.runtime || meta.runtime.phase === "ready";
}

// normalizeTurnSubmit is the final frontend boundary before optimistic
// transcript state is created. Display text may intentionally be shorter than
// the provider input, but a visible-only message must never start an empty model
// turn (#6869).
export function normalizeTurnSubmit(displayText: string, submitText: string): {
  display: string;
  submit: string;
} {
  const display = displayText.trim();
  const submit = submitText.trim();
  if (!submit) throw new Error("Message cannot be empty.");
  return { display, submit };
}

const frontendSubmissionEpoch = typeof globalThis.crypto?.randomUUID === "function"
  ? globalThis.crypto.randomUUID()
  : `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
export function createTurnSubmissionId(tabId: string, sessionGen: number, seq: number, runtimeEpoch?: string): string {
  return JSON.stringify([frontendSubmissionEpoch, tabId, sessionGen, runtimeEpoch ?? "", seq]);
}

export function acceptsRuntimeEventEpoch(acceptedEpoch: string | undefined, eventEpoch: string | undefined): boolean {
  return !eventEpoch || !acceptedEpoch || acceptedEpoch === eventEpoch;
}

export function composerProfileApplicationKey(
  runtimeEpoch: string | undefined,
  collaborationMode: CollaborationMode,
  toolApprovalMode: ToolApprovalMode,
  goal: string,
): string {
  return JSON.stringify([runtimeEpoch ?? "", collaborationMode, toolApprovalMode, goal]);
}

function metaWithoutCanonicalTodos(meta?: Meta): Meta | undefined {
  if (!meta || meta.canonicalTodos === undefined) return meta;
  return { ...meta, canonicalTodos: undefined };
}

const STALE_TURN_RECONCILE_MS = 30_000;
const CANCEL_RECONCILE_DELAYS_MS = [0, 100, 300, 1_000] as const;
// After a stale runtime snapshot is rejected (its fetch predates the live
// prompt), refetch authoritative backend state once. Short enough to be barely
// perceptible, long enough to let any other in-flight replay events land first
// so the refetch reflects settled backend truth (#6432).
const STALE_PROMPT_RECONCILE_MS = 150;
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

function hasReusableCachedTranscript(state: State | undefined, sessionPath?: string, revision?: number, digest?: string): boolean {
  if (!state || state.items.length === 0) return false;
  const expectedSessionPath = (sessionPath ?? "").trim();
  if (!expectedSessionPath) return true;
  if ((state.meta?.sessionPath ?? "").trim() !== expectedSessionPath) return false;
  if (typeof revision === "number" && revision > 0) {
    return state.historyRevision === revision && (digest ?? "") === (state.historyDigest ?? "");
  }
  if ((digest ?? "").trim() !== "") return state.historyDigest === digest;
  // A cached page with a known fingerprint must not be reused when the
  // backend temporarily cannot provide one (for example while its metadata
  // sidecar is being atomically replaced). Reloading is the only way to avoid
  // presenting a stale persisted transcript as current.
  return state.historyRevision === undefined && !state.historyDigest;
}

function historyFingerprintMatchesMeta(history: { revision: number; revisionKnown?: boolean; digest?: string }, meta: Meta): boolean {
  const expectedDigest = (meta.sessionDigest ?? "").trim();
  if (expectedDigest && history.digest !== expectedDigest) return false;
  const expectedRevision = meta.sessionRevision ?? 0;
  if (expectedRevision > 0 && (!history.revisionKnown || history.revision !== expectedRevision)) return false;
  return true;
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
  | { type: "stream_batch"; segments: StreamSegment[] }
  | { type: "user"; text: string; submitText?: string; seq: number; submissionId: string; deliveryRecovery?: boolean }
  | { type: "unsend" }
  | { type: "send_confirmed"; submissionId: string }
  | { type: "send_failed"; submissionId: string; error: string }
  | { type: "backend_status"; running: boolean; pendingPrompt?: boolean; backgroundJobs?: number; cancelRequested?: boolean; cancellable?: boolean; snapshotAt?: number }
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
  // TranscriptStore-driven history actions (windowed HistorySliceForTab flow).
  // Items carry stable entryId-derived ids; prepend also lists existing item
  // ids superseded by cross-page tool call/result merges.
  | { type: "history_replace"; items: Item[]; startTurn: number; totalTurns: number; hasOlder: boolean; revision?: number; digest?: string }
  | { type: "history_prepend"; items: Item[]; removeIds: string[]; startTurn: number; totalTurns: number; hasOlder: boolean; revision?: number; digest?: string }
  | { type: "history_items_patch"; patches: Record<string, Item> }
  | { type: "history_older_start" }
  | { type: "history_older_error" }
  | { type: "local_notice"; level: "info" | "warn"; text: string }
  | { type: "clearApproval" }
  | { type: "clearAsk" }
  | { type: "clearExtensionForm" }
  | { type: "extension_notifications_drained" }
  | { type: "approval_drained"; ids: string[]; epoch: number }
  | { type: "submit_prompt_failed"; id: string; epoch: number }
  | { type: "controller_rebuilt" }
  | { type: "reset" }
  | { type: "context_panel_refresh" };

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
      if (m.content.trim() !== "" || m.decisionReceipt) {
        const next = appendNoticeItem(items, seq, `${idPrefix}${seq}`, m.level === "warn" ? "warn" : "info", m.content, m.detail, m.code, m.decisionReceipt);
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
          readOnly: typeof tc.resolvedReadOnly === "boolean" ? tc.resolvedReadOnly : isReadOnlyTool(tc.name),
          resolvedName: tc.resolvedName,
          capabilityId: tc.capabilityId,
          status: result ? (error ? "error" : "done") : "stopped",
          output,
          error,
          dataArchived: archived || undefined,
          subject: tc.subject,
          summary: summarizeFileDiff(fileDiff) || tc.summary,
          fileDiff,
          isShell: tc.name === "bash" || (tc.id || "").startsWith("shell-"),
          execution: result?.execution,
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
        isShell: (m.toolName || "") === "bash" || (m.toolCallId || "").startsWith("shell-"),
        execution: m.execution,
      });
      seq++;
      continue;
    }
  }
  return { items, seq };
}

function applyTurnCheckpoint(items: Item[], submissionId: string | undefined, turn: number | undefined): Item[] {
  if (!submissionId) return items;
  const validTurn = turn !== undefined && Number.isInteger(turn) && turn >= 0;
  let changed = false;
  const next = items.map((item) => {
    if (item.kind !== "user" || item.submissionId !== submissionId) return item;
    changed = true;
    return { ...item, submissionId: undefined, checkpointTurn: item.checkpointTurn ?? (validTurn ? turn : undefined) };
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

export function historyToolError(output: string): string | undefined {
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

// applyDeltaSegments folds ordered stream segments into the assistant's live
// stream in one state transition. Assumes applyEvent's preamble already ran.
function applyDeltaSegments(s: State, segments: StreamSegment[]): State {
  const { items, id, seq } = ensureAssistant(s);
  const base = s.live?.id === id ? s.live : { id, text: "", reasoning: "", reasoningComplete: false };
  const now = Date.now();
  const deltaChars = segments.reduce((total, segment) => total + segment.delta.length, 0);
  const next = { ...s, items, live: applyLiveSegments(base, segments, now), currentAssistant: id, seq, turnOutputChars: s.turnOutputChars + deltaChars };
  return deltaChars > 0 ? beginTurnModelActivity(next, now) : next;
}

// applyStreamBatch is the stream_batch action: one frame's deltas, one reducer
// pass, one notification. Mirrors applyEvent's preamble for delta events.
function applyStreamBatch(s: State, segments: StreamSegment[]): State {
  if (s.discardTurn) return s;
  if (s.retry) s = { ...s, retry: undefined };
  return applyDeltaSegments(s, segments);
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

function resetTurnTiming(now = Date.now()): Pick<State, "turnStartAt" | "turnDoneAt" | "turnWaitAccumMs" | "promptWaitStartedAt" | "turnTokens" | "turnTotalTokens" | "turnOutputTokens" | "turnOutputChars" | "turnOutputCharsAtUsage" | "turnOutputEstimated" | "turnModelActiveAt" | "turnModelActiveMs" | "turnCost" | "turnArgChars" | "pendingRequestModelMs"> {
  return {
    turnStartAt: now,
    turnDoneAt: 0,
    turnWaitAccumMs: 0,
    promptWaitStartedAt: undefined,
    turnTokens: 0,
    turnTotalTokens: 0,
    turnOutputTokens: 0,
    turnOutputChars: 0,
    turnOutputCharsAtUsage: 0,
    turnOutputEstimated: false,
    turnModelActiveAt: undefined,
    turnModelActiveMs: 0, pendingRequestModelMs: undefined,
    turnCost: 0,
    turnArgChars: 0,
  };
}

function confirmPendingUser(s: State, submissionId: string | undefined): State {
  if (!submissionId || s.pendingSubmissionId !== submissionId) return s;
  return { ...s, pendingUser: undefined, pendingSubmissionId: undefined };
}

function beginTurnModelActivity(s: State, now = Date.now()): State {
  return s.turnModelActiveAt && s.turnModelActiveAt > 0
    ? s
    : { ...s, turnModelActiveAt: now };
}

function endTurnModelActivity(s: State, now = Date.now(), stashForUsage = false): State {
  if (!s.turnModelActiveAt || s.turnModelActiveAt <= 0) return s;
  const closedMs = Math.max(0, now - s.turnModelActiveAt);
  return { ...s, turnModelActiveAt: undefined, turnModelActiveMs: Math.max(0, s.turnModelActiveMs) + closedMs,
    pendingRequestModelMs: stashForUsage ? closedMs : s.pendingRequestModelMs };
}

function snapshotCompletedTurnTelemetry(s: State, now = Date.now()): State {
  const settled = endTurnModelActivity(s, now);
  const liveChars = (settled.live?.text.length ?? 0) + (settled.live?.reasoning.length ?? 0);
  const inFlightChars = settled.turnOutputTokens > 0
    ? Math.max(0, liveChars - settled.turnOutputCharsAtUsage) + settled.turnArgChars
    : settled.turnOutputChars + settled.turnArgChars;
  const estimatedInFlightTokens = Math.round(inFlightChars / 4);
  return {
    ...settled,
    turnDoneAt: now,
    lastTurnOutputTokens: settled.turnOutputTokens + estimatedInFlightTokens,
    lastTurnStartAt: settled.turnStartAt,
    lastTurnDoneAt: now,
    lastTurnWaitAccumMs: settled.turnWaitAccumMs,
    lastTurnModelMs: settled.turnModelActiveMs,
    lastTurnOutputEstimated: settled.turnOutputEstimated || estimatedInFlightTokens > 0 || (settled.turnOutputTokens === 0 && settled.turnOutputChars > 0),
  };
}

// applyExtensionSurfaceEvent reduces one extension_surface / extension_status
// wire event. Every publication passes the per-surface generation fence first
// (withAcceptedExtensionGeneration); the per-tab runtime-epoch fence in the
// onEvent handler has already dropped anything from an older runtime
// generation.
function applyExtensionSurfaceEvent(s: State, surface: WireExtensionSurface | undefined): State {
  if (!surface) return s;
  const gated = withAcceptedExtensionGeneration(s, surface);
  if (gated === null) return s;
  s = gated;
  const kind = surface.kind || (surface.status ? "status" : "");
  switch (kind) {
    case "status":
      return applyExtensionStatus(s, surface);
    case "card":
      return applyExtensionCard(s, surface);
    case "form":
      return applyExtensionForm(s, surface);
    case "notification":
      return applyExtensionNotification(s, surface);
    default:
      return s;
  }
}

// withAcceptedExtensionGeneration applies the per-surface generation fence.
// Returns null when the event is a stale re-ordering and must be dropped;
// otherwise returns state with the accepted generation recorded.
function withAcceptedExtensionGeneration(s: State, surface: WireExtensionSurface): State | null {
  const key = extensionSurfaceKey(surface);
  if (!acceptsExtensionGeneration(s.extensionGenerations[key], surface.generation)) return null;
  if (surface.generation === undefined || s.extensionGenerations[key] === surface.generation) return s;
  return { ...s, extensionGenerations: { ...s.extensionGenerations, [key]: surface.generation } };
}

function applyExtensionStatus(s: State, surface: WireExtensionSurface): State {
  const status: WireExtensionStatus | undefined = surface.status;
  if (!status) return s;
  const entry: ExtensionStatusEntry = {
    pluginId: surface.pluginId,
    surfaceId: surface.surfaceId,
    label: status.label,
    detail: status.detail,
    severity: status.severity,
    progress: status.progress,
    generation: surface.generation,
  };
  return { ...s, extensionStatuses: { ...s.extensionStatuses, [extensionSurfaceKey(surface)]: entry } };
}

function applyExtensionCard(s: State, surface: WireExtensionSurface): State {
  const card: WireExtensionCard | undefined = surface.card;
  if (!card) return s;
  const key = extensionSurfaceKey(surface);
  const idx = s.items.findIndex((it) => it.kind === "extension" && it.surfaceKey === key);
  if (idx >= 0) {
    const next = [...s.items];
    const prev = next[idx];
    if (prev.kind === "extension") next[idx] = { ...prev, generation: surface.generation, card };
    return { ...s, items: next };
  }
  return {
    ...s,
    seq: s.seq + 1,
    items: [
      ...s.items,
      { kind: "extension", id: `x${s.seq}`, surfaceKey: key, pluginId: surface.pluginId, surfaceId: surface.surfaceId, generation: surface.generation, card },
    ],
  };
}

function applyExtensionForm(s: State, surface: WireExtensionSurface): State {
  const form = surface.form;
  if (!form) return s;
  return {
    ...s,
    extensionForm: { pluginId: surface.pluginId, surfaceId: surface.surfaceId, generation: surface.generation, form },
  };
}

function applyStreamAttempt(s: State, e: WireEvent): State {
  const sa = e.streamAttempt;
  if (!sa?.id || !sa.action) return s;
  switch (sa.action) {
    case "begin": {
      // Snapshot only what this attempt may replace in the visible stream.
      // Provider activity timing is closed at discard but remains accumulated so
      // retry backoff is not counted in the completed TPS denominator.
      const baselineLive = s.live
        ? {
            id: s.live.id,
            text: s.live.text,
            reasoning: s.live.reasoning,
            reasoningComplete: s.live.reasoningComplete,
            reasoningStartedAt: s.live.reasoningStartedAt,
            reasoningCompletedAt: s.live.reasoningCompletedAt,
          }
        : undefined;
      return {
        ...s,
        running: true,
        turnActive: true,
        cancellable: true,
        turnStartAt: s.turnStartAt || Date.now(),
        streamAttemptJournal: {
          id: sa.id,
          baselineLive,
          baselineTurnArgChars: s.turnArgChars,
          createdToolIds: [],
          priorTools: {},
        },
      };
    }
    case "discard": {
      const journal = s.streamAttemptJournal;
      if (!journal || journal.id !== sa.id) {
        // Stale/out-of-order discard for an older attempt — leave the current
        // journal (and live speculative UI) untouched.
        return s;
      }
      const remove = new Set(journal.createdToolIds);
      const items = s.items
        .filter((it) => !(it.kind === "tool" && remove.has(it.id)))
        .map((it) => {
          if (it.kind !== "tool") return it;
          const prior = journal.priorTools[it.id];
          return prior ? { ...prior } : it;
        });
      // Restore live to the pre-attempt snapshot so partial text/reasoning is
      // replaced, not concatenated with the next attempt.
      const live = journal.baselineLive
        ? { ...journal.baselineLive }
        : s.live
          ? { ...s.live, text: "", reasoning: "", reasoningComplete: false, reasoningStartedAt: undefined, reasoningCompletedAt: undefined }
          : undefined;
      return {
        ...endTurnModelActivity(s),
        items,
        live,
        turnArgChars: journal.baselineTurnArgChars,
        streamAttemptJournal: undefined,
        running: true,
        turnActive: true,
        cancellable: true,
      };
    }
    case "commit": {
      // Commit clears bookkeeping only; subsequent tool_dispatch/result are real.
      if (s.streamAttemptJournal && s.streamAttemptJournal.id !== sa.id) {
        return s;
      }
      return { ...s, streamAttemptJournal: undefined };
    }
    default:
      return s;
  }
}

/** Record a tool card mutation against the active sampling-attempt journal.
 * Only parent-sampling partials with a matching attemptId are journaled —
 * background sub-agent tools (parentId) and committed full dispatches are not.
 */
function noteToolInJournal(
  s: State,
  toolId: string,
  existedBefore: boolean,
  prior: Extract<Item, { kind: "tool" }> | undefined,
  meta?: { attemptId?: string; parentId?: string; partial?: boolean },
): State {
  const journal = s.streamAttemptJournal;
  if (!journal || !toolId) return s;
  // Require explicit attempt membership — do not journal by arrival time alone.
  if (!meta?.attemptId || meta.attemptId !== journal.id) return s;
  if (meta.parentId) return s;
  if (meta.partial === false) return s;
  if (!existedBefore) {
    if (journal.createdToolIds.includes(toolId)) return s;
    return {
      ...s,
      streamAttemptJournal: {
        ...journal,
        createdToolIds: [...journal.createdToolIds, toolId],
      },
    };
  }
  if (prior && !journal.priorTools[toolId] && !journal.createdToolIds.includes(toolId)) {
    return {
      ...s,
      streamAttemptJournal: {
        ...journal,
        priorTools: { ...journal.priorTools, [toolId]: { ...prior } },
      },
    };
  }
  return s;
}

function applyExtensionNotification(s: State, surface: WireExtensionSurface): State {
  const notification = surface.notification;
  if (!notification) return s;
  const entry: ExtensionNotificationEntry = {
    id: `xn${s.seq}`,
    pluginId: surface.pluginId,
    title: notification.title,
    body: notification.body,
    severity: notification.severity,
  };
  return { ...s, seq: s.seq + 1, extensionNotifications: [...s.extensionNotifications, entry] };
}

function applyEvent(s: State, e: WireEvent): State {
  if (s.discardTurn) {
    if (e.kind === "turn_done") {
      return {
        ...s,
        items: applyTurnCheckpoint(s.items, e.submissionId, e.checkpointTurn),
        discardTurn: false,
        running: false,
        turnActive: false,
        pendingPrompt: false,
        cancelRequested: false,
        cancellable: false,
        currentAssistant: undefined,
        live: undefined,
      };
    }
    return s;
  }
  s = confirmPendingUser(s, e.submissionId);
  if (e.kind === "mcp_surface_ready") {
    // Background readiness remains a no-op unless the sink explicitly
    // correlates it to this submit in the common preamble above.
    return s;
  }
  if (e.kind === "extension_surface" || e.kind === "extension_status") {
    // Sidecar publications without exact sink correlation remain background-
    // only and must not clear the retry indicator.
    return applyExtensionSurfaceEvent(s, e.extension);
  }
  if (e.kind === "retrying") {
    // Retrying is emitted synchronously from inside the foreground provider
    // request, immediately before its cancellation-aware backoff. Treat it as
    // authoritative proof that the turn is still active. An idle ListTabs
    // snapshot fetched before this event can otherwise clear `running`,
    // regardless of which one reaches the reducer first, and leave the
    // composer showing "retrying (n/m)" without its Stop button (or Escape
    // cancellation) until all retries are exhausted.
    return {
      ...s,
      retry: {
        attempt: e.retryAttempt ?? 0,
        max: e.retryMax ?? 0,
        observedAt: promptEventClock(),
      },
      running: true,
      turnActive: true,
      cancellable: true,
      turnStartAt: s.turnStartAt || Date.now(),
    };
  }
  if (e.kind === "stream_attempt") {
    return applyStreamAttempt(s, e);
  }
  if (s.retry) s = { ...s, retry: undefined };
  switch (e.kind) {
    case "turn_started": {
      // Pre-create an empty assistant bubble
      // immediately so the user sees their message + a blinking cursor the
      // instant the backend acknowledges the turn — no dead gap waiting for
      // the first text/reasoning token.
      const { items, id, seq } = ensureAssistant(s);
      return {
        ...s,
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
      return applyDeltaSegments(s, [{ kind: e.kind, delta: e.text ?? e.reasoning ?? "" }]);
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
        return { ...endTurnModelActivity(s, Date.now(), true), items, live: undefined, currentAssistant: undefined, turnOutputCharsAtUsage: 0 };
      }
      const now = Date.now();
      const settled = endTurnModelActivity(s, now, true);
      const { items, id, seq } = ensureAssistant(settled);
      const streamedChars = settled.live?.id === id ? settled.live.text.length + settled.live.reasoning.length : 0;
      const turnOutputChars = Math.max(0, settled.turnOutputChars - streamedChars + text.length + reasoning.length);
      const completedLive = settled.live?.id === id ? completeLiveReasoning({ ...settled.live, text, reasoning }, now) : undefined;
      const reasoningDurationMs = liveReasoningDurationMs(completedLive);
      const workDurationMs = currentTurnDurationMs(settled, now);
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
      return { ...settled, items: next, live: undefined, currentAssistant: undefined, turnOutputChars, turnOutputCharsAtUsage: 0, seq };
    }
    case "tool_dispatch": {
      const t = e.tool;
      if (!t) return s;
      // A partial dispatch (args still streaming from the model) upserts a
      // lightweight "receiving" card immediately. Dropping it entirely — the
      // old behavior — left a 30KB write_file body streaming for a minute with
      // zero visible activity, indistinguishable from a hang. The full
      // dispatch that follows merges by ID and fills in args/summary.
      if (t.partial) {
        const activeState = t.parentId ? s : beginTurnModelActivity(s);
        const turnArgChars = t.argChars && t.argChars > 0 ? t.argChars : s.turnArgChars;
        // Some OpenAI-compatible streams surface the call name before its ID.
        // Without a stable ID the card could never be merged with the full
        // dispatch (a synthetic `tool${seq}` id would orphan it as a forever-
        // running duplicate), so count the progress but wait for the ID before
        // creating the card.
        if (!t.id) return { ...activeState, turnArgChars };
        const id = t.id;
        const idx = activeState.items.findIndex((it) => it.kind === "tool" && it.id === id);
        if (idx >= 0) {
          const next = [...activeState.items];
          const it = next[idx];
          if (it.kind === "tool" && it.status === "running" && !it.args) {
            const prior = it;
            next[idx] = { ...it, argChars: t.argChars || it.argChars };
            return noteToolInJournal({ ...activeState, items: next, turnArgChars }, id, true, prior, {
              attemptId: t.attemptId, parentId: t.parentId, partial: true,
            });
          }
          return { ...activeState, turnArgChars };
        }
        return noteToolInJournal({
          ...activeState,
          turnArgChars,
          seq: activeState.seq + 1,
          items: [...activeState.items, { kind: "tool", id, name: t.name, args: "", readOnly: t.readOnly, resolvedName: t.resolvedName, capabilityId: t.capabilityId, status: "running", argChars: t.argChars || undefined, parentId: t.parentId, subagentProgress: SUBAGENT_PROGRESS_TOOLS.has(t.name) ? freshSubagentProgress() : undefined }],
        }, id, false, undefined, { attemptId: t.attemptId, parentId: t.parentId, partial: true });
      }
      const settled = t.parentId ? s : endTurnModelActivity(s, Date.now(), true);
      const id = t.id || `tool${s.seq}`;
      const idx = settled.items.findIndex((it) => it.kind === "tool" && it.id === id);
      if (idx >= 0) {
        const next = [...settled.items];
        const it = next[idx];
        if (it.kind === "tool") {
          const args = t.args ? t.args : it.args;
          const fileDiff = fileDiffFromWire(t);
          const summary = summarizeFileDiff(fileDiff) || summarize(t.name, args) || (t.name === it.name && args === it.args ? it.summary : undefined);
          next[idx] = { ...it, name: t.name, args, readOnly: t.readOnly, resolvedName: t.resolvedName ?? it.resolvedName, capabilityId: t.capabilityId ?? it.capabilityId, profile: t.profile ?? it.profile, summary, fileDiff, argChars: undefined, isShell: it.isShell || t.name === "bash" || id.startsWith("shell-"), execution: t.execution ?? it.execution, subagentProgress: it.subagentProgress ?? (SUBAGENT_PROGRESS_TOOLS.has(t.name) ? freshSubagentProgress() : undefined) };
        }
        if (t.parentId) touchSubagentParent(next, t.parentId);
        return { ...settled, items: next };
      }
      const args = t.args ?? "";
      const fileDiff = fileDiffFromWire(t);
      const created: ToolItem = { kind: "tool", id, name: t.name, args, readOnly: t.readOnly, resolvedName: t.resolvedName, capabilityId: t.capabilityId, status: "running", summary: summarizeFileDiff(fileDiff) || summarize(t.name, args), fileDiff, isShell: t.name === "bash" || id.startsWith("shell-"), execution: t.execution, parentId: t.parentId, profile: t.profile, subagentProgress: SUBAGENT_PROGRESS_TOOLS.has(t.name) ? freshSubagentProgress() : undefined };
      const items = [...settled.items, created];
      // A sub-agent call nested under a task card refreshes that card's
      // recent activity and switches its phase to "tool".
      if (t.parentId) touchSubagentParent(items, t.parentId);
      return { ...settled, seq: settled.seq + 1, items };
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
          let status: ToolStatus = t.err ? "error" : "done";
          if (existing.subagentProgress) {
            // Sub-agent progress owns the card's final visual: a background
            // call that returned a job id stays running while the child
            // works; a cancelled child keeps its stopped semantics even when
            // the aggregate result carries an error. Group cards
            // (parallel_tasks/fleet) settle only from their own lifecycle
            // terminal event — the backend emits running at start and exactly
            // one terminal at the end (including validation failures and
            // zero-child cancellation) — never from inferring the children
            // observed so far, since a background group's children dispatch
            // asynchronously and a fast child can finish before later ones
            // even appear.
            if (isGroupSubagentTool(existing.name)) {
              status = isTerminalSubagentPhase(existing.subagentProgress.phase)
                ? terminalStatusOf(existing.subagentProgress.phase)
                : "running";
            } else if (!isTerminalSubagentPhase(existing.subagentProgress.phase)) {
              status = "running";
            } else {
              status = terminalStatusOf(existing.subagentProgress.phase);
            }
          }
          next[idx] = {
            ...existing,
            readOnly: t.readOnly,
            resolvedName: t.resolvedName ?? existing.resolvedName,
            capabilityId: t.capabilityId ?? existing.capabilityId,
            status,
            output: t.output,
            error: t.err,
            truncated: t.truncated,
            durationMs: t.durationMs,
            summary,
            isShell: existing.isShell || existing.name === "bash" || t.name === "bash",
            execution: t.execution ?? existing.execution,
          };
        }
      }
      // A nested result refreshes its sub-agent parent's recent activity.
      if (t.parentId) touchSubagentParent(next, t.parentId);
      return { ...s, items: compactArchivedToolItems(next) };
    }
    case "tool_progress": {
      const t = e.tool;
      if (!t?.id) return s;
      // Reserved sub-agent progress channels update the card's in-memory
      // preview; they never touch tool.output or the parent's live stream.
      if (isSubagentProgressName(t.name)) {
        return applySubagentProgress(s, t);
      }
      const idx = s.items.findIndex((it) => it.kind === "tool" && it.id === t.id);
      if (idx < 0) return s;
      const next = [...s.items];
      const it = next[idx];
      if (it.kind === "tool") next[idx] = { ...it, output: (it.output ?? "") + (t.output ?? "") };
      // Streaming output of a sub-agent's real tool refreshes its card.
      if (t.parentId) touchSubagentParent(next, t.parentId);
      return { ...s, items: next };
    }
    case "usage": {
      if (!countsTowardCurrentTurn(s)) return s;
      const updateContextGauge = updatesContextGauge(e.usage);
      // Only executor usage belongs to the foreground model stream. Planner,
      // subagent, and auxiliary usage still contributes to session totals and
      // usageSeq, but must not close or inflate the executor TPS interval.
      const settled = updateContextGauge ? endTurnModelActivity(s, Date.now(), true) : s;
      const hasRequestCompletion = (e.usage?.contextCompletionTokens ?? 0) > 0;
      const requestModelMs = updateContextGauge ? (settled.pendingRequestModelMs ?? 0) : 0;
      const requestTokens = updateContextGauge ? (hasRequestCompletion ? (e.usage?.contextCompletionTokens ?? 0) : (e.usage?.completionTokens ?? 0)) : 0;
      const lastRequestTps = updateContextGauge ? (requestTokens > 0 && requestModelMs >= 500 ? requestTokens / (requestModelMs / 1000) : null) : s.lastRequestTps;
      // Context* is the latest sampling attempt; other token fields are billable aggregates.
      let used = settled.context.used;
      if (e.usage && settled.context.window && updateContextGauge) used = (e.usage.contextPromptTokens ?? 0) > 0
        ? (e.usage.contextPromptTokens ?? 0)
        : (e.usage.promptTokens ?? 0);
      const turnTokens = settled.turnTokens + (e.usage?.completionTokens ?? 0);
      const turnOutputTokens = updateContextGauge
        ? settled.turnOutputTokens + (e.usage?.completionTokens ?? 0)
        : settled.turnOutputTokens;
      const turnOutputCharsAtUsage = updateContextGauge
        ? (settled.live?.text.length ?? 0) + (settled.live?.reasoning.length ?? 0)
        : settled.turnOutputCharsAtUsage;
      const turnOutputEstimated = updateContextGauge
        ? settled.turnOutputEstimated || Boolean(e.usage?.estimated)
        : settled.turnOutputEstimated;
      const usageTokens = usageTotalTokens(e.usage);
      const turnTotalTokens = settled.turnTotalTokens + usageTokens;
      const sessionTokens = settled.sessionTokens + usageTokens;
      const usageCost = e.usage?.cost ?? e.usage?.costUsd ?? 0;
      const turnCost = settled.turnCost + usageCost;
      const sessionCost = settled.sessionCost + usageCost;
      const sessionCurrency = e.usage?.currency || settled.sessionCurrency || "¥";
      const usage = updateContextGauge ? e.usage : settled.usage;
      // The completed round's usage now accounts for the streamed tool-call
      // arguments, so drop the live estimate rather than double-count it.
      return { ...settled, usage, context: { ...settled.context, used, sessionTokens }, turnTokens, turnOutputTokens, turnOutputCharsAtUsage, turnOutputEstimated, turnTotalTokens, turnCost, turnArgChars: updateContextGauge ? 0 : settled.turnArgChars, sessionTokens, sessionCost, sessionCurrency, usageSeq: settled.usageSeq + 1, lastRequestTps, pendingRequestModelMs: updateContextGauge ? undefined : settled.pendingRequestModelMs };
    }
    case "notice":
      return appendNoticeToState(s, e.level ?? "info", e.text ?? "", e.detail, e.code, e.decisionReceipt);
    case "context_maintenance": {
      const m = e.maintenance;
      if (!m || m.status === "noop") return s;
      if (!isNewMaintenanceOperation(s.seenMaintenanceOps, m.operationId)) return s;
      const next = appendNoticeToState(s, m.status === "failed" ? "warn" : "info", formatContextMaintenanceNotice(m, t), m.reason);
      return { ...next, seenMaintenanceOps: rememberMaintenanceOperation(s.seenMaintenanceOps, m.operationId) };
    }
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
      return { ...s, seq: s.seq + 1, items: [...s.items, { kind: "notice", id: `s${s.seq}`, level: "info", text: `${STEER_NOTICE_PREFIX}${e.text ?? ""}` }] };
    case "approval_request": {
      if (s.cancelRequested) return s;
      // A delayed re-delivery of a prompt the user already answered locally
      // (clearApproval) must not resurrect it — no downstream snapshot is
      // guaranteed to ever reject it again (#6432 round 2).
      if (e.approval?.id !== undefined && e.approval.id === s.resolvedPromptId) return s;
      return beginPromptWait({
        ...s,
        approval: e.approval,
        // A replay of the SAME prompt (post-answer delayed delivery, or the
        // #6429 re-arm after activation) keeps the original arrival time; only
        // a genuinely new prompt id re-anchors it (#6432 reverse race).
        promptArrivedAt: e.approval?.id === s.promptArrivedId ? s.promptArrivedAt : promptEventClock(),
        promptArrivedId: e.approval?.id,
        pendingPrompt: true,
        running: true,
        turnActive: true,
        cancellable: true,
      });
    }
    case "ask_request": {
      if (s.cancelRequested) return s;
      if (e.ask?.id !== undefined && e.ask.id === s.resolvedPromptId) return s;
      return beginPromptWait({
        ...s,
        ask: e.ask,
        promptArrivedAt: e.ask?.id === s.promptArrivedId ? s.promptArrivedAt : promptEventClock(),
        promptArrivedId: e.ask?.id,
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
      const now = Date.now();
      s = snapshotCompletedTurnTelemetry(s, now);
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
      let items: Item[] = s.deliveryRecoveryActive && !e.err
        ? finalized.filter((item) => item.kind !== "notice" || item.variant !== "delivery")
        : finalized;
      if (e.outcome === "final_readiness") {
        const previous = items.map((item) => item.kind === "notice" && item.variant === "delivery"
          ? { ...item, action: undefined }
          : item);
        items = [...previous, {
          kind: "notice",
          id: `e${s.seq}`,
          level: "info",
          variant: "delivery",
          title: t("notice.deliveryIncompleteTitle"),
          text: t("notice.deliveryIncompleteBody"),
          detail: deliveryReadinessDetail(e.readiness, e.err),
          action: "continue_delivery",
        }];
      } else if (e.outcome === "recovery_paused") {
        // Informational pause — not a send failure. Composer is immediately free.
        items = [...finalized, {
          kind: "notice",
          id: `e${s.seq}`,
          level: "info",
          title: t("notice.recoveryPausedTitle"),
          text: t("notice.recoveryPausedBody"),
        }];
      } else if (e.err) {
        items = [...finalized, { kind: "notice", id: `e${s.seq}`, level: "warn", text: e.err }];
      }
      // Plan approval can arrive before turn_done on some Wails event paths.
      // Keep that gate visible instead of clearing the only UI that can answer it.
      const keepPlanApproval = s.approval?.tool === "exit_plan_mode";
      let next: State = {
        ...s,
        items: applyTurnCheckpoint(items, e.submissionId, e.checkpointTurn),
        live: undefined,
        streamAttemptJournal: undefined,
        running: keepPlanApproval,
        turnActive: keepPlanApproval,
        pendingPrompt: keepPlanApproval,
        cancelRequested: false,
        cancellable: keepPlanApproval,
        currentAssistant: undefined,
        approval: keepPlanApproval ? s.approval : undefined,
        ask: undefined,
        deliveryRecoveryActive: false,
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
      const userItemId = `u${seq}`;
      return {
        ...s,
        seq: seq + 1,
        items: [...s.items, { kind: "user", id: userItemId, submissionId: a.submissionId, text: a.text, submitText: a.submitText, createdAt: Date.now() }],
        running: true,
        pendingPrompt: false,
        cancelRequested: false,
        cancellable: true,
        ...resetTurnTiming(),
        // New turn epoch: forget the previous prompt anchor so a genuinely new
        // prompt re-anchors freshly instead of inheriting a stale id/time.
        promptArrivedAt: undefined,
        promptArrivedId: undefined,
        pendingUser: a.text,
        pendingSubmissionId: a.submissionId,
        deliveryRecoveryActive: Boolean(a.deliveryRecovery),
        discardTurn: false,
      };
    }
    case "unsend": {
      const cleared = endPromptWait({
        ...s,
        pendingUser: undefined,
        pendingSubmissionId: undefined,
        discardTurn: true,
        running: false,
        pendingPrompt: false,
        cancelRequested: true,
        cancellable: false,
        approval: undefined,
        ask: undefined,
        promptArrivedAt: undefined,
        promptArrivedId: undefined,
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
        promptArrivedAt: undefined,
        promptArrivedId: undefined,
        cancellable: s.running || s.turnActive,
      });
    }
    case "send_confirmed": return confirmPendingUser(s, a.submissionId);
    case "send_failed": {
      if (s.pendingSubmissionId !== a.submissionId) return s;
      const idx = s.items.findIndex((it) => it.kind === "user" && it.submissionId === a.submissionId);
      const items = idx >= 0 ? s.items.map((it, i) => (i === idx ? { ...it, submissionId: undefined, failed: true } : it)) : s.items;
      const notice: Item = { kind: "notice", id: `n${s.seq}`, level: "warn", text: a.error };
      return { ...s, pendingUser: undefined, pendingSubmissionId: undefined, deliveryRecoveryActive: false, running: false, turnActive: false, pendingPrompt: false, cancelRequested: false, cancellable: false, live: undefined, seq: s.seq + 1, items: [...items, notice] };
    }
    case "backend_status": {
      // A snapshot fetched before the live approval/ask event arrived cannot
      // know about the prompt; everything it reports about the turn lifecycle
      // is equally stale. Ignore it and let an explicit answer/cancel or a
      // fresher snapshot settle the state (#6429).
      if (runtimeSnapshotPredatesPrompt(s, a.snapshotAt)) return s;
      const pendingPrompt = Boolean(a.pendingPrompt);
      const backgroundJobs = Math.max(0, a.backgroundJobs ?? s.backgroundJobs ?? 0);
      const cancelRequested = Boolean(a.cancelRequested);
      const foregroundRunning = foregroundRunningFromRuntimeMeta({ running: a.running, pendingPrompt, backgroundJobs, cancellable: a.cancellable });
      // A retry event is newer evidence of foreground activity than an idle
      // snapshot whose fetch started earlier. Keep the turn cancellable until
      // a snapshot started after the retry confirms that it is actually idle.
      if (!foregroundRunning && runtimeSnapshotPredatesRetry(s, a.snapshotAt)) return s;
      const cancellable = foregroundRunning;
      const clearsRetry = !foregroundRunning && s.retry !== undefined;
      if (
        foregroundRunning === s.running &&
        pendingPrompt === s.pendingPrompt &&
        backgroundJobs === s.backgroundJobs &&
        cancelRequested === s.cancelRequested &&
        cancellable === s.cancellable &&
        !clearsRetry
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
      const telemetry = snapshotCompletedTurnTelemetry(s);
      const finalized = telemetry.items.map((it) => {
        if (it.kind === "assistant" && telemetry.live && it.id === telemetry.live.id) return { ...it, text: telemetry.live.text, reasoning: telemetry.live.reasoning, streaming: false };
        if (it.kind === "assistant" && it.streaming) return { ...it, streaming: false };
        if (it.kind === "tool" && it.status === "running") return { ...it, status: "stopped" as const };
        return it;
      });
      return endPromptWait({
        ...telemetry,
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
        retry: undefined,
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
      // Mid-turn snapshot refreshes can race a rebuilt executor whose
      // LastUsage is still nil: the backend then reports used=0 for a session
      // that visibly holds tokens, and the gauge collapses to "0/1M" until the
      // next executor usage arrives. Keep the last known fill while a turn is
      // live; genuine resets flow through the "reset" action or land when the
      // session is idle.
      const context =
        a.context.used === 0 && s.context.used > 0 && (s.running || s.turnActive) && a.context.window === s.context.window
          ? { ...a.context, used: s.context.used }
          : a.context;
      return { ...s, context, sessionTokens, sessionCost, sessionCurrency };
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
      // New tab epoch: drop the prompt anchor so the post-activation replay
      // re-anchors against this activation, keeping the #6429 stale-snapshot
      // guard armed for the freshly restored prompt.
      promptArrivedAt: undefined,
      promptArrivedId: undefined,
      running: false,
      turnActive: false,
      cancellable: false,
    };
    case "backend_activation_done": return s.backendActivationPending ? { ...s, backendActivationPending: false } : s;
    case "message_action_start": return { ...s, messageAction: a.action };
    case "message_action_done": return { ...s, messageAction: undefined };
    case "history": {
      const { items, seq } = historyMessagesToItems(a.messages, "h", s.seq);
      return { ...s, items: compactArchivedToolItems(items), pendingSubmissionId: undefined, seq, hydrateHistoryLoaded: true, hydratePlaceholderItems: undefined, historyStartTurn: 0, historyTotalTurns: 0, historyHasOlder: false, historyOlderLoading: false, historyRevision: undefined, historyDigest: undefined };
    }
    case "history_page": {
      const { items, seq } = historyPageItems(a.page);
      const nextItems = a.mode === "prepend" ? [...items, ...s.items] : items;
      return {
        ...s,
        items: compactArchivedToolItems(nextItems),
        pendingSubmissionId: a.mode === "replace" ? undefined : s.pendingSubmissionId,
        seq: Math.max(s.seq, seq),
        hydrateHistoryLoaded: true,
        hydratePlaceholderItems: undefined,
        historyStartTurn: a.page.startTurn,
        historyTotalTurns: a.page.totalTurns,
        historyHasOlder: a.page.hasOlder,
        historyOlderLoading: false,
        historyRevision: a.page.revision,
        historyDigest: a.page.digest,
      };
    }
    case "history_older_start": return s.historyOlderLoading ? s : { ...s, historyOlderLoading: true };
    case "history_older_error": return s.historyOlderLoading ? { ...s, historyOlderLoading: false } : s;
    case "history_replace":
      return {
        ...s,
        items: compactArchivedToolItems(a.items),
        pendingSubmissionId: undefined,
        hydrateHistoryLoaded: true,
        hydratePlaceholderItems: undefined,
        historyStartTurn: a.startTurn,
        historyTotalTurns: a.totalTurns,
        historyHasOlder: a.hasOlder,
        historyOlderLoading: false,
        historyRevision: a.revision,
        historyDigest: a.digest,
      };
    case "history_prepend": {
      const remove = a.removeIds.length > 0 ? new Set(a.removeIds) : undefined;
      const rest = remove ? s.items.filter((item) => !remove.has(item.id)) : s.items;
      return {
        ...s,
        items: compactArchivedToolItems([...a.items, ...rest]),
        hydrateHistoryLoaded: true,
        hydratePlaceholderItems: undefined,
        historyStartTurn: a.startTurn,
        historyTotalTurns: a.totalTurns,
        historyHasOlder: a.hasOlder,
        historyOlderLoading: false,
        historyRevision: a.revision,
        historyDigest: a.digest,
      };
    }
    // Ref-resolved full content landed for history items already on screen:
    // patch by stable item id so the live tail and untouched items keep their
    // identity.
    case "history_items_patch": {
      let changed = false;
      const next = s.items.map((item) => {
        const patch = a.patches[item.id];
        if (!patch) return item;
        changed = true;
        return patch;
      });
      return changed ? { ...s, items: next } : s;
    }
    case "local_notice": return { ...s, running: false, turnActive: false, seq: s.seq + 1, items: [...s.items, { kind: "notice", id: `n${s.seq}`, level: a.level, text: a.text }] };
    case "clearApproval": {
      const next = { ...s, approval: undefined, pendingPrompt: Boolean(s.ask), resolvedPromptId: s.approval?.id ?? s.resolvedPromptId };
      return endPromptWaitIfIdle(next);
    }
    case "clearAsk": {
      const next = { ...s, ask: undefined, pendingPrompt: Boolean(s.approval), resolvedPromptId: s.ask?.id ?? s.resolvedPromptId };
      return endPromptWaitIfIdle(next);
    }
    case "clearExtensionForm": return s.extensionForm ? { ...s, extensionForm: undefined } : s;
    case "extension_notifications_drained": return s.extensionNotifications.length > 0 ? { ...s, extensionNotifications: [] } : s;
    // A tool-approval posture switch auto-allowed exactly these prompt ids on
    // the backend. Hide + tombstone the visible approval only when it is one
    // of them; anything else (plan/memory/sandbox-escape, ask-rule approvals
    // under auto) is still genuinely pending there and must stay visible —
    // tombstoning it would filter every future replay and strand the turn. The
    // drain result must also belong to this controller's prompt-id epoch.
    case "approval_drained": {
      if (s.promptEpoch !== a.epoch || !s.approval || !a.ids.includes(s.approval.id)) return s;
      const next = { ...s, approval: undefined, pendingPrompt: Boolean(s.ask), resolvedPromptId: s.approval.id };
      return endPromptWaitIfIdle(next);
    }
    // The optimistic clearApproval/clearAsk tombstone was wrong: the backend
    // call that was supposed to actually resolve this id failed, so the
    // prompt is still genuinely pending there. Undo the tombstone so the next
    // replay (proactively requested by the caller) can re-arm it instead of
    // being silently swallowed forever. Only for the epoch the RPC was issued
    // in: after a controller rebuild the same numeric id names a DIFFERENT
    // prompt, and a late failure from the old controller must not erase the
    // new controller's tombstone.
    case "submit_prompt_failed":
      return s.resolvedPromptId === a.id && s.promptEpoch === a.epoch ? { ...s, resolvedPromptId: undefined } : s;
    // A controller rebuild (model/effort/token-mode switch) replaces the
    // backend controller in place and its approval/ask ids restart from "1"
    // (per-controller counters, see sound.ts). Any id-anchored bookkeeping
    // from the OLD controller is meaningless for the new one and must be
    // dropped, or a genuinely new prompt reusing an old id would be misread
    // as a stale replay of an already-answered prompt and silently ignored.
    case "controller_rebuilt":
      // A rebuild restarts the runtime's extension sidecars too, so extension
      // surface state (and the per-surface generation fence) from the old
      // runtime is meaningless for the new one and is dropped with the rest of
      // the id-anchored bookkeeping.
      return {
        ...s,
        items: s.items.map((item) => item.kind === "user" && item.submissionId ? { ...item, submissionId: undefined } : item),
        promptEpoch: s.promptEpoch + 1,
        pendingSubmissionId: undefined,
        resolvedPromptId: undefined,
        promptArrivedId: undefined,
        promptArrivedAt: undefined,
        extensionStatuses: {},
        extensionForm: undefined,
        extensionNotifications: [],
        extensionGenerations: {},
      };
    case "reset": return { ...initialState, meta: metaWithoutCanonicalTodos(s.meta), context: { used: 0, window: s.context.window, sessionTokens: 0, compactRatio: s.context.compactRatio }, balance: s.balance, effort: s.effort, jobs: s.jobs, hydrating: s.hydrating, hydrateReason: s.hydrateReason, hydrateError: s.hydrateError, hydrateHistoryLoaded: s.hydrateHistoryLoaded, hydratePlaceholderItems: s.hydratePlaceholderItems, backendActivationPending: s.backendActivationPending, sessionGen: s.sessionGen + 1, promptEpoch: s.promptEpoch + 1 };
    case "context_panel_refresh": return { ...s, contextPanelSeq: s.contextPanelSeq + 1 };
    case "event": return applyEvent(s, a.e);
    case "stream_batch": return applyStreamBatch(s, a.segments);
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
    busyRunning: "status.effortSwitchBusyRunning",
    busyPrompt: "status.effortSwitchBusyPrompt",
    busyJobs: "status.effortSwitchBusyJobs",
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
    busyRunning: "status.modelSwitchBusyRunning",
    busyPrompt: "status.modelSwitchBusyPrompt",
    busyJobs: "status.modelSwitchBusyJobs",
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
    busyRunning: "status.tokenModeSwitchBusyRunning",
    busyPrompt: "status.tokenModeSwitchBusyPrompt",
    busyJobs: "status.tokenModeSwitchBusyJobs",
    leaseHeld: "status.tokenModeSwitchLeaseHeld",
    starting: "status.tokenModeSwitchStarting",
    startupFailed: "status.tokenModeSwitchStartupFailed",
    retry: "status.tokenModeSwitchRetry",
    failed: "status.tokenModeSwitchFailed",
  });
}

// noticeCodeKeys maps the backend's stable notice codes (event.NoticeCode*) to
// dictionary keys. Codes survive backend copy edits, unlike the exact-text
// matching in backendNoticeKey, which stays only as the fallback for events
// and replayed histories that carry no code.
const noticeCodeKeys: Record<string, DictKey> = {
  final_readiness: "notice.finalReadiness",
  empty_final: "notice.emptyFinal",
  executor_handoff: "notice.executorHandoff",
  tool_budget: "notice.toolBudget",
  prompt_queued: "notice.promptQueued",
  loop_guard: "notice.loopGuard",
  workspace_lease: "notice.workspaceLease",
  cancelled_turn_display: "notice.cancelledTurnDisplay",
  session_recovery_forked: "recovery.noticeSavedCopy",
  session_recovery_adopted: "recovery.noticeAdopted",
  session_recovery_adopted_covered: "recovery.noticeAdoptedCovered",
  session_recovery_depth_cap: "recovery.noticeKeptCurrent",
  session_shutdown_recovery_forked: "recovery.noticeSavedCopy",
  decision_receipt: "notice.decisionReceiptTitle",
  context_editing_fallback: "notice.contextEditingFallback",
};

// localizedNoticeText localizes a notice's main copy by its stable code first,
// then falls back to English-text matching for codeless payloads.
export function localizedNoticeText(text: string, code?: string): string {
  if (code === "unapplied_steer") {
    const separator = text.indexOf("\n");
    const guidance = separator >= 0 ? text.slice(separator + 1) : text;
    return t("notice.unappliedSteer", { guidance });
  }
  const key = code ? noticeCodeKeys[code] : undefined;
  if (key) return t(key);
  return localizedBackendNoticeText(text);
}

const deliveryRequirementKeys: Record<string, DictKey> = {
  project_check: "notice.deliveryRequirementProjectCheck",
  todo: "notice.deliveryRequirementTodo",
  criteria: "notice.deliveryRequirementCriteria",
  verification: "notice.deliveryRequirementVerification",
  review: "notice.deliveryRequirementReview",
  signoff: "notice.deliveryRequirementSignoff",
  action: "notice.deliveryRequirementAction",
  mutation: "notice.deliveryRequirementMutation",
  capability: "notice.deliveryRequirementCapability",
};

export function deliveryReadinessDetail(readiness: WireFinalReadiness | undefined, fallback = ""): string {
  const labels = asArray(readiness?.missing)
    .map((id) => deliveryRequirementKeys[id])
    .filter((key): key is DictKey => Boolean(key))
    .map((key) => t(key));
  if (labels.length === 0) return fallback;
  return t("notice.deliveryIncompleteMissing", { items: labels.join(t("notice.deliveryRequirementSeparator")) });
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
    /^repeated save conflicts were detected; saved the current conflict copy in an isolated recovery branch$/i.test(msg) ||
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
    case "Goal is not ready to complete yet; continuing the remaining work.":
      return "notice.goalNotReady";
    case "Goal still has unfinished task state; continuing the remaining work.":
      return "notice.goalUnfinished";
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

function recoveryNoticeDedupeKey(text: string, code?: string): string {
  switch (code) {
    case "session_recovery_forked":
    case "session_shutdown_recovery_forked":
      return "recovery:saved-copy";
    case "session_recovery_depth_cap":
      return "recovery:kept-current";
    case "session_recovery_adopted_covered":
      return "recovery:adopted-covered";
    case "session_recovery_adopted":
      return "recovery:adopted";
  }
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
    /^repeated save conflicts were detected; saved the current conflict copy in an isolated recovery branch$/i.test(msg) ||
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

export function quietTranscriptNoticeKey(text: string, code?: string): string {
  const recovery = recoveryNoticeDedupeKey(text, code);
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

function appendNoticeItem(items: Item[], seq: number, id: string, level: "info" | "warn", rawText: string, detail?: string, code?: string, decisionReceipt?: WireDecisionReceipt): { items: Item[]; seq: number } {
  if (quietTranscriptNoticeKey(rawText, code)) {
    return { items, seq };
  }
  const text = localizedNoticeText(rawText, code);
  if (quietTranscriptNoticeKey(text, code)) {
    return { items, seq };
  }
  const trimmedDetail = detail?.trim();
  return { items: [...items, { kind: "notice", id, level, text, ...(trimmedDetail ? { detail: trimmedDetail } : {}), ...(decisionReceipt ? { decisionReceipt } : {}) }], seq: seq + 1 };
}

function appendNoticeToState(s: State, level: "info" | "warn", text: string, detail?: string, code?: string, decisionReceipt?: WireDecisionReceipt): State {
  const next = appendNoticeItem(s.items, s.seq, `n${s.seq}`, level, text, detail, code, decisionReceipt);
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
    busyRunning: DictKey;
    busyPrompt: DictKey;
    busyJobs: DictKey;
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
    const detail = /running=(true|false);\s*pending_prompt=(true|false);\s*background_jobs=(\d+)/i.exec(msg);
    if (detail?.[2] === "true") return t(keys.busyPrompt);
    if (detail?.[1] === "true") return t(keys.busyRunning);
    const jobs = Number(detail?.[3] ?? 0);
    if (jobs > 0) return t(keys.busyJobs, { n: jobs });
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

export function replayPendingPromptsForActiveTab(activeTabId: string | undefined, replay: () => Promise<void> = () => app.ReplayPendingPrompts()): void {
  if (!activeTabId) return;
  void replay().catch(() => {});
}

export function useController() {
  const statesRef = useRef<TabStates>(new Map());
  const liveListenersByTabRef = useRef(new Map<string, Set<() => void>>());
  const balanceRefreshSeqByTab = useRef(new Map<string, number>());
  const modelSwitchSeqByTab = useRef(new Map<string, number>());
  const modelSwitchSuccessVersionByTab = useRef(new Map<string, number>());
  const modelSwitchQueueByTab = useRef(new Map<string, ModelSwitchQueueState>());
  const lastTurnActivityAtByTab = useRef(new Map<string, number>());
  const runtimeEpochByTabRef = useRef(new Map<string, string>());
  const appliedComposerProfileByTabRef = useRef(new Map<string, string>());
  const composerProfileInFlightByTabRef = useRef(new Map<string, { key: string; promise: Promise<boolean> }>());
  const composerProfileQueueByTabRef = useRef(new Map<string, Promise<void>>());
  const composerProfileLifecycleByTabRef = useRef(new Map<string, number>());
  const cancelReconcileTimers = useRef(new Map<string, number>());
  const stalePromptReconcileTimers = useRef(new Map<string, number>());
  // Indirection so dispatchRuntimeStatusForTab (defined above reconcileTabRuntime)
  // can schedule an authoritative refetch after it rejects a stale snapshot.
  const scheduleStalePromptReconcileRef = useRef<(tabId: string) => void>(() => {});
  const [activeTabId, setActiveTabId] = useState<string | undefined>();
  const activeTabIdRef = useRef<string | undefined>(undefined);
  // Invalidates async navigation completions even for ABA switches where the
  // visible tab ID eventually returns to the original value.
  const activeNavigationSeqRef = useRef(0);
  // A render-triggering counter so that mutations to a non-active tab's state still
  // cause a re-render when that tab becomes active.
  const [, setVersion] = useState(0);
  const bump = useCallback(() => setVersion((v) => v + 1), []);
  const notifyLiveListeners = useCallback((tabId: string) => {
    for (const listener of liveListenersByTabRef.current.get(tabId) ?? []) listener();
  }, [t]);
  const disposeComposerProfileState = useCallback((tabId: string) => {
    appliedComposerProfileByTabRef.current.delete(tabId);
    composerProfileInFlightByTabRef.current.delete(tabId);
    composerProfileQueueByTabRef.current.delete(tabId);
    composerProfileLifecycleByTabRef.current.set(
      tabId,
      (composerProfileLifecycleByTabRef.current.get(tabId) ?? 0) + 1,
    );
  }, []);
  const liveStore = useMemo<ControllerLiveStore>(() => ({
    subscribe(tabId, listener) {
      if (!tabId) return () => {};
      let listeners = liveListenersByTabRef.current.get(tabId);
      if (!listeners) {
        listeners = new Set();
        liveListenersByTabRef.current.set(tabId, listeners);
      }
      listeners.add(listener);
      return () => {
        listeners?.delete(listener);
        if (listeners?.size === 0) liveListenersByTabRef.current.delete(tabId);
      };
    },
    getSnapshot(tabId) {
      return tabId ? statesRef.current.get(tabId)?.live : undefined;
    },
    getModelActiveAt(tabId) {
      return tabId ? statesRef.current.get(tabId)?.turnModelActiveAt : undefined;
    },
  }), []);
  const beginActiveNavigation = useCallback(() => {
    activeNavigationSeqRef.current += 1;
    return activeNavigationSeqRef.current;
  }, []);
  const isNavigationIntentCurrent = useCallback((seq: number): boolean => {
    return activeNavigationSeqRef.current === seq;
  }, []);
  const navigationCompletionCurrent = useCallback((seq: number, kind: string, tabId: string): boolean => {
    if (activeNavigationSeqRef.current === seq) return true;
    addBreadcrumb(kind, `stale ${tabId} seq=${seq} current=${activeNavigationSeqRef.current}`);
    return false;
  }, []);

  // The active tab's current state, with a stable identity for cancel().
  const activeState = activeTabId ? getOrCreateState(statesRef.current, activeTabId) : initialState;
  const stateRef = useRef(activeState);
  const backendActiveTabIdRef = useRef<string | undefined>(undefined);
  const backendActivationPromises = useRef(new Map<string, Promise<boolean>>());
  // The latest ticketed topic activation (StartTopicActivation). Registered
  // before the backend call returns so synchronously-emitted lifecycle events
  // always match; a terminal event arriving before the ticket is stashed and
  // replayed once the ticket lands.
  const pendingTopicActivationRef = useRef<PendingTopicActivation | undefined>(undefined);
  const topicActivationSeqRef = useRef(0);
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
      // A tab with a live or in-flight turn is pinned out of transcript-store
      // eviction; its cached rows must survive until the turn settles.
      getTranscriptStore().setPinned(tabId, Boolean(next.running || next.turnActive || next.live));
      uiPerfTracker.onStateCommit();
      notifyLiveListeners(tabId);
      const streamDeltaOnly =
        (action.type === "stream_batch" ||
          (action.type === "event" && (action.e.kind === "text" || action.e.kind === "reasoning"))) &&
        prev.items === next.items &&
        prev.currentAssistant === next.currentAssistant &&
        prev.pendingUser === next.pendingUser &&
        prev.retry === next.retry;
      // Text/reasoning-only deltas only update the live stream — which the
      // frontend reads through its own subscription — so they must not bump the
      // full controller tree (the run-strip TPS estimate subscribes to the live
      // stream directly and updates itself).
      if (!streamDeltaOnly) bump();
    }
  }, [bump, notifyLiveListeners]);
  const clearBalanceForTab = useCallback((tabId: string): void => {
    invalidateSharedQuery("BalanceForTab", [tabId]);
    invalidateSharedQuery("MetaForTab", [tabId]);
    const seq = (balanceRefreshSeqByTab.current.get(tabId) ?? 0) + 1;
    balanceRefreshSeqByTab.current.set(tabId, seq);
    dispatchTo(tabId, { type: "balance", balance: { available: false, display: "" } });
  }, [dispatchTo]);

  const invalidateProviderStateForTab = useCallback((tabId: string): void => {
    balanceRefreshSeqByTab.current.set(
      tabId,
      (balanceRefreshSeqByTab.current.get(tabId) ?? 0) + 1,
    );
    modelSwitchSeqByTab.current.set(
      tabId,
      (modelSwitchSeqByTab.current.get(tabId) ?? 0) + 1,
    );
  }, []);

  const refreshBalanceForTab = useCallback(async (
    tabId: string,
    options: { apply?: () => boolean } = {},
  ): Promise<void> => {
    const seq = (balanceRefreshSeqByTab.current.get(tabId) ?? 0) + 1;
    balanceRefreshSeqByTab.current.set(tabId, seq);
    try {
      const balance = await app.BalanceForTab(tabId);
      if (balanceRefreshSeqByTab.current.get(tabId) !== seq) return;
      if (options.apply && !options.apply()) return;
      if (balance.err?.trim()) return;
      dispatchTo(tabId, { type: "balance", balance });
    } catch {
      // Balance is optional. Keep the last explicit cleared/unavailable state
      // instead of surfacing a provider-specific wallet failure in chat.
    }
  }, [dispatchTo]);

  const confirmBackendActiveTab = useCallback((tabId: string) => {
    backendActiveTabIdRef.current = tabId;
    dispatchTo(tabId, { type: "backend_activation_done" });
  }, [dispatchTo]);

  const reassertVisibleTabAfterStaleNavigation = useCallback(async (kind: string, staleTabId: string): Promise<void> => {
    // Backend navigation calls activate their result before returning. If a
    // newer tab click won in the frontend while that call was in flight, put
    // the backend back on the visible tab. Re-check after every await because
    // another click can supersede the target while SetActiveTab is running.
    for (;;) {
      const currentTabId = activeTabIdRef.current;
      if (!currentTabId) return;
      if (currentTabId === staleTabId) {
        confirmBackendActiveTab(currentTabId);
        return;
      }
      try {
        await app.SetActiveTab(currentTabId);
      } catch (err) {
        addBreadcrumb(kind, `stale reassert failed ${currentTabId}: ${errorMessage(err)}`);
        return;
      }
      if (activeTabIdRef.current === currentTabId) {
        confirmBackendActiveTab(currentTabId);
        addBreadcrumb(kind, `stale reasserted ${currentTabId}`);
        return;
      }
    }
  }, [confirmBackendActiveTab]);

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
  const metaRefreshSeq = useRef(new Map<string, number>());
  const sessionLoadSeq = useRef(new Map<string, number>());
  const cancelHydrateSeq = useRef(new Map<string, number>());
  const sessionLoadInFlight = useRef(new Map<string, { sessionPath: string; revision?: number; digest?: string; promise: Promise<void> }>());
  const transcriptSubscriptions = useRef(new Map<string, () => void>());
  const bumpMetaRefreshSeq = useCallback((tabId: string): number => {
    const seq = (metaRefreshSeq.current.get(tabId) ?? 0) + 1;
    metaRefreshSeq.current.set(tabId, seq);
    return seq;
  }, []);
  const metaRefreshCurrent = useCallback((tabId: string, seq: number): boolean => {
    return metaRefreshSeq.current.get(tabId) === seq;
  }, []);
  const bumpSessionLoadSeq = useCallback((tabId: string): number => {
    bumpMetaRefreshSeq(tabId);
    invalidateSharedQuery("MetaForTab", [tabId]);
    const seq = (sessionLoadSeq.current.get(tabId) ?? 0) + 1;
    sessionLoadSeq.current.set(tabId, seq);
    return seq;
  }, [bumpMetaRefreshSeq]);
  // Ref-resolved content updates flow from the transcript store into the tab's
  // state as id-keyed patches. Subscribed once per tab; released when the tab
  // state is dropped (close / single-surface prune).
  const ensureTranscriptSubscription = useCallback((tabId: string) => {
    if (transcriptSubscriptions.current.has(tabId)) return;
    const unsubscribe = getTranscriptStore().subscribe(tabId, (change) => {
      if (!statesRef.current.has(tabId)) return;
      dispatchTo(tabId, { type: "history_items_patch", patches: change.patches });
    });
    transcriptSubscriptions.current.set(tabId, unsubscribe);
  }, [dispatchTo]);
  const releaseTranscriptState = useCallback((tabId: string) => {
    transcriptSubscriptions.current.get(tabId)?.();
    transcriptSubscriptions.current.delete(tabId);
    getTranscriptStore().evictTab(tabId);
  }, []);
  const sessionLoadCurrent = useCallback((tabId: string, seq: number): boolean => {
    return sessionLoadSeq.current.get(tabId) === seq;
  }, []);
  const bumpCancelHydrateSeq = useCallback((tabId: string): number => {
    const seq = (cancelHydrateSeq.current.get(tabId) ?? 0) + 1;
    cancelHydrateSeq.current.set(tabId, seq);
    return seq;
  }, []);
  const cancelHydrateCurrent = useCallback((tabId: string, seq: number): boolean => {
    return cancelHydrateSeq.current.get(tabId) === seq;
  }, []);
  const loadMetaForTab = useCallback(async (tabId: string): Promise<Meta | undefined> => {
    const seq = bumpMetaRefreshSeq(tabId);
    const meta = await app.MetaForTab(tabId).catch(() => undefined);
    if (!metaRefreshCurrent(tabId, seq)) return undefined;
    if (meta?.runtime?.epoch) runtimeEpochByTabRef.current.set(tabId, meta.runtime.epoch);
    return meta;
  }, [bumpMetaRefreshSeq, metaRefreshCurrent]);
  const refreshMetaOnlyForTab = useCallback(async (tabId: string): Promise<Meta | undefined> => {
    const meta = await loadMetaForTab(tabId);
    if (meta !== undefined) dispatchTo(tabId, { type: "meta", meta });
    return meta;
  }, [dispatchTo, loadMetaForTab]);
  const refreshMetaForTab = useCallback(async (tabId: string): Promise<void> => {
    const sessionSeq = sessionLoadSeq.current.get(tabId) ?? 0;
    const meta = await loadMetaForTab(tabId);
    if (meta === undefined || (sessionLoadSeq.current.get(tabId) ?? 0) !== sessionSeq) return;
    dispatchTo(tabId, { type: "meta", meta });
    const [context, effort] = await Promise.all([
      app.ContextUsageForTab(tabId).catch(() => undefined),
      app.EffortForTab(tabId).catch(() => undefined),
    ]);
    if ((sessionLoadSeq.current.get(tabId) ?? 0) !== sessionSeq) return;
    if (context !== undefined) dispatchTo(tabId, { type: "context", context });
    if (effort !== undefined) dispatchTo(tabId, { type: "effort", effort });
  }, [dispatchTo, loadMetaForTab]);
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
    options: {
      skipHistory?: boolean;
      placeholderItems?: Item[];
      preserveCachedHistory?: boolean;
      sessionPath?: string;
      sessionRevision?: number;
      sessionDigest?: string;
      cancelHydrateGeneration?: number;
      deferResetUntilHistory?: boolean;
    } = {},
  ) => {
    const stateMeta = statesRef.current.get(tabId)?.meta;
    const sessionPath = (options.sessionPath ?? stateMeta?.sessionPath ?? "").trim();
    const sessionRevision = options.sessionRevision ?? stateMeta?.sessionRevision;
    const sessionDigest = options.sessionDigest ?? stateMeta?.sessionDigest;
    const canJoinInFlight = !reset && !options.skipHistory;
    const shouldTrackInFlight = !options.skipHistory;
    if (canJoinInFlight) {
      const existing = sessionLoadInFlight.current.get(tabId);
      if (existing?.sessionPath === sessionPath && existing.revision === sessionRevision && existing.digest === sessionDigest) return existing.promise;
    } else {
      sessionLoadInFlight.current.delete(tabId);
    }

    const promise = (async () => {
      const cancelHydrateGeneration = options.cancelHydrateGeneration;
      if (cancelHydrateGeneration !== undefined && !cancelHydrateCurrent(tabId, cancelHydrateGeneration)) return;
      const seq = bumpSessionLoadSeq(tabId);
      const hydrateStartedAt = Date.now();
      const skipHistory = Boolean(
        options.skipHistory ||
        (options.preserveCachedHistory && !reset && hasReusableCachedTranscript(statesRef.current.get(tabId), sessionPath, sessionRevision, sessionDigest)),
      );
      const deferResetUntilHistory = Boolean(options.deferResetUntilHistory && reset && !skipHistory);
      const stillCurrent = () =>
        sessionLoadCurrent(tabId, seq) &&
        (cancelHydrateGeneration === undefined || cancelHydrateCurrent(tabId, cancelHydrateGeneration));
      if (!stillCurrent()) return;
      addBreadcrumb("tab.hydrate", `start ${reason} ${tabId}`);
      ensureTranscriptSubscription(tabId);
      dispatchTo(tabId, { type: "hydrate_start", reason, placeholderItems: options.placeholderItems });
      if (reset && !deferResetUntilHistory && stillCurrent()) dispatchTo(tabId, { type: "reset" });
      const requiresVisibleTab = reason === "startup" || reason === "switch-tab" || reason === "open-topic";
      const stillVisible = () => !requiresVisibleTab || activeTabIdRef.current === tabId;
      const foregroundTurnActive = (): boolean => {
        const state = statesRef.current.get(tabId);
        return Boolean(state?.running || state?.turnActive || state?.pendingPrompt);
      };
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
      let projection = skipHistory
        ? undefined
        : await loadTimed("history", () =>
            // Windowed slice load through the transcript store. A resident
            // session (LRU hit) projects synchronously with no backend round
            // trip; reset loads always re-fetch so a rebound session can never
            // serve the previous session's rows.
            getTranscriptStore().loadLatest(tabId, sessionPath, {
              turns: HISTORY_PAGE_TURNS,
              preferResident: !reset,
              expectedRevision: sessionRevision,
              expectedDigest: sessionDigest,
            }),
          );

      if (!stillCurrent()) return;
      if (!skipHistory && projection !== undefined && !foregroundTurnActive()) {
        if (deferResetUntilHistory && stillCurrent()) dispatchTo(tabId, { type: "reset" });
        dispatchTo(tabId, {
          type: "history_replace",
          items: projection.items,
          startTurn: projection.startTurn,
          totalTurns: projection.totalTurns,
          hasOlder: projection.hasOlder,
          revision: projection.revisionKnown ? projection.revision : undefined,
          digest: projection.digest || undefined,
        });
        addBreadcrumb(
          "tab.hydrate",
          `history page ${tabId} items=${projection.items.length} turns=${projection.startTurn}-${projection.endTurn}/${projection.totalTurns} ms=${Date.now() - historyStartedAt}`,
        );
        if (reason === "switch-tab") {
          addBreadcrumb(
            "tab.switch",
            `history-done ${tabId} items=${projection.items.length} turns=${projection.startTurn}-${projection.endTurn}/${projection.totalTurns} ms=${Date.now() - historyStartedAt}`,
          );
        }
      } else if (skipHistory) {
        const skipReason = options.skipHistory ? "cached-live-turn" : "cached-transcript";
        addBreadcrumb("tab.hydrate", `history skipped ${tabId} reason=${skipReason}`);
        if (reason === "switch-tab") {
          addBreadcrumb("tab.switch", `history-done ${tabId} skipped ms=${Date.now() - historyStartedAt}`);
        }
      }

      if (!stillCurrent()) return;
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
      let meta = await loadTimed("meta", () => loadMetaForTab(tabId));
      if (!stillCurrent()) return;
      if (!stillVisible()) {
        addBreadcrumb("tab.hydrate", `meta ignored inactive ${reason} ${tabId}`);
        return;
      }
      if (meta !== undefined) dispatchTo(tabId, { type: "meta", meta });
      if (meta !== undefined && projection !== undefined && !skipHistory &&
        !foregroundTurnActive() && !historyFingerprintMatchesMeta(projection, meta)) {
        // The transcript and metadata are persisted in separate files. A save
        // can advance between those reads, so an exact-content page may be
        // older than the metadata sampled just afterwards. Re-read both as a
        // bounded pair; only replace the visible history when their canonical
        // fingerprints agree. A continuously changing session keeps the first
        // exact page and remains non-reusable because its digest differs.
        for (let attempt = 0; attempt < 2; attempt += 1) {
          const reconciledProjection = await loadTimed("history reconcile", () => getTranscriptStore().loadLatest(tabId, sessionPath, {
            turns: HISTORY_PAGE_TURNS,
            preferResident: false,
            expectedRevision: meta?.sessionRevision,
            expectedDigest: meta?.sessionDigest,
          }));
          if (!stillCurrent() || !stillVisible() || reconciledProjection === undefined) return;
          const reconciledMeta = await loadTimed("meta reconcile", () => loadMetaForTab(tabId));
          if (!stillCurrent() || !stillVisible() || reconciledMeta === undefined) return;
          meta = reconciledMeta;
          dispatchTo(tabId, { type: "meta", meta });
          if (!foregroundTurnActive() && historyFingerprintMatchesMeta(reconciledProjection, meta)) {
            projection = reconciledProjection;
            dispatchTo(tabId, {
              type: "history_replace",
              items: projection.items,
              startTurn: projection.startTurn,
              totalTurns: projection.totalTurns,
              hasOlder: projection.hasOlder,
              revision: projection.revisionKnown ? projection.revision : undefined,
              digest: projection.digest || undefined,
            });
            break;
          }
          if (foregroundTurnActive()) break;
        }
      }
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
      if (effort !== undefined) dispatchTo(tabId, { type: "effort", effort });
      if (jobs !== undefined) dispatchTo(tabId, { type: "jobs", jobs: asArray(jobs) });
      if (context !== undefined) dispatchTo(tabId, { type: "context", context });
      // Signal ContextPanel to re-fetch now that ancillary data (context,
      // effort, jobs) has landed. Without this, the right-side panel keeps
      // stale RequestCount / ElapsedMs / SessionCost from before a session
      // rebind because its refreshKey (dockRefreshKey) only bumps on turn_done.
      dispatchTo(tabId, { type: "context_panel_refresh" });
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
      void refreshBalanceForTab(tabId, {
        apply: () => sessionLoadCurrent(tabId, seq) && stillVisible(),
      });
    })();
    if (shouldTrackInFlight) {
      sessionLoadInFlight.current.set(tabId, { sessionPath, revision: sessionRevision, digest: sessionDigest, promise });
    }
    try {
      await promise;
    } finally {
      if (sessionLoadInFlight.current.get(tabId)?.promise === promise) {
        sessionLoadInFlight.current.delete(tabId);
      }
    }
  }, [bumpSessionLoadSeq, cancelHydrateCurrent, dispatchTo, loadMetaForTab, refreshBalanceForTab, sessionLoadCurrent]);

  // On-demand full content for a ref-replaced history field (entries carrying
  // refs[] ship a ≤4KiB preview inline). Resolves through the transcript
  // store, which patches the projected items by stable id on completion. The
  // rendering layer calls this when a truncated entry scrolls into view.
  const requestHistoryFullContent = useCallback(async (entryId: string, field: string): Promise<string | undefined> => {
    const tabId = activeTabIdRef.current;
    if (!tabId) return undefined;
    ensureTranscriptSubscription(tabId);
    return getTranscriptStore().requestFullContent(tabId, entryId, field);
  }, [ensureTranscriptSubscription]);

  const loadOlderHistory = useCallback(async (tabId?: string): Promise<void> => {
    const targetTabId = tabId || activeTabIdRef.current;
    if (!targetTabId) return;
    const state = statesRef.current.get(targetTabId);
    if (!state?.historyHasOlder || state.historyOlderLoading || state.running) return;
    const sessionPath = state.meta?.sessionPath ?? "";
    const sessionRevision = state.meta?.sessionRevision ?? state.historyRevision;
    const sessionDigest = state.meta?.sessionDigest ?? state.historyDigest;
    ensureTranscriptSubscription(targetTabId);
    dispatchTo(targetTabId, { type: "history_older_start" });
    const startedAt = Date.now();
    try {
      const result = await getTranscriptStore().loadOlder(targetTabId, sessionPath, { turns: HISTORY_PAGE_TURNS });
      const current = statesRef.current.get(targetTabId);
      const currentRevision = current?.meta?.sessionRevision ?? current?.historyRevision;
      const currentDigest = current?.meta?.sessionDigest ?? current?.historyDigest;
      const fingerprintMatches = (expected: number | undefined, actual: number | undefined) =>
        expected === undefined || expected <= 0 ? true : actual === expected;
      const digestMatches = (expected: string | undefined, actual: string | undefined) =>
        !expected || actual === expected;
      // A replace-level hydrate while the page was in flight clears
      // historyOlderLoading; a metadata or canonical-identity change also
      // makes the page belong to a different transcript generation.
      if (!current || !current.historyOlderLoading || (current.meta?.sessionPath ?? "") !== sessionPath ||
        !fingerprintMatches(sessionRevision, currentRevision) || !digestMatches(sessionDigest, currentDigest) ||
        (result !== undefined && (!fingerprintMatches(sessionRevision, result.revisionKnown ? result.revision : undefined) ||
          !digestMatches(sessionDigest, result.digest)))) {
        dispatchTo(targetTabId, { type: "history_older_error" });
        return;
      }
      if (!result) {
        // Superseded (generation moved) or nothing older left.
        dispatchTo(targetTabId, { type: "history_older_error" });
        return;
      }
      if (result.kind === "reload") {
        // The cursor went stale (session rewritten): the store reloaded the
        // latest page; replace instead of prepend.
        dispatchTo(targetTabId, {
          type: "history_replace",
          items: result.items,
          startTurn: result.startTurn,
          totalTurns: result.totalTurns,
          hasOlder: result.hasOlder,
          revision: result.revisionKnown ? result.revision : undefined,
          digest: result.digest || undefined,
        });
      } else {
        dispatchTo(targetTabId, {
          type: "history_prepend",
          items: result.prependItems,
          removeIds: result.removeIds,
          startTurn: result.startTurn,
          totalTurns: result.totalTurns,
          hasOlder: result.hasOlder,
          revision: result.revisionKnown ? result.revision : undefined,
          digest: result.digest || undefined,
        });
      }
      addBreadcrumb(
        "tab.hydrate",
        `history older ${targetTabId} kind=${result.kind} items=${result.kind === "prepend" ? result.prependItems.length : result.items.length} turns=${result.startTurn}-${result.endTurn}/${result.totalTurns} ms=${Date.now() - startedAt}`,
      );
    } catch (err) {
      dispatchTo(targetTabId, { type: "history_older_error" });
      addBreadcrumb("tab.hydrate", `history older failed ${targetTabId}: ${errorMessage(err)}`);
    }
  }, [dispatchTo, ensureTranscriptSubscription]);

  const activeTabFromBackend = useCallback(async (): Promise<TabMeta | undefined> => {
    const tabs = asArray(await app.ListTabs().catch(() => [] as TabMeta[]));
    return tabs.find((tab) => tab.active) ?? tabs[0];
  }, []);

  // snapshotAt is the promptEventClock() reading taken immediately before
  // initiating the backend call that produced `tab`. The reducer uses it to
  // ignore snapshots that predate a live approval/ask event (#6429).
  const dispatchRuntimeStatusForTab = useCallback((tabId: string, tab: RuntimeMetaSnapshot, snapshotAt?: number) => {
    const foregroundRunning = foregroundRunningFromRuntimeMeta(tab);
    // Will the reducer reject this as a snapshot that predates the live prompt?
    // Computed on pre-dispatch state so we can schedule an authoritative
    // refetch when a stale idle snapshot is ignored.
    const rejectedStaleIdle = !tab.pendingPrompt && runtimeSnapshotPredatesPrompt(statesRef.current.get(tabId), snapshotAt);
    dispatchTo(tabId, {
      type: "backend_status",
      running: foregroundRunning,
      pendingPrompt: Boolean(tab.pendingPrompt),
      backgroundJobs: tab.backgroundJobs ?? 0,
      cancelRequested: Boolean(tab.cancelRequested),
      cancellable: foregroundRunning,
      snapshotAt,
    });
    // backend_status reconciliation can clear a live prompt from frontend state.
    // If the backend is still blocked, ask it to replay the approval/ask event.
    if (tab.pendingPrompt) replayPendingPromptsForActiveTab(tabId);
    // A stale idle snapshot the reducer ignored cannot be trusted to have kept a
    // GENUINE prompt: navigation can drop the prompt anchor, so a delayed replay
    // of an already-answered prompt looks like a fresh prompt and re-anchors,
    // making this authoritative idle look stale. Refetch backend truth once so a
    // resolved prompt is cleared instead of surviving as a zombie (#6432).
    if (rejectedStaleIdle) scheduleStalePromptReconcileRef.current(tabId);
    // A prompt that survived reconciliation (fresh pendingPrompt=true meta, or
    // a stale snapshot the reducer ignored) keeps the tab blocked on the user.
    // Report it as foreground-running so callers do not treat the snapshot as
    // a missed turn_done and reset the session out from under the prompt.
    const local = statesRef.current.get(tabId);
    if (local?.approval || local?.ask) return true;
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
    const snapshotAt = promptEventClock();
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
    if (active.runtime?.epoch) runtimeEpochByTabRef.current.set(active.id, active.runtime.epoch);
    dispatchTo(active.id, { type: "optimistic_meta", meta: metaFromTab(active, statesRef.current.get(active.id)?.meta) });
    const preserveCachedHistory = options.preserveCachedHistory ?? !reset;
    if (!reset) dispatchRuntimeStatusForTab(active.id, active, snapshotAt);
    await loadSessionDataForTab(active.id, reset, "startup", {
      preserveCachedHistory,
      sessionPath: active.sessionPath,
      sessionRevision: active.sessionRevision,
      sessionDigest: active.sessionDigest,
    });
    if (reset) dispatchRuntimeStatusForTab(active.id, active, snapshotAt);
    return active.id;
  }, [activeTabFromBackend, beginActiveNavigation, confirmBackendActiveTab, dispatchRuntimeStatusForTab, dispatchTo, loadSessionDataForTab]);

  const reconcileTabRuntime = useCallback(async (
    tabId: string,
    options: { hydrateSessionData?: boolean } = {},
  ): Promise<TabMeta[] | undefined> => {
    const hydrateSessionData = options.hydrateSessionData ?? true;
    const snapshotAt = promptEventClock();
    const tabs = asArray(await app.ListTabs().catch(() => [] as TabMeta[]));
    const tab = tabs.find((candidate) => candidate.id === tabId);
    if (!tab) return undefined;
    if (tab.runtime?.epoch) runtimeEpochByTabRef.current.set(tabId, tab.runtime.epoch);
    const local = statesRef.current.get(tabId);
    const needsInitialLoad = !local?.meta;
    const foregroundRunning = dispatchRuntimeStatusForTab(tabId, tab, snapshotAt);
    const missedTurnDone = Boolean(local?.running && !foregroundRunning);
    if (hydrateSessionData && (needsInitialLoad || missedTurnDone)) {
      await loadSessionDataForTab(tabId, missedTurnDone, "startup", {
        sessionPath: tab.sessionPath,
        sessionRevision: tab.sessionRevision,
        sessionDigest: tab.sessionDigest,
      });
      return tabs;
    }
    const [jobs, effort] = await Promise.all([
      app.JobsForTab(tabId).catch(() => undefined),
      app.EffortForTab(tabId).catch(() => undefined),
    ]);
    if (jobs) dispatchTo(tabId, { type: "jobs", jobs: asArray(jobs) });
    if (effort) dispatchTo(tabId, { type: "effort", effort });
    await refreshBalanceForTab(tabId);
    return tabs;
  }, [dispatchRuntimeStatusForTab, loadSessionDataForTab, refreshBalanceForTab]);

  // Authoritative backstop for the prompt-freshness heuristic: after the reducer
  // rejects a stale idle snapshot, refetch backend state once. If the backend
  // resolved the prompt, the fresh snapshot (fetched after any in-flight replay)
  // is newer than the anchor and reconciles the zombie away; if the prompt is
  // genuinely pending, the fresh snapshot keeps it. Debounced per tab so a burst
  // of stale snapshots schedules at most one refetch (#6432).
  const scheduleStalePromptReconcile = useCallback((tabId: string) => {
    if (stalePromptReconcileTimers.current.has(tabId)) return;
    const timer = window.setTimeout(() => {
      stalePromptReconcileTimers.current.delete(tabId);
      void reconcileTabRuntime(tabId, { hydrateSessionData: false }).catch(() => {});
    }, STALE_PROMPT_RECONCILE_MS);
    stalePromptReconcileTimers.current.set(tabId, timer);
  }, [reconcileTabRuntime]);
  scheduleStalePromptReconcileRef.current = scheduleStalePromptReconcile;

  const clearCancelReconcileTimer = useCallback((tabId: string) => {
    const timer = cancelReconcileTimers.current.get(tabId);
    if (timer === undefined) return;
    window.clearTimeout(timer);
    cancelReconcileTimers.current.delete(tabId);
  }, []);

  const scheduleCancelReconcile = useCallback((tabId: string, attempt = 0, cancelHydrateGeneration?: number) => {
    clearCancelReconcileTimer(tabId);
    const delay = CANCEL_RECONCILE_DELAYS_MS[Math.min(attempt, CANCEL_RECONCILE_DELAYS_MS.length - 1)];
    const timer = window.setTimeout(() => {
      cancelReconcileTimers.current.delete(tabId);
      void reconcileTabRuntime(tabId, { hydrateSessionData: false }).then((tabs) => {
        const tab = tabs?.find((candidate) => candidate.id === tabId);
        if (!tab) return;
        const stillReconciling = foregroundRunningFromRuntimeMeta(tab) || Boolean(tab.cancelRequested);
        if (stillReconciling && attempt + 1 < CANCEL_RECONCILE_DELAYS_MS.length) {
          scheduleCancelReconcile(tabId, attempt + 1, cancelHydrateGeneration);
          return;
        }
        // Cancel can race the optimistic user bubble before turn_started. The
        // backend still persists that visible prompt (and its checkpoint), so
        // hydrate the authoritative transcript once teardown is idle instead
        // of leaving the UI with a discarded bubble and no edit/rewind target.
        if (!stillReconciling) {
          void loadSessionDataForTab(tabId, true, "rewind", {
            cancelHydrateGeneration,
            deferResetUntilHistory: true,
          }).catch(() => {});
          void refreshCheckpoints(tabId);
        }
      }).catch(() => {});
    }, delay);
    cancelReconcileTimers.current.set(tabId, timer);
  }, [clearCancelReconcileTimer, loadSessionDataForTab, reconcileTabRuntime, refreshCheckpoints]);

  // Topic-activation lifecycle events drive the ticketed activation flow: the
  // visible surface already switched when StartTopicActivation returned; the
  // history hydrate waits for the terminal "ready" of the LATEST request.
  // Events for superseded requestIds (including their "cancelled") are
  // dropped; agent:ready/agent:event handling is untouched and still covers
  // every non-ticketed flow (rebind, recovery, restore, SetActiveTab).
  const handleTopicActivationEvent = useCallback((event: TopicActivationEvent) => {
    const pending = pendingTopicActivationRef.current;
    if (!pending || event.requestId !== pending.requestId) return;
    if (event.phase === "starting") {
      noteActivationStarted(event.requestId, event.tabId);
      return;
    }
    if (!pending.tabId) {
      // The ticket has not resolved yet; replay once activateTopic applies it.
      pending.terminal = event;
      return;
    }
    if (event.phase === "cancelled") {
      noteActivationSettled(event.requestId, "cancelled");
      if (pendingTopicActivationRef.current === pending) pendingTopicActivationRef.current = undefined;
      return;
    }
    pendingTopicActivationRef.current = undefined;
    if (!isNavigationIntentCurrent(pending.navigationSeq)) return;
    const tabId = pending.tabId;
    if (activeTabIdRef.current !== tabId) return;
    if (event.phase === "failed") {
      noteActivationSettled(event.requestId, "failed", event.error);
      dispatchTo(tabId, {
        type: "hydrate_error",
        reason: "open-topic",
        error: event.error?.trim() || "session is not ready",
      });
      return;
    }
    noteActivationSettled(event.requestId, "ready");
    ensureTranscriptSubscription(tabId);
    void loadSessionDataForTab(tabId, true, "open-topic", { placeholderItems: pending.placeholderItems })
      .then(() => reconcileTabRuntime(tabId, { hydrateSessionData: false }))
      .catch(() => {});
  }, [dispatchTo, ensureTranscriptSubscription, isNavigationIntentCurrent, loadSessionDataForTab, reconcileTabRuntime]);

  useEffect(() => {
    const textBatch = createRafBatch<StreamDeltaEntry>((batch) => {
      uiPerfTracker.onStreamDispatch();
      for (const b of coalesceStreamDeltas(batch)) dispatchTo(b.tabId, { type: "stream_batch", segments: b.segments });
    });
    const off = onEvent((e) => {
      // Untagged compatibility events belong to the tab that the backend has
      // actually activated, not the frontend's optimistic selection. During a
      // slow SetActiveTab these can differ, and routing to the optimistic tab
      // leaks the previous session's approval/ask gate into the new composer.
      const targetTabId = e.tabId || backendActiveTabIdRef.current || activeTabIdRef.current;
      if (!targetTabId) return;
      const acceptedEpoch = runtimeEpochByTabRef.current.get(targetTabId);
      if (e.runtimeEpoch) {
        if (!acceptsRuntimeEventEpoch(acceptedEpoch, e.runtimeEpoch)) return;
        if (!acceptedEpoch) runtimeEpochByTabRef.current.set(targetTabId, e.runtimeEpoch);
      }
      uiPerfTracker.onWireEvent(targetTabId, e.kind);
      if (TURN_ACTIVITY_KINDS.has(e.kind)) lastTurnActivityAtByTab.current.set(targetTabId, Date.now());
      if (e.kind === "text" || e.kind === "reasoning") {
        if (e.submissionId) dispatchTo(targetTabId, { type: "send_confirmed", submissionId: e.submissionId });
        textBatch.push({ tabId: targetTabId, e });
      } else {
        textBatch.drain();
        dispatchTo(targetTabId, { type: "event", e });
      }
      if (e.kind === "turn_done" || e.kind === "context_maintenance") {
        void app.ContextUsageForTab(targetTabId).then((context) => dispatchTo(targetTabId, { type: "context", context })).catch(() => {});
      }
      if (e.kind === "turn_done") {
        invalidateSharedQuery("BalanceForTab", [targetTabId]);
        void refreshBalanceForTab(targetTabId);
        app.EffortForTab(targetTabId).then((effort) => dispatchTo(targetTabId, { type: "effort", effort })).catch(() => {});
        void refreshCheckpoints(targetTabId);
        invalidateSharedQuery("MetaForTab", [targetTabId]);
        void refreshMetaForTab(targetTabId);
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

    // A rebuilt controller reissues approval/ask ids from "1" (see sound.ts).
    // Drop this tab's id-anchored prompt bookkeeping so a genuinely new
    // prompt from the new controller is never misread as a stale replay of
    // one the old controller already resolved (#6432 round 3). A tab-less
    // rebuild (settings-wide) affects every known tab.
    const offRebuilt = onRuntimeRebuilt((rebuiltTabId, runtimeEpoch) => {
      if (rebuiltTabId) {
        invalidateSharedQuery("MetaForTab", [rebuiltTabId]);
        if (runtimeEpoch) runtimeEpochByTabRef.current.set(rebuiltTabId, runtimeEpoch);
        dispatchTo(rebuiltTabId, { type: "controller_rebuilt" });
      } else {
        if (runtimeEpoch) {
          for (const id of Array.from(statesRef.current.keys())) runtimeEpochByTabRef.current.set(id, runtimeEpoch);
        }
        for (const id of Array.from(statesRef.current.keys())) {
          invalidateSharedQuery("MetaForTab", [id]);
          dispatchTo(id, { type: "controller_rebuilt" });
        }
      }
    });
    const offTopicActivation = onTopicActivation(handleTopicActivationEvent);
    // tab:meta carries a full refreshed Meta after the backend's background
    // refresh of the expensive fields (git branch, image-input capability) —
    // those arrive empty in the first MetaForTab response now. Merge it like a
    // MetaForTab result, fenced to the session the tab is currently bound to.
    const offTabMeta = onTabMeta(({ tabId, meta }) => {
      if (!tabId || !meta) return;
      const current = statesRef.current.get(tabId);
      if (!current?.meta) return;
      if (
        meta.sessionPath !== undefined &&
        current.meta.sessionPath !== undefined &&
        meta.sessionPath !== current.meta.sessionPath
      ) {
        return;
      }
      dispatchTo(tabId, { type: "meta", meta });
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
      for (const timer of stalePromptReconcileTimers.current.values()) {
        window.clearTimeout(timer);
      }
      stalePromptReconcileTimers.current.clear();
      off();
      offReady();
      offRebuilt();
      offTopicActivation();
      offTabMeta();
    };
  }, [dispatchTo, handleTopicActivationEvent, loadSessionDataForTab, refreshBalanceForTab, refreshCheckpoints, refreshMetaForTab, syncActiveTabFromBackend]);

  // Track the visible tab in the transcript store: the active tab is pinned
  // out of LRU eviction. (In-flight loads of background tabs still complete
  // into their own per-tab state; store generations move on session switch,
  // evict, and unload — not on visible-tab changes.)
  const previousStoreActiveTabRef = useRef<string | undefined>(undefined);
  useEffect(() => {
    getTranscriptStore().noteActiveTab(activeTabId, previousStoreActiveTabRef.current);
    previousStoreActiveTabRef.current = activeTabId;
  }, [activeTabId]);

  // Keep shared all-source telemetry live between turn boundaries. Delivery
  // mode can complete dozens of provider requests inside one UI turn, while
  // the status bar reads state.context and would otherwise stay pinned to the
  // previous turn_done snapshot. A usage event is emitted after the backend
  // has recorded that request, so refresh the authoritative tab aggregate here.
  // The usage sequence and active-tab checks make this latest-request-wins:
  // slower snapshots cannot overwrite a newer usage event or a tab switch.
  useEffect(() => {
    const tabId = activeTabId;
    const usageSeq = activeState.usageSeq;
    if (!tabId || usageSeq <= 0 || !activeState.turnActive) return;

    let cancelled = false;
    void app.ContextUsageForTab(tabId).then((context) => {
      if (cancelled || activeTabIdRef.current !== tabId) return;
      if (statesRef.current.get(tabId)?.usageSeq !== usageSeq) return;
      dispatchTo(tabId, { type: "context", context });
    }).catch(() => {});

    return () => {
      cancelled = true;
    };
  }, [activeTabId, activeState.turnActive, activeState.usageSeq, dispatchTo]);

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
      const nextMeta = await refreshMetaOnlyForTab(tabId);
      if (!stillCurrent()) return;
      if (nextMeta?.ready || nextMeta?.startupErr || attempt + 1 >= STARTUP_READY_META_RECONCILE_ATTEMPTS) return;
      schedule(attempt + 1);
    };

    schedule(0);
    return () => {
      cancelled = true;
      if (timer !== undefined) window.clearTimeout(timer);
    };
  }, [activeTabId, activeState.meta?.ready, activeState.meta?.startupErr, activeState.backendActivationPending, refreshMetaOnlyForTab]);

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

  const sendToTab = useCallback(async (
    tabId: string,
    displayText: string,
    submitText = displayText,
    originalText?: string,
    structured?: import("./invocationDisplay").StructuredInvocationSubmit,
    initialGoal?: {
      goal: string;
      collaborationMode: CollaborationMode;
      toolApprovalMode: ToolApprovalMode;
    },
  ) => {
    if (!tabId) throw new Error(t("composer.workspaceStarting"));
    const currentState = getOrCreateState(statesRef.current, tabId);
    const runtime = currentState.meta?.runtime;
    if (currentState.meta && !runtimeReadyForSubmit(currentState.meta)) {
      throw new Error(runtime?.issue?.message || currentState.meta.startupErr || t("composer.workspaceStarting"));
    }
    const seq = currentState.seq;
    const submissionId = createTurnSubmissionId(tabId, currentState.sessionGen, seq, runtimeEpochByTabRef.current.get(tabId) ?? runtime?.epoch);
    const promptEpoch = currentState.promptEpoch;
    const { display, submit } = normalizeTurnSubmit(displayText, submitText);
    const original = originalText?.trim() ?? "";
    bumpCancelHydrateSeq(tabId);
    if (currentState.hydrateReason === "rewind") dispatchTo(tabId, { type: "hydrate_done" });
    dispatchTo(tabId, { type: "user", text: displayText, submitText: display !== submit ? submit : undefined, seq, submissionId });
    invalidateCache();
    try {
      const submitPromise = initialGoal
        ? app.SubmitInitialGoalToTabWithID(
            tabId,
            initialGoal.goal,
            structured?.display.trim() || display,
            structured?.input.trim() || submit,
            structured?.invocations ?? [],
            initialGoal.collaborationMode,
            initialGoal.toolApprovalMode,
            submissionId,
          )
        : structured
        ? app.SubmitInvocationsToTabWithID(tabId, structured.display.trim(), structured.input.trim(), structured.invocations, submissionId)
        : original
        ? app.SubmitEditedDisplayToTabWithID(tabId, display, submit, original, submissionId)
        : display !== submit ? app.SubmitDisplayToTabWithID(tabId, display, submit, submissionId) : app.SubmitToTabWithID(tabId, submit, submissionId);
      if (initialGoal) {
        const drained = await submitPromise;
        dispatchTo(tabId, { type: "send_confirmed", submissionId });
        const ids = Array.isArray(drained) ? drained : [];
        if (ids.length) dispatchTo(tabId, { type: "approval_drained", ids, epoch: promptEpoch });
        return;
      }
      void submitPromise.then(
        () => dispatchTo(tabId, { type: "send_confirmed", submissionId }),
        (error) => dispatchTo(tabId, { type: "send_failed", submissionId, error: `Send failed: ${error instanceof Error ? error.message : String(error)}` }),
      );
    } catch (error) {
      dispatchTo(tabId, { type: "send_failed", submissionId, error: `Send failed: ${error instanceof Error ? error.message : String(error)}` });
      throw error;
    }
  }, [bumpCancelHydrateSeq, dispatchTo]);

  const recoverDeliveryToTab = useCallback(async (tabId: string, displayText: string, submitText = displayText) => {
    if (!tabId) throw new Error(t("composer.workspaceStarting"));
    const currentState = getOrCreateState(statesRef.current, tabId);
    const runtime = currentState.meta?.runtime;
    if (currentState.meta && !runtimeReadyForSubmit(currentState.meta)) {
      throw new Error(runtime?.issue?.message || currentState.meta.startupErr || t("composer.workspaceStarting"));
    }
    const seq = currentState.seq;
    const submissionId = createTurnSubmissionId(tabId, currentState.sessionGen, seq, runtimeEpochByTabRef.current.get(tabId) ?? runtime?.epoch);
    const display = displayText.trim();
    const submit = submitText.trim();
    dispatchTo(tabId, { type: "user", text: displayText, submitText: display !== submit ? submit : undefined, seq, submissionId, deliveryRecovery: true });
    invalidateCache();
    try {
      void app.SubmitDeliveryRecoveryToTabWithID(tabId, display, submit, submissionId).then(
        () => dispatchTo(tabId, { type: "send_confirmed", submissionId }),
        (error) => dispatchTo(tabId, { type: "send_failed", submissionId, error: `Send failed: ${error instanceof Error ? error.message : String(error)}` }),
      );
    } catch (error) {
      dispatchTo(tabId, { type: "send_failed", submissionId, error: `Send failed: ${error instanceof Error ? error.message : String(error)}` });
      throw error;
    }
  }, [dispatchTo]);

  const send = useCallback((displayText: string, submitText = displayText) => {
    const tabId = activeTabIdRef.current ?? activeTabId;
    if (tabId) {
      return sendToTab(tabId, displayText, submitText);
    }
    const snapshotAt = promptEventClock();
    return activeTabFromBackend().then((active) => {
      if (!active?.id) throw new Error(t("composer.workspaceStarting"));
      setActiveTabId(active.id);
      activeTabIdRef.current = active.id;
      confirmBackendActiveTab(active.id);
      dispatchRuntimeStatusForTab(active.id, active, snapshotAt);
      return sendToTab(active.id, displayText, submitText);
    });
  }, [activeTabFromBackend, activeTabId, confirmBackendActiveTab, dispatchRuntimeStatusForTab, sendToTab]);

  const runShellForTab = useCallback(async (tabId: string, command: string) => {
    if (!tabId) throw new Error(t("composer.workspaceStarting"));
    const currentState = getOrCreateState(statesRef.current, tabId);
    const submissionId = createTurnSubmissionId(tabId, currentState.sessionGen, currentState.seq, runtimeEpochByTabRef.current.get(tabId) ?? currentState.meta?.runtime?.epoch);
    dispatchTo(tabId, { type: "user", text: `!${command}`, seq: currentState.seq, submissionId });
    try {
      await app.RunShellForTab(tabId, command);
      dispatchTo(tabId, { type: "send_confirmed", submissionId });
    } catch (error) {
      dispatchTo(tabId, { type: "send_failed", submissionId, error: `Command failed: ${error instanceof Error ? error.message : String(error)}` });
      throw error;
    }
  }, [dispatchTo]);

  const runShell = useCallback(async (command: string) => {
    if (!activeTabId) throw new Error(t("composer.workspaceStarting"));
    await runShellForTab(activeTabId, command);
  }, [activeTabId, runShellForTab]);

  const steerForTab = useCallback(async (tabId: string, text: string) => {
    if (!tabId) throw new Error(t("composer.workspaceStarting"));
    // Durable steer first: body is on disk before admission. Rejected steers
    // become follow-ups automatically (disposition queued_followup).
    const receipt = await app.EnqueueInboxSteer(tabId, text, text, "");
    if (receipt?.error) throw new Error(receipt.error);
    if (receipt?.disposition && receipt.disposition !== "steer_accepted") {
      throw new Error(inboxSteerQueuedMessage(getLocale()));
    }
  }, []);

  const steer = useCallback(async (text: string) => {
    if (!activeTabId) throw new Error(t("composer.workspaceStarting"));
    await steerForTab(activeTabId, text);
  }, [activeTabId, steerForTab]);

  const notice = useCallback((text: string, level: "info" | "warn" = "info") => {
    if (!activeTabId) return;
    dispatchTo(activeTabId, { type: "local_notice", level, text });
  }, [activeTabId, dispatchTo]);

  // Extension form dismissed/submitted locally: hide the surface. The backend
  // round-trip (SubmitExtensionForm) lives in App.tsx, which owns the toast
  // context used for error reporting.
  const dismissExtensionForm = useCallback(() => {
    if (!activeTabId) return;
    dispatchTo(activeTabId, { type: "clearExtensionForm" });
  }, [activeTabId, dispatchTo]);

  // The App drained the queued extension notifications into the toast system.
  const drainExtensionNotifications = useCallback(() => {
    if (!activeTabId) return;
    dispatchTo(activeTabId, { type: "extension_notifications_drained" });
  }, [activeTabId, dispatchTo]);

  const cancelTab = useCallback((tabId: string, inboxItemIDs: string[] = []) => {
    const cancelHydrateGeneration = bumpCancelHydrateSeq(tabId);
    const cancelRequest = inboxItemIDs.length > 0
      ? app.CancelTabWithInboxItems(tabId, inboxItemIDs)
      : app.CancelTab(tabId);
    cancelRequest
      .then(() => scheduleCancelReconcile(tabId, 0, cancelHydrateGeneration))
      .catch((error) => {
        dispatchTo(tabId, { type: "local_notice", level: "warn", text: formatInboxCancelError(error, getLocale()) });
      });
  }, [bumpCancelHydrateSeq, dispatchTo, scheduleCancelReconcile]);

  const cancel = useCallback((inboxItemIDs: string[] = []): string | undefined => {
    const cur = stateRef.current;
    const tabId = activeTabId;
    if (cur.running && cur.pendingUser !== undefined) {
      const text = cur.pendingUser;
      if (tabId) {
        dispatchTo(tabId, { type: "unsend" });
        cancelTab(tabId, inboxItemIDs);
      }
      return text;
    }
    if (tabId) {
      dispatchTo(tabId, { type: "cancel_requested" });
      cancelTab(tabId, inboxItemIDs);
    }
    return undefined;
  }, [activeTabId, cancelTab, dispatchTo]);

  const approve = useCallback((id: string, allow: boolean, session: boolean, persist: boolean) => {
    if (!activeTabId) return;
    const tabId = activeTabId;
    // Pin the failure callback to the prompt-id epoch the RPC was issued in:
    // if a controller rebuild lands while the call is in flight, a late
    // failure must not undo bookkeeping the NEW controller wrote for the same
    // numeric id (#6432 round 4).
    const epoch = statesRef.current.get(tabId)?.promptEpoch ?? 0;
    dispatchTo(tabId, { type: "clearApproval" });
    app.ApproveTab(tabId, id, allow, session, persist).catch(() => {
      // The backend never actually resolved this prompt — undo the optimistic
      // tombstone and ask it to replay, so the approval card can come back
      // instead of being silently lost forever (#6432 round 3).
      dispatchTo(tabId, { type: "submit_prompt_failed", id, epoch });
      replayPendingPromptsForActiveTab(tabId);
    });
  }, [activeTabId, dispatchTo]);

  const resolvePlanDecision = useCallback((id: string, action: "start_execution" | "revise_plan" | "exit_plan") => {
    if (!activeTabId) return;
    const tabId = activeTabId;
    const epoch = statesRef.current.get(tabId)?.promptEpoch ?? 0;
    dispatchTo(tabId, { type: "clearApproval" });
    const request = typeof app.ResolvePlanDecisionTab === "function"
      ? app.ResolvePlanDecisionTab(tabId, id, action)
      : app.ApproveTab(tabId, id, action === "start_execution", false, false);
    request.catch(() => {
      dispatchTo(tabId, { type: "submit_prompt_failed", id, epoch });
      replayPendingPromptsForActiveTab(tabId);
    });
  }, [activeTabId, dispatchTo]);

  const resolveRecovery = useCallback((id: string, action: "continue" | "continue_task" | "revise" | "stop", feedback = "") => {
    if (!activeTabId) return;
    const tabId = activeTabId;
    const epoch = statesRef.current.get(tabId)?.promptEpoch ?? 0;
    dispatchTo(tabId, { type: "clearApproval" });
    app.ResolveRecoveryTab(tabId, id, action, feedback).catch(() => {
      dispatchTo(tabId, { type: "submit_prompt_failed", id, epoch });
      replayPendingPromptsForActiveTab(tabId);
    });
  }, [activeTabId, dispatchTo]);

  const answerQuestion = useCallback((id: string, answers: QuestionAnswer[]) => {
    if (!activeTabId) return;
    const tabId = activeTabId;
    const epoch = statesRef.current.get(tabId)?.promptEpoch ?? 0;
    dispatchTo(tabId, { type: "clearAsk" });
    app.AnswerQuestionForTab(tabId, id, answers).catch(() => {
      dispatchTo(tabId, { type: "submit_prompt_failed", id, epoch });
      replayPendingPromptsForActiveTab(tabId);
    });
  }, [activeTabId, dispatchTo]);

  const setControllerMode = useCallback((mode: Mode): Promise<void> => {
    if (!activeTabId) return Promise.resolve();
    const tabId = activeTabId;
    const epoch = statesRef.current.get(tabId)?.promptEpoch ?? 0;
    return app.SetModeForTab(tabId, mode).then((drained) => {
      // Only dismiss the approvals the backend reports it actually
      // auto-allowed. Fresh prompts (plan/memory/sandbox escape) survive a
      // yolo switch backend-side and must stay visible (#6432 round 4).
      const ids = Array.isArray(drained) ? drained : [];
      if (ids.length) dispatchTo(tabId, { type: "approval_drained", ids, epoch });
    }).catch(() => {});
  }, [activeTabId, dispatchTo]);

  const setCollaborationModeForTab = useCallback(async (tabId: string, mode: CollaborationMode): Promise<void> => {
    if (!tabId) return;
    await app.SetCollaborationModeForTab(tabId, mode).catch(() => {});
    await refreshMetaForTab(tabId);
  }, [refreshMetaForTab]);

  const setCollaborationMode = useCallback(async (mode: CollaborationMode): Promise<void> => {
    if (!activeTabId) return;
    await setCollaborationModeForTab(activeTabId, mode);
  }, [activeTabId, setCollaborationModeForTab]);

  const setToolApprovalModeForTab = useCallback(async (tabId: string, mode: ToolApprovalMode): Promise<void> => {
    if (!tabId) return;
    const epoch = statesRef.current.get(tabId)?.promptEpoch ?? 0;
    // Same contract as setControllerMode: the backend reports which pending
    // approvals the new posture auto-allowed; anything else is still pending
    // there (fresh prompts; ask-rule approvals under auto) and stays visible.
    const drained = await app.SetToolApprovalModeForTab(tabId, mode).catch(() => undefined);
    const ids = Array.isArray(drained) ? drained : [];
    if (ids.length) dispatchTo(tabId, { type: "approval_drained", ids, epoch });
    await refreshMetaForTab(tabId);
  }, [dispatchTo, refreshMetaForTab]);

  const setToolApprovalMode = useCallback(async (mode: ToolApprovalMode): Promise<void> => {
    if (!activeTabId) return;
    await setToolApprovalModeForTab(activeTabId, mode);
  }, [activeTabId, setToolApprovalModeForTab]);

  const setComposerProfileForTab = useCallback(async (
    tabId: string,
    collaborationMode: CollaborationMode,
    toolApprovalMode: ToolApprovalMode,
    goal: string,
    options?: { propagateError?: boolean },
  ): Promise<boolean> => {
    if (!tabId) return false;
    const state = statesRef.current.get(tabId);
    const promptEpoch = state?.promptEpoch ?? 0;
    const key = composerProfileApplicationKey(
      runtimeEpochByTabRef.current.get(tabId) ?? state?.meta?.runtime?.epoch,
      collaborationMode,
      toolApprovalMode,
      goal,
    );
    if (appliedComposerProfileByTabRef.current.get(tabId) === key) return true;
    const existing = composerProfileInFlightByTabRef.current.get(tabId);
    if (existing?.key === key) return existing.promise;

    const lifecycle = composerProfileLifecycleByTabRef.current.get(tabId) ?? 0;
    const previous = composerProfileQueueByTabRef.current.get(tabId) ?? Promise.resolve();
    const promise = previous.then(async () => {
      if ((composerProfileLifecycleByTabRef.current.get(tabId) ?? 0) !== lifecycle) return false;
      if (appliedComposerProfileByTabRef.current.get(tabId) === key) return true;
      let drained: string[] | void;
      try {
        drained = await app.SetComposerProfileForTab(
          tabId,
          collaborationMode,
          toolApprovalMode,
          goal,
        );
      } catch (error) {
        if ((composerProfileLifecycleByTabRef.current.get(tabId) ?? 0) === lifecycle) {
          await refreshMetaForTab(tabId);
        }
        if (options?.propagateError) throw error;
        return false;
      }
      if ((composerProfileLifecycleByTabRef.current.get(tabId) ?? 0) !== lifecycle) return false;
      appliedComposerProfileByTabRef.current.set(tabId, key);
      const ids = Array.isArray(drained) ? drained : [];
      if (ids.length) dispatchTo(tabId, { type: "approval_drained", ids, epoch: promptEpoch });
      await refreshMetaForTab(tabId);
      return true;
    });
    const tail = promise.then(() => {}, () => {});
    composerProfileQueueByTabRef.current.set(tabId, tail);
    composerProfileInFlightByTabRef.current.set(tabId, { key, promise });
    try {
      return await promise;
    } finally {
      const current = composerProfileInFlightByTabRef.current.get(tabId);
      if (current?.promise === promise) composerProfileInFlightByTabRef.current.delete(tabId);
      if (composerProfileQueueByTabRef.current.get(tabId) === tail) {
        composerProfileQueueByTabRef.current.delete(tabId);
      }
    }
  }, [dispatchTo, refreshMetaForTab]);

  const setGoalForTab = useCallback(async (tabId: string, goal: string): Promise<void> => {
    if (!tabId) return;
    // Propagate activation failures so the first Goal turn (especially structured
    // Skill submit) can abort instead of executing without an active Goal.
    try {
      await app.SetGoalForTab(tabId, goal);
    } finally {
      await refreshMetaForTab(tabId);
    }
  }, [refreshMetaForTab]);

  const setGoal = useCallback(async (goal: string): Promise<void> => {
    if (!activeTabId) return;
    await setGoalForTab(activeTabId, goal);
  }, [activeTabId, setGoalForTab]);

  const clearGoalForTab = useCallback(async (tabId: string): Promise<void> => {
    if (!tabId) return;
    try {
      await app.ClearGoalForTab(tabId);
    } finally {
      await refreshMetaForTab(tabId);
    }
  }, [refreshMetaForTab]);

  const clearGoal = useCallback(async (): Promise<void> => {
    if (!activeTabId) return;
    await clearGoalForTab(activeTabId);
  }, [activeTabId, clearGoalForTab]);

  const resumeGoalForTab = useCallback(async (tabId: string): Promise<boolean> => {
    if (!tabId) return false;
    try {
      const resumed = await app.ResumeGoalForTab(tabId);
      await refreshMetaForTab(tabId);
      return resumed;
    } catch {
      return false;
    }
  }, [refreshMetaForTab]);

  const resumeGoal = useCallback(async (): Promise<boolean> => {
    if (!activeTabId) return false;
    return resumeGoalForTab(activeTabId);
  }, [activeTabId, resumeGoalForTab]);

  const pauseGoalForTab = useCallback(async (tabId: string): Promise<boolean> => {
    if (!tabId) return false;
    try {
      const paused = await app.PauseGoalForTab(tabId);
      await refreshMetaForTab(tabId);
      return paused;
    } catch {
      return false;
    }
  }, [refreshMetaForTab]);

  const pauseGoal = useCallback(async (): Promise<boolean> => {
    if (!activeTabId) return false;
    return pauseGoalForTab(activeTabId);
  }, [activeTabId, pauseGoalForTab]);

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
      void refreshMetaForTab(tabId);
      app.ContextUsageForTab(tabId).then((context) => dispatchTo(tabId, { type: "context", context })).catch(() => {});
      void refreshCheckpoints(tabId);
    }
  }, [activeTabId, bumpCheckpointRefreshSeq, bumpSessionLoadSeq, dispatchTo, loadSessionDataForTab, refreshCheckpoints, refreshMetaForTab, waitForTabReady]);

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
  const resumeSession = useCallback(async (path: string, tabId?: string, navigationIntentSeq?: number) => {
    const targetTabId = tabId || activeTabId;
    if (!targetTabId) return;
    const navigationSeq = navigationIntentSeq ?? beginActiveNavigation();
    if (tabId) await waitForTabReady(tabId);
    else if (!(await waitForBackendActiveTab(targetTabId))) return;
    if (!navigationCompletionCurrent(navigationSeq, "session.resume", targetTabId)) return;
    const seq = bumpSessionLoadSeq(targetTabId);
    dispatchTo(targetTabId, { type: "hydrate_start", reason: "resume-session" });
    let page: HistoryPage;
    try {
      page = tabId
        ? await app.ResumeSessionPageForTab(tabId, path, HISTORY_PAGE_TURNS)
        : await app.ResumeSessionPage(path, HISTORY_PAGE_TURNS);
    } catch (err) {
      if (isNavigationIntentCurrent(navigationSeq) && sessionLoadCurrent(targetTabId, seq)) {
        dispatchTo(targetTabId, { type: "hydrate_error", reason: "resume-session", error: errorMessage(err) });
        dispatchTo(targetTabId, { type: "local_notice", level: "warn", text: `${t("history.failedOpenSession")}: ${errorMessage(err)}` });
      }
      return;
    }
    if (!navigationCompletionCurrent(navigationSeq, "session.resume", targetTabId) || !sessionLoadCurrent(targetTabId, seq)) return;
    dispatchTo(targetTabId, { type: "reset" });
    dispatchTo(targetTabId, { type: "history_page", page, mode: "replace" });
    dispatchTo(targetTabId, { type: "hydrate_done" });
    await refreshMetaOnlyForTab(targetTabId);
    if (!isNavigationIntentCurrent(navigationSeq) || !sessionLoadCurrent(targetTabId, seq)) return;
    app.ContextUsageForTab(targetTabId).then((context) => dispatchTo(targetTabId, { type: "context", context })).catch(() => {});
    void refreshCheckpoints(targetTabId);
  }, [activeTabId, beginActiveNavigation, bumpSessionLoadSeq, dispatchTo, isNavigationIntentCurrent, navigationCompletionCurrent, refreshCheckpoints, refreshMetaOnlyForTab, sessionLoadCurrent, waitForBackendActiveTab, waitForTabReady]);

  const openChannelSession = useCallback(async (path: string, tabId: string, navigationIntentSeq?: number) => {
    if (!tabId) return;
    const navigationSeq = navigationIntentSeq ?? beginActiveNavigation();
    await waitForTabReady(tabId);
    if (!navigationCompletionCurrent(navigationSeq, "session.channel", tabId)) return;
    const seq = bumpSessionLoadSeq(tabId);
    dispatchTo(tabId, { type: "hydrate_start", reason: "resume-session" });
    let page: HistoryPage;
    try {
      page = await app.OpenChannelSessionPageForTab(tabId, path, HISTORY_PAGE_TURNS);
    } catch (err) {
      if (isNavigationIntentCurrent(navigationSeq) && sessionLoadCurrent(tabId, seq)) {
        dispatchTo(tabId, { type: "hydrate_error", reason: "resume-session", error: errorMessage(err) });
        dispatchTo(tabId, { type: "local_notice", level: "warn", text: `${t("history.failedOpenSession")}: ${errorMessage(err)}` });
      }
      return;
    }
    if (!navigationCompletionCurrent(navigationSeq, "session.channel", tabId) || !sessionLoadCurrent(tabId, seq)) return;
    dispatchTo(tabId, { type: "reset" });
    dispatchTo(tabId, { type: "history_page", page, mode: "replace" });
    dispatchTo(tabId, { type: "hydrate_done" });
    await refreshMetaOnlyForTab(tabId);
    if (!isNavigationIntentCurrent(navigationSeq) || !sessionLoadCurrent(tabId, seq)) return;
    app.ContextUsageForTab(tabId).then((context) => dispatchTo(tabId, { type: "context", context })).catch(() => {});
    void refreshCheckpoints(tabId);
  }, [beginActiveNavigation, bumpSessionLoadSeq, dispatchTo, isNavigationIntentCurrent, navigationCompletionCurrent, refreshCheckpoints, refreshMetaOnlyForTab, sessionLoadCurrent, waitForTabReady]);

  const previewSession = useCallback(async (path: string): Promise<HistoryMessage[]> => asArray<HistoryMessage>(await app.PreviewSession(path).catch(() => [])), []);
  const deleteSession = useCallback((path: string) => app.DeleteSession(path).finally(() => invalidateCache()), []);
  const restoreSession = useCallback((path: string) => app.RestoreSession(path).catch(() => {}).finally(() => invalidateCache()), []);
  const purgeTrashedSession = useCallback((path: string) => app.PurgeTrashedSession(path).catch(() => {}).finally(() => invalidateCache()), []);
  const renameSession = useCallback((path: string, title: string) => app.RenameSession(path, title).catch(() => {}).finally(() => invalidateCache()), []);
  const refreshMeta = useCallback(async () => {
    if (!activeTabId) return;
    invalidateSharedQuery("MetaForTab", [activeTabId]);
    await refreshMetaForTab(activeTabId);
  }, [activeTabId, refreshMetaForTab]);

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

  const enqueueModelSwitch = useCallback((tabId: string, name: string, fallbackBalance?: BalanceInfo) => {
    let queue = modelSwitchQueueByTab.current.get(tabId);
    if (!queue) {
      queue = { running: false, fallbackBalance };
      modelSwitchQueueByTab.current.set(tabId, queue);
    }
    const queueState = queue;

    return new Promise<ModelSwitchQueueResult>((resolve, reject) => {
      const request: ModelSwitchQueueRequest = { name, resolve, reject };
      const run = (next: ModelSwitchQueueRequest) => {
        queueState.running = true;
        void Promise.resolve()
          .then(() => app.SetModelForTab(tabId, next.name))
          .then(
            () => next.resolve("applied"),
            (err) => next.reject(err),
          )
          .finally(() => {
            if (modelSwitchQueueByTab.current.get(tabId) !== queueState) return;
            const pending = queueState.pending;
            queueState.pending = undefined;
            if (pending) {
              run(pending);
              return;
            }
            queueState.running = false;
            modelSwitchQueueByTab.current.delete(tabId);
          });
      };

      if (queueState.running) {
        queueState.pending?.resolve("superseded");
        queueState.pending = request;
        return;
      }
      run(request);
    });
  }, []);

  const setModel = useCallback(async (name: string) => {
    if (!activeTabId) return false;
    const tabId = activeTabId;
    const switchSeq = (modelSwitchSeqByTab.current.get(tabId) ?? 0) + 1;
    const successVersion = modelSwitchSuccessVersionByTab.current.get(tabId) ?? 0;
    const existingQueue = modelSwitchQueueByTab.current.get(tabId);
    // Every attempt in one queued burst shares the balance that was visible
    // before the first switch cleared it. Otherwise a later queued failure
    // captures the placeholder and cannot restore the outgoing provider.
    const fallbackBalance = existingQueue
      ? existingQueue.fallbackBalance
      : statesRef.current.get(tabId)?.balance;
    modelSwitchSeqByTab.current.set(tabId, switchSeq);
    // Hide the outgoing provider's wallet as soon as the user starts a hot
    // switch. If the rebuild fails, the catch path re-queries the still-active
    // provider and restores its balance.
    clearBalanceForTab(tabId);
    try {
      const result = await enqueueModelSwitch(tabId, name, fallbackBalance);
      if (result === "superseded") return false;
      modelSwitchSuccessVersionByTab.current.set(
        tabId,
        (modelSwitchSuccessVersionByTab.current.get(tabId) ?? 0) + 1,
      );
    } catch (err) {
      if (modelSwitchSeqByTab.current.get(tabId) !== switchSeq) return false;
      dispatchTo(tabId, { type: "local_notice", level: "warn", text: modelSwitchNoticeText(err) });
      const olderSwitchSucceeded =
        (modelSwitchSuccessVersionByTab.current.get(tabId) ?? 0) !== successVersion;
      // Restore the known balance only when no older overlapping switch
      // completed after this attempt began. Otherwise the backend now owns a
      // different provider and the refresh below must establish its balance.
      if (fallbackBalance && !olderSwitchSucceeded) {
        dispatchTo(tabId, { type: "balance", balance: fallbackBalance });
      }
      void refreshBalanceForTab(tabId);
      // A superseded success deliberately skips its own UI reconciliation.
      // If this latest queued switch then fails, reconcile the model metadata
      // to the provider that actually became active in the backend.
      if (olderSwitchSucceeded) await refreshMetaForTab(tabId);
      return false;
    }
    if (modelSwitchSeqByTab.current.get(tabId) !== switchSeq) return false;
    void refreshBalanceForTab(tabId);
    await refreshMetaForTab(tabId);
    return modelSwitchSeqByTab.current.get(tabId) === switchSeq;
  }, [activeTabId, clearBalanceForTab, dispatchTo, enqueueModelSwitch, refreshBalanceForTab, refreshMetaForTab]);

  const setEffort = useCallback(async (level: string) => {
    if (!activeTabId) return;
    try {
      await app.SetEffortForTab(activeTabId, level);
    } catch (err) {
      dispatchTo(activeTabId, { type: "local_notice", level: "warn", text: effortSwitchNoticeText(err) });
      return;
    }
    await refreshMetaForTab(activeTabId);
  }, [activeTabId, dispatchTo, refreshMetaForTab]);

  const setTokenMode = useCallback(async (mode: TokenMode): Promise<boolean> => {
    if (!activeTabId) return false;
    try {
      await app.SetTokenModeForTab(activeTabId, mode);
    } catch (err) {
      dispatchTo(activeTabId, { type: "local_notice", level: "warn", text: tokenModeSwitchNoticeText(err) });
      return false;
    }
    await refreshMetaForTab(activeTabId);
    return true;
  }, [activeTabId, dispatchTo, refreshMetaForTab]);

  const cancelJob = useCallback(async (jobID: string): Promise<boolean> => {
    const tabId = activeTabId;
    if (!tabId || !jobID.trim()) return false;
    try {
      const cancelled = await app.CancelJobForTab(tabId, jobID);
      const jobs = asArray(await app.JobsForTab(tabId));
      dispatchTo(tabId, { type: "jobs", jobs });
      await refreshMetaForTab(tabId);
      return cancelled;
    } catch {
      dispatchTo(tabId, { type: "local_notice", level: "warn", text: t("status.jobStopFailed") });
      return false;
    }
  }, [activeTabId, dispatchTo, refreshMetaForTab]);

  const fetchMemory = useCallback((): Promise<MemoryView> =>
    app.Memory().catch(() => ({
      docs: [], facts: [], archives: [], scopes: [], instructionDiagnostics: [], conflicts: [],
      lastRecall: { query: "", hits: [], omitted: 0, charBudget: 0, usedChars: 0 },
      storeDir: "", available: false,
    })), []);
  const remember = useCallback(async (scope: string, note: string) => { await app.Remember(scope, note).catch(() => {}); }, []);
  const forget = useCallback(async (name: string) => { await app.Forget(name).catch(() => {}); }, []);
  const saveDoc = useCallback(async (path: string, body: string) => { await app.SaveDoc(path, body).catch(() => {}); }, []);

  type RewindOutcome = {
    ok: boolean;
    transactionId?: string;
    undoAvailable?: boolean;
    written?: string[];
    deleted?: string[];
  };

  const rewindForTabDetailed = useCallback(async (sourceTabId: string, turn: number, scope: string): Promise<RewindOutcome> => {
    if (!sourceTabId) return { ok: false };
    const forkNavigationSeq = activeNavigationSeqRef.current;
    await waitForTabReady(sourceTabId);
    const actionScope = (["fork", "summ-from", "summ-upto", "conversation", "code", "both"].includes(scope) ? scope : "both") as MessageActionScope;
    dispatchTo(sourceTabId, { type: "message_action_start", action: { turn, scope: actionScope } });
    dispatchTo(sourceTabId, { type: "local_notice", level: "info", text: messageActionBusyText(actionScope) });
    try {
      if (actionScope === "fork") {
        const snapshotAt = promptEventClock();
        const tab = await app.ForkForTab(sourceTabId, turn);
        if (tab?.id) {
          const navigationUnchanged = activeNavigationSeqRef.current === forkNavigationSeq;
          const activateFork = tab.active && navigationUnchanged && activeTabIdRef.current === sourceTabId;
          if (!activateFork) {
            dispatchTo(tab.id, { type: "optimistic_meta", meta: metaFromTab(tab, statesRef.current.get(tab.id)?.meta) });
            dispatchRuntimeStatusForTab(tab.id, tab, snapshotAt);
            const currentTabId = activeTabIdRef.current;
            if (tab.active) {
              await reassertVisibleTabAfterStaleNavigation("tab.fork", tab.id);
            } else if (!tab.active && navigationUnchanged && currentTabId === sourceTabId) {
              await syncActiveTabFromBackend(false, true);
            }
            addBreadcrumb("tab.fork", `stale completion ${tab.id} current=${currentTabId ?? ""}`);
            return { ok: true };
          }
          beginActiveNavigation();
          setActiveTabId(tab.id);
          activeTabIdRef.current = tab.id;
          confirmBackendActiveTab(tab.id);
          dispatchRuntimeStatusForTab(tab.id, tab, snapshotAt);
          await waitForTabReady(tab.id);
          await loadSessionDataForTab(tab.id, true);
          await reconcileTabRuntime(tab.id, { hydrateSessionData: false });
        } else {
          await syncActiveTabFromBackend(true);
        }
        return { ok: true };
      }

      let outcome: RewindOutcome = { ok: true };
      if (actionScope === "summ-from") await app.SummarizeFromForTab(sourceTabId, turn);
      else if (actionScope === "summ-upto") await app.SummarizeUpToForTab(sourceTabId, turn);
      else {
        const { commitRewindWithPreview } = await import("./rewindCommit");
        const result = await commitRewindWithPreview(sourceTabId, turn, actionScope);
        if (!result?.ok) {
          const detail = result?.error
            || (result?.conflicts?.length ? result.conflicts.join("; ") : "")
            || "rewind failed";
          dispatchTo(sourceTabId, { type: "local_notice", level: "warn", text: detail });
          return { ok: false, written: result?.written, deleted: result?.deleted };
        }
        outcome = result as RewindOutcome;
      }

      await loadSessionDataForTab(sourceTabId, true, "rewind");
      return outcome;
    } catch {
      return { ok: false };
    } finally {
      dispatchTo(sourceTabId, { type: "message_action_done" });
    }
  }, [beginActiveNavigation, confirmBackendActiveTab, dispatchRuntimeStatusForTab, dispatchTo, loadSessionDataForTab, reassertVisibleTabAfterStaleNavigation, reconcileTabRuntime, syncActiveTabFromBackend, waitForTabReady]);

  const rewindForTab = useCallback(async (sourceTabId: string, turn: number, scope: string): Promise<boolean> => {
    return (await rewindForTabDetailed(sourceTabId, turn, scope)).ok;
  }, [rewindForTabDetailed]);

  const rewind = useCallback(async (turn: number, scope: string): Promise<boolean> => {
    if (!activeTabId) return false;
    return rewindForTab(activeTabId, turn, scope);
  }, [activeTabId, rewindForTab]);

  const undoRewindForTab = useCallback(async (sourceTabId: string, transactionId: string): Promise<boolean> => {
    if (!sourceTabId || !transactionId) return false;
    try {
      const { undoCommittedRewind } = await import("./rewindCommit");
      const result = await undoCommittedRewind(sourceTabId, transactionId);
      if (!result?.ok) {
        const detail = result?.error || "undo rewind failed";
        dispatchTo(sourceTabId, { type: "local_notice", level: "warn", text: detail });
        return false;
      }
      await loadSessionDataForTab(sourceTabId, true, "rewind");
      return true;
    } catch (err) {
      dispatchTo(sourceTabId, {
        type: "local_notice",
        level: "warn",
        text: err instanceof Error ? err.message : String(err),
      });
      return false;
    }
  }, [dispatchTo, loadSessionDataForTab]);

  // Tab management: switch preserves per-tab state; open creates it.
  const switchTab = useCallback(async (tabId: string, optimisticTab?: TabMeta, navigationIntentSeq?: number): Promise<TabMeta[] | undefined> => {
    const navigationSeq = navigationIntentSeq ?? beginActiveNavigation();
    if (!navigationCompletionCurrent(navigationSeq, "tab.switch", tabId)) return undefined;
    const startedAt = Date.now();
    topicActivationSeqRef.current += 1;
    const switchRequestId = `fe-switch-${Date.now()}-${topicActivationSeqRef.current}`;
    noteActivationRequested(switchRequestId);
    const previousTabId = activeTabIdRef.current;
    const targetSessionPath = optimisticTab?.sessionPath ?? statesRef.current.get(tabId)?.meta?.sessionPath;
    const targetSessionRevision = optimisticTab?.sessionRevision ?? statesRef.current.get(tabId)?.meta?.sessionRevision;
    const targetSessionDigest = optimisticTab?.sessionDigest ?? statesRef.current.get(tabId)?.meta?.sessionDigest;
    const preserveCachedHistory = hasReusableCachedTranscript(statesRef.current.get(tabId), targetSessionPath, targetSessionRevision, targetSessionDigest);
    addBreadcrumb("tab.switch", `click ${tabId}`);
    setActiveTabId(tabId);
    activeTabIdRef.current = tabId;
    dispatchTo(tabId, { type: "backend_activation_start" });
    noteActivationStarted(switchRequestId, tabId);
    if (optimisticTab) {
      dispatchTo(tabId, { type: "optimistic_meta", meta: metaFromTab(optimisticTab, statesRef.current.get(tabId)?.meta) });
      const optimisticStatus = backendStatusFromRuntimeMeta(optimisticTab);
      if (optimisticStatus.running) dispatchTo(tabId, optimisticStatus);
    }
    dispatchTo(tabId, { type: "hydrate_start", reason: "switch-tab" });
    addBreadcrumb("tab.switch", `active-rendered ${tabId} ms=${Date.now() - startedAt}`);
    const backendActivation = app.SetActiveTab(tabId)
      .then(async () => {
        const navigationCurrent = isNavigationIntentCurrent(navigationSeq);
        if (!navigationCurrent || activeTabIdRef.current !== tabId) {
          const currentTabId = activeTabIdRef.current;
          noteActivationSettled(switchRequestId, "cancelled");
          await reassertVisibleTabAfterStaleNavigation("tab.switch", tabId);
          addBreadcrumb("tab.switch", `set-active-stale ${tabId} seq=${navigationSeq} current=${currentTabId ?? ""} ms=${Date.now() - startedAt}`);
          return false;
        }
        confirmBackendActiveTab(tabId);
        addBreadcrumb("tab.switch", `set-active-done ${tabId} ms=${Date.now() - startedAt}`);
        return true;
      })
      .catch((err) => {
        noteActivationSettled(switchRequestId, "failed", errorMessage(err));
        if (!isNavigationIntentCurrent(navigationSeq)) return false;
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
        if (!activated || !isNavigationIntentCurrent(navigationSeq)) {
          if (!activated) noteActivationSettled(switchRequestId, "failed", "backend activation did not complete");
          return undefined;
        }
        const tabs = await reconcileTabRuntime(tabId, { hydrateSessionData: false });
        if (!isNavigationIntentCurrent(navigationSeq)) return tabs;
        void loadSessionDataForTab(tabId, false, "switch-tab", {
          skipHistory: hasCachedLiveTurn(statesRef.current.get(tabId)),
          preserveCachedHistory,
          sessionPath: targetSessionPath,
          sessionRevision: targetSessionRevision,
          sessionDigest: targetSessionDigest,
        });
        noteActivationSettled(switchRequestId, "ready");
        return tabs;
      })
      .catch((err) => {
        noteActivationSettled(switchRequestId, "failed", errorMessage(err));
        if (isNavigationIntentCurrent(navigationSeq)) {
          dispatchTo(tabId, { type: "hydrate_error", reason: "switch-tab", error: errorMessage(err) });
        }
        return undefined;
      });
    return backendSwitch;
  }, [beginActiveNavigation, confirmBackendActiveTab, dispatchTo, isNavigationIntentCurrent, loadSessionDataForTab, navigationCompletionCurrent, reassertVisibleTabAfterStaleNavigation, reconcileTabRuntime, trackBackendActivation]);

  const openProjectTab = useCallback(async (workspaceRoot: string, topicId: string, navigationIntentSeq?: number): Promise<TabMeta> => {
    const navigationSeq = navigationIntentSeq ?? beginActiveNavigation();
    const snapshotAt = promptEventClock();
    const meta = await app.OpenProjectTab(workspaceRoot, topicId);
    if (!navigationCompletionCurrent(navigationSeq, "tab.open-project", meta.id)) {
      await reassertVisibleTabAfterStaleNavigation("tab.open-project", meta.id);
      return meta;
    }
    const prevItems = activeTabIdRef.current ? statesRef.current.get(activeTabIdRef.current)?.items : undefined;
    const prevState = statesRef.current.get(meta.id);
    const isNewTab = !prevState;
    const preserveCachedHistory = hasReusableCachedTranscript(prevState, meta.sessionPath, meta.sessionRevision, meta.sessionDigest);
    setActiveTabId(meta.id);
    activeTabIdRef.current = meta.id;
    confirmBackendActiveTab(meta.id);
    dispatchTo(meta.id, { type: "optimistic_meta", meta: metaFromTab(meta, statesRef.current.get(meta.id)?.meta) });
    dispatchRuntimeStatusForTab(meta.id, meta, snapshotAt);
    const load = loadSessionDataForTab(meta.id, isNewTab, "open-topic", {
      placeholderItems: isNewTab ? prevItems : undefined,
      preserveCachedHistory,
      sessionPath: meta.sessionPath,
      sessionRevision: meta.sessionRevision,
      sessionDigest: meta.sessionDigest,
    });
    if (isNewTab) void load.then(() => reconcileTabRuntime(meta.id, { hydrateSessionData: false })).catch(() => {});
    else void load;
    return meta;
  }, [beginActiveNavigation, confirmBackendActiveTab, dispatchRuntimeStatusForTab, dispatchTo, loadSessionDataForTab, navigationCompletionCurrent, reassertVisibleTabAfterStaleNavigation, reconcileTabRuntime]);

  const openGlobalTab = useCallback(async (topicId: string, navigationIntentSeq?: number): Promise<TabMeta> => {
    const navigationSeq = navigationIntentSeq ?? beginActiveNavigation();
    const snapshotAt = promptEventClock();
    const meta = await app.OpenGlobalTab(topicId);
    if (!navigationCompletionCurrent(navigationSeq, "tab.open-global", meta.id)) {
      await reassertVisibleTabAfterStaleNavigation("tab.open-global", meta.id);
      return meta;
    }
    const prevItems = activeTabIdRef.current ? statesRef.current.get(activeTabIdRef.current)?.items : undefined;
    const prevState = statesRef.current.get(meta.id);
    const isNewTab = !prevState;
    const preserveCachedHistory = hasReusableCachedTranscript(prevState, meta.sessionPath, meta.sessionRevision, meta.sessionDigest);
    setActiveTabId(meta.id);
    activeTabIdRef.current = meta.id;
    confirmBackendActiveTab(meta.id);
    dispatchTo(meta.id, { type: "optimistic_meta", meta: metaFromTab(meta, statesRef.current.get(meta.id)?.meta) });
    dispatchRuntimeStatusForTab(meta.id, meta, snapshotAt);
    const load = loadSessionDataForTab(meta.id, isNewTab, "open-topic", {
      placeholderItems: isNewTab ? prevItems : undefined,
      preserveCachedHistory,
      sessionPath: meta.sessionPath,
      sessionRevision: meta.sessionRevision,
      sessionDigest: meta.sessionDigest,
    });
    if (isNewTab) void load.then(() => reconcileTabRuntime(meta.id, { hydrateSessionData: false })).catch(() => {});
    else void load;
    return meta;
  }, [beginActiveNavigation, confirmBackendActiveTab, dispatchRuntimeStatusForTab, dispatchTo, loadSessionDataForTab, navigationCompletionCurrent, reassertVisibleTabAfterStaleNavigation, reconcileTabRuntime]);

  const openTopicSession = useCallback(async (scope: string, workspaceRoot: string, topicId: string, sessionPath: string, navigationIntentSeq?: number): Promise<TabMeta> => {
    const navigationSeq = navigationIntentSeq ?? beginActiveNavigation();
    const snapshotAt = promptEventClock();
    const meta = await app.OpenTopicSession(scope, workspaceRoot, topicId, sessionPath);
    if (!navigationCompletionCurrent(navigationSeq, "tab.open-session", meta.id)) {
      await reassertVisibleTabAfterStaleNavigation("tab.open-session", meta.id);
      return meta;
    }
    const prevItems = activeTabIdRef.current ? statesRef.current.get(activeTabIdRef.current)?.items : undefined;
    const prevState = statesRef.current.get(meta.id);
    const isNewTab = !prevState;
    const preserveCachedHistory = hasReusableCachedTranscript(prevState, meta.sessionPath, meta.sessionRevision, meta.sessionDigest);
    setActiveTabId(meta.id);
    activeTabIdRef.current = meta.id;
    confirmBackendActiveTab(meta.id);
    dispatchTo(meta.id, { type: "optimistic_meta", meta: metaFromTab(meta, statesRef.current.get(meta.id)?.meta) });
    dispatchRuntimeStatusForTab(meta.id, meta, snapshotAt);
    const load = loadSessionDataForTab(meta.id, isNewTab, "open-topic", {
      placeholderItems: isNewTab ? prevItems : undefined,
      preserveCachedHistory,
      sessionPath: meta.sessionPath,
      sessionRevision: meta.sessionRevision,
      sessionDigest: meta.sessionDigest,
    });
    if (isNewTab) void load.then(() => reconcileTabRuntime(meta.id, { hydrateSessionData: false })).catch(() => {});
    else void load;
    return meta;
  }, [beginActiveNavigation, confirmBackendActiveTab, dispatchRuntimeStatusForTab, dispatchTo, loadSessionDataForTab, navigationCompletionCurrent, reassertVisibleTabAfterStaleNavigation, reconcileTabRuntime]);

  const activateTopic = useCallback(async (scope: string, workspaceRoot: string, topicId: string, sessionPath = "", navigationIntentSeq?: number): Promise<TabMeta> => {
    const navigationSeq = navigationIntentSeq ?? beginActiveNavigation();
    const snapshotAt = promptEventClock();
    // Ticketed two-phase activation: the backend switches the visible surface
    // before returning the ticket; the controller build and tab prune finish
    // in the background and report through "topic:activation". Register the
    // pending ticket before the call so synchronously-emitted events match.
    topicActivationSeqRef.current += 1;
    const pending: PendingTopicActivation = {
      requestId: `fe-act-${Date.now()}-${topicActivationSeqRef.current}`,
      navigationSeq,
    };
    pendingTopicActivationRef.current = pending;
    noteActivationRequested(pending.requestId);
    const ticket = await app.StartTopicActivation({
      scope,
      workspaceRoot,
      topicId,
      sessionPath,
      requestId: pending.requestId,
    });
    const meta = ticket.meta;
    pending.tabId = ticket.tabId;
    if (pendingTopicActivationRef.current === pending && ticket.requestId) {
      if (ticket.requestId !== pending.requestId) aliasActivationRequest(pending.requestId, ticket.requestId);
      pending.requestId = ticket.requestId;
    }
    if (!navigationCompletionCurrent(navigationSeq, "topic.activate", meta.id)) {
      // A newer navigation started while the backend processed this
      // activation. Applying the stale result would flip the visible tab
      // away from the user's last click and — worse — the single-surface
      // prune below deletes every other tab's cached state, blanking the
      // surface the user is actually looking at. Last click wins: hand the
      // meta back for bookkeeping and leave the visible state to the newer
      // navigation. The backend supersedes this ticket (its terminal event
      // is ignored above: the pending slot belongs to the newer request).
      await reassertVisibleTabAfterStaleNavigation("topic.activate", meta.id);
      return meta;
    }
    // Save previous tab's items so the new tab can use them as a placeholder
    // during loading, avoiding a blank/Welcome flash before history arrives.
    const prevItems = activeTabIdRef.current ? statesRef.current.get(activeTabIdRef.current)?.items : undefined;
    pending.placeholderItems = prevItems;
    for (const id of Array.from(statesRef.current.keys())) {
      if (id !== meta.id) {
        invalidateProviderStateForTab(id);
        disposeComposerProfileState(id);
        statesRef.current.delete(id);
        releaseTranscriptState(id);
      }
    }
    setActiveTabId(meta.id);
    activeTabIdRef.current = meta.id;
    confirmBackendActiveTab(meta.id);
    noteActivationStarted(pending.requestId, meta.id);
    dispatchTo(meta.id, { type: "optimistic_meta", meta: metaFromTab(meta, statesRef.current.get(meta.id)?.meta) });
    dispatchRuntimeStatusForTab(meta.id, meta, snapshotAt);
    // The hydrate is driven by the activation's terminal "ready" event; until
    // then keep the loading surface up with the previous tab's items as the
    // placeholder (same no-flash behavior the immediate hydrate had).
    dispatchTo(meta.id, { type: "hydrate_start", reason: "open-topic", placeholderItems: prevItems });
    if (pending.terminal && pendingTopicActivationRef.current === pending) {
      // The terminal event beat the ticket resolution; process it now.
      handleTopicActivationEvent(pending.terminal);
    }
    return meta;
  }, [beginActiveNavigation, confirmBackendActiveTab, dispatchRuntimeStatusForTab, dispatchTo, disposeComposerProfileState, handleTopicActivationEvent, invalidateProviderStateForTab, navigationCompletionCurrent, reassertVisibleTabAfterStaleNavigation, releaseTranscriptState]);

  // Ensure a blank tab exists for the given scope — reuses an existing one
  // or creates a new tab, then loads its session data.
  const ensureBlankTab = useCallback(async (scope: string, workspaceRoot: string, navigationIntentSeq?: number): Promise<TabMeta> => {
    const navigationSeq = navigationIntentSeq ?? beginActiveNavigation();
    const snapshotAt = promptEventClock();
    const meta = await app.EnsureBlankTab(scope, workspaceRoot);
    if (!navigationCompletionCurrent(navigationSeq, "tab.ensure-blank", meta.id)) {
      await reassertVisibleTabAfterStaleNavigation("tab.ensure-blank", meta.id);
      return meta;
    }
    // EnsureBlankTab may return a tab id already present in local state.
    // Invalidate its old hydration and force a fresh history read, otherwise a
    // late request can restore orphaned tool cards from the prior session.
    bumpCheckpointRefreshSeq(meta.id);
    const isNewTab = !statesRef.current.has(meta.id);
    setActiveTabId(meta.id);
    activeTabIdRef.current = meta.id;
    confirmBackendActiveTab(meta.id);
    dispatchTo(meta.id, { type: "optimistic_meta", meta: metaFromTab(meta, statesRef.current.get(meta.id)?.meta) });
    dispatchRuntimeStatusForTab(meta.id, meta, snapshotAt);
    const load = loadSessionDataForTab(meta.id, true, "new-session", { sessionPath: meta.sessionPath });
    if (isNewTab) void load.then(() => reconcileTabRuntime(meta.id, { hydrateSessionData: false })).catch(() => {});
    else void load;
    return meta;
  }, [beginActiveNavigation, bumpCheckpointRefreshSeq, confirmBackendActiveTab, dispatchRuntimeStatusForTab, dispatchTo, loadSessionDataForTab, navigationCompletionCurrent, reassertVisibleTabAfterStaleNavigation, reconcileTabRuntime]);

  const ensureBlankSurface = useCallback(async (scope: string, workspaceRoot: string, navigationIntentSeq?: number): Promise<TabMeta> => {
    const navigationSeq = navigationIntentSeq ?? beginActiveNavigation();
    const snapshotAt = promptEventClock();
    const meta = await app.EnsureBlankSurface(scope, workspaceRoot);
    if (!navigationCompletionCurrent(navigationSeq, "surface.ensure-blank", meta.id)) {
      await reassertVisibleTabAfterStaleNavigation("surface.ensure-blank", meta.id);
      return meta;
    }
    for (const id of Array.from(statesRef.current.keys())) {
      if (id !== meta.id) {
        invalidateProviderStateForTab(id);
        disposeComposerProfileState(id);
        statesRef.current.delete(id);
        releaseTranscriptState(id);
      }
    }
    setActiveTabId(meta.id);
    activeTabIdRef.current = meta.id;
    confirmBackendActiveTab(meta.id);
    dispatchTo(meta.id, { type: "optimistic_meta", meta: metaFromTab(meta, statesRef.current.get(meta.id)?.meta) });
    dispatchRuntimeStatusForTab(meta.id, meta, snapshotAt);
    void loadSessionDataForTab(meta.id, true, "open-topic")
      .then(() => reconcileTabRuntime(meta.id, { hydrateSessionData: false }))
      .catch(() => {});
    return meta;
  }, [beginActiveNavigation, confirmBackendActiveTab, dispatchRuntimeStatusForTab, dispatchTo, disposeComposerProfileState, invalidateProviderStateForTab, loadSessionDataForTab, navigationCompletionCurrent, reassertVisibleTabAfterStaleNavigation, reconcileTabRuntime, releaseTranscriptState]);

  const createDeliveryWorktree = useCallback(async (workspaceRoot: string, navigationIntentSeq?: number): Promise<DeliveryWorktreeOpenResult> => {
    const navigationSeq = navigationIntentSeq ?? beginActiveNavigation();
    const snapshotAt = promptEventClock();
    const result = await app.CreateDeliveryWorktree(workspaceRoot);
    const meta = result.tab;
    if (!navigationCompletionCurrent(navigationSeq, "tab.delivery-worktree", meta.id)) {
      await reassertVisibleTabAfterStaleNavigation("tab.delivery-worktree", meta.id);
      return result;
    }
    const isNewTab = !statesRef.current.has(meta.id);
    setActiveTabId(meta.id);
    activeTabIdRef.current = meta.id;
    confirmBackendActiveTab(meta.id);
    dispatchTo(meta.id, { type: "optimistic_meta", meta: metaFromTab(meta, statesRef.current.get(meta.id)?.meta) });
    dispatchRuntimeStatusForTab(meta.id, meta, snapshotAt);
    const load = loadSessionDataForTab(meta.id, isNewTab, "open-topic");
    if (isNewTab) void load.then(() => reconcileTabRuntime(meta.id, { hydrateSessionData: false })).catch(() => {});
    else void load;
    return result;
  }, [beginActiveNavigation, confirmBackendActiveTab, dispatchRuntimeStatusForTab, dispatchTo, loadSessionDataForTab, navigationCompletionCurrent, reassertVisibleTabAfterStaleNavigation, reconcileTabRuntime]);

  const closeTab = useCallback(async (
    tabId: string,
    policy: "keep_running" | "stop_and_close" = "keep_running",
  ): Promise<boolean> => {
    if (tabId === activeTabIdRef.current) beginActiveNavigation();
    try {
      await app.CloseTabWithPolicy(tabId, policy);
      invalidateProviderStateForTab(tabId);
      disposeComposerProfileState(tabId);
      statesRef.current.delete(tabId);
      releaseTranscriptState(tabId);
      notifyLiveListeners(tabId);
      bump();
      if (tabId === activeTabId) await syncActiveTabFromBackend(false);
      return true;
    } catch {
      return false;
    }
  }, [activeTabId, beginActiveNavigation, bump, disposeComposerProfileState, invalidateProviderStateForTab, notifyLiveListeners, releaseTranscriptState, syncActiveTabFromBackend]);

  const reorderTabs = useCallback(async (tabIds: string[]) => {
    try {
      await app.ReorderTabs(tabIds);
    } catch { /* ignore */ }
  }, []);

  return {
    state: activeState,
    liveStore,
    activeTabId,
    send, sendToTab, recoverDeliveryToTab, runShell, runShellForTab, steer, steerForTab, notice, cancel, approve, resolvePlanDecision, resolveRecovery, answerQuestion, setControllerMode,
    dismissExtensionForm, drainExtensionNotifications,
    setCollaborationMode, setCollaborationModeForTab, setToolApprovalMode, setToolApprovalModeForTab, setComposerProfileForTab, setGoal, setGoalForTab, clearGoal, clearGoalForTab, resumeGoal, resumeGoalForTab, pauseGoal, pauseGoalForTab,
    newSession, clearSession, listSessions, listTrashedSessions, resumeSession, openChannelSession, previewSession, deleteSession, restoreSession, purgeTrashedSession, renameSession,
    loadOlderHistory,
    requestHistoryFullContent,
    refreshMeta, pickWorkspace, switchWorkspace, compact, rewind, rewindForTab, rewindForTabDetailed, undoRewindForTab, setModel, setEffort, setTokenMode, cancelJob,
    fetchMemory, remember, forget, saveDoc,
    switchTab, openProjectTab, openGlobalTab, openTopicSession, ensureBlankTab, activateTopic, ensureBlankSurface, createDeliveryWorktree, closeTab, reorderTabs,
    // Invalidate in-flight navigation completions (activateTopic's stale
    // guard) from outside the hook. The App-level navigation queue must call
    // this at ENQUEUE time: a queued click does not run — and so does not
    // advance this epoch — until the running request finishes, which would
    // let the running stale activation pass the guard and prune the state of
    // the surface the user just clicked.
    noteNavigationIntent: beginActiveNavigation,
    isNavigationIntentCurrent,
    syncActiveTab: syncActiveTabFromBackend,
  };
}
