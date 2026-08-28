// transcriptRows — the block-level virtual row model behind the transcript
// (Phase D of the session-switch/history refactor). The transcript renders as
// a flat list of virtual rows (user message, process-fold header, tool batch,
// answer, notice, turn actions, …) instead of the old hot/warm/cold turn
// layers. Everything here is pure: Transcript.tsx feeds items + fold state in
// and gets rows out, which keeps the model testable without a DOM.
//
// Fold semantics are hoisted out of the old per-instance TurnCollapse state
// into an explicit FoldMap keyed by segment: auto-open while running,
// auto-close on completion (unless the user toggled or the preference is
// "expanded"), and preference switches applying to folds already on screen.

import { isHostRecoveryGuidance } from "./hostRecoverySteer";
import { stableStringHash } from "./stableStringHash";
import { isBatchedReadOnlyTool, isSteerNoticeText, type ExtensionItem, type Item } from "./useController";
import { appendTurnActionCopyText } from "./turnActionCopy";
import { isCreationGroupableTool, toolGroupKind, type ToolGroupKind } from "../components/ToolGroup";
import type { ProcessFoldPreference } from "./processFoldPreference";
import type { ResolvedReasoningDisplayMode } from "./reasoningDisplayPreference";
import {
  estimateTranscriptRowGeometry,
  resolveReasoningLayoutVariant,
  resolveToolCardDefaultOpen,
  type TranscriptGeometryEnvironment,
  type TranscriptRowLayoutVariant,
} from "./transcriptRowGeometry";

export type UserItem = Extract<Item, { kind: "user" }>;
export type AssistantItem = Extract<Item, { kind: "assistant" }>;
export type ToolItem = Extract<Item, { kind: "tool" }>;
export type NoticeItem = Extract<Item, { kind: "notice" }>;
export type PhaseItem = Extract<Item, { kind: "phase" }>;
export type CompactionItem = Extract<Item, { kind: "compaction" }>;

// ── Live presence flags ───────────────────────────────────────────────────────
// The model only depends on PRESENCE of live text/reasoning (which rows exist
// and whether folds auto-open), never on the streaming content itself — the
// row components read the stream through LiveStreamContext. That keeps token
// updates out of the model rebuild path.

export interface TranscriptLiveFlags {
  id?: string;
  hasAnswerText: boolean;
  hasReasoning: boolean;
  reasoningComplete?: boolean;
}

export const NO_LIVE: TranscriptLiveFlags = { hasAnswerText: false, hasReasoning: false };

// ── Turn partitioning ─────────────────────────────────────────────────────────

export type TurnDisplayParts = {
  processItems: Item[];
  outsideItems: Array<NoticeItem | AssistantItem | ExtensionItem>;
};

function assistantHasVisibleAnswer(item: AssistantItem, live: TranscriptLiveFlags): boolean {
  if (item.text.trim() !== "") return true;
  return live.id === item.id && live.hasAnswerText;
}

// Splits a turn by channel, not by position: reasoning, tools, phases, info
// notices, and compaction cards are process material and fold; every assistant
// message with answer text is model output addressed to the user and stays
// outside the fold. Warnings must survive the fold auto-closing on completion,
// and steers are the user's own words — neither belongs to the model's work
// process.
//
// The turn is returned as ordered segments so the conversation keeps its real
// timeline: process that ran after an answer or steer opens a new segment
// (and thus a new fold) instead of being pulled ahead of it. Warn notices and
// delivery status cards stay visible but do not split the fold — a mid-turn
// warning is not a conversational boundary, and a delivery pause must keep its
// continue action reachable instead of collapsing with the process items.
export function partitionTurnItems(items: readonly Item[], live: TranscriptLiveFlags = NO_LIVE): TurnDisplayParts[] {
  const segments: TurnDisplayParts[] = [];
  let current: TurnDisplayParts = { processItems: [], outsideItems: [] };
  let currentHasConversation = false;
  const flushSegment = () => {
    if (current.processItems.length === 0 && current.outsideItems.length === 0) return;
    segments.push(current);
    current = { processItems: [], outsideItems: [] };
    currentHasConversation = false;
  };
  const pushProcess = (item: Item) => {
    if (currentHasConversation) flushSegment();
    current.processItems.push(item);
  };
  for (const item of items) {
    if (item.kind === "user") continue;
    if (item.kind === "notice") {
      if (isHostRecoveryGuidance(item.text)) {
        continue;
      }
      if (isSteerNoticeText(item.text)) {
        current.outsideItems.push(item);
        currentHasConversation = true;
      } else if (item.level === "warn" || item.variant === "delivery") {
        current.outsideItems.push(item);
      } else {
        pushProcess(item);
      }
      continue;
    }
    if (item.kind === "extension") {
      // Extension cards carry their own actions and progress — keep them
      // visible like warnings instead of folding them into the process
      // collapse, but never treat them as a conversational boundary.
      current.outsideItems.push(item);
      continue;
    }
    if (item.kind !== "assistant") {
      pushProcess(item);
      continue;
    }
    const hasReasoning = Boolean(item.reasoning || (live.id === item.id && live.hasReasoning));
    if (assistantHasVisibleAnswer(item, live)) {
      if (hasReasoning) pushProcess(assistantReasoningOnly(item));
      current.outsideItems.push(item);
      currentHasConversation = true;
      continue;
    }
    if (hasReasoning) pushProcess(item);
  }
  flushSegment();
  return segments;
}

function assistantReasoningOnly(item: AssistantItem): AssistantItem {
  const reasoning = { ...item, text: "" };
  itemMeasurementVersionOverrides.set(reasoning, itemMeasurementVersion(item));
  return reasoning;
}

export function turnWorkDurationMs(items: readonly Item[]): number {
  const persisted = items.reduce((ms, it) => {
    if (it.kind !== "assistant") return ms;
    return Math.max(ms, it.workDurationMs ?? 0);
  }, 0);
  if (persisted > 0) return persisted;
  return items.reduce((ms, it) => {
    if (it.kind === "tool") return ms + (it.durationMs ?? 0);
    if (it.kind === "assistant") return ms + (it.reasoningDurationMs ?? 0);
    return ms;
  }, 0);
}

// ── Turn / segment models ─────────────────────────────────────────────────────

export interface SegmentModel {
  /** Stable fold key: the segment's first raw process item id. */
  key: string;
  processItems: Item[];
  outsideItems: TurnDisplayParts["outsideItems"];
  /** Items the fold body would render (parentId/todo/plan tools filtered out). */
  displayItems: Item[];
  /** Turn-level: the turn renders anything outside its folds. */
  hasOutsideContent: boolean;
  /** Disclosure ownership: every untouched segment stays open until its turn settles. */
  foldActive: boolean;
  hasRunningWork: boolean;
  durationMs: number;
  /** "full" carries the work-duration label; earlier segments only list counts. */
  labelStyle: "full" | "counts";
  turnActive: boolean;
}

export interface TurnModel {
  user: UserItem | undefined;
  /** Question-navigator turn number (undefined for the prelude). */
  turn: number | undefined;
  /** Running and the last turn — its final segment stays open and ticks. */
  isActive: boolean;
  turnItems: Item[];
  segments: SegmentModel[];
  /** Combined assistant answer text for the turn action row ("" → no row). */
  actionText: string;
}

function turnStableIdentity(model: TurnModel): string {
  if (model.user) {
    const user = model.user;
    return JSON.stringify([user.id, user.submissionId ?? "", user.createdAt ?? null, user.text, user.submitText ?? "", user.historyTurn ?? null, user.checkpointTurn ?? null]);
  }
  const first = model.turnItems[0];
  return JSON.stringify(["prelude", first?.kind ?? "", first?.id ?? ""]);
}

// Keep only items the fold body will actually render — an expandable fold over
// nothing is worse than no fold. Assistant items reach the fold stripped to
// their reasoning (answer text renders outside), so reasoning presence is the
// only thing that keeps them.
function foldDisplayItems(items: readonly Item[], live: TranscriptLiveFlags, hideReasoning: boolean): Item[] {
  return items.filter((it) => {
    if (it.kind === "assistant") {
      if (hideReasoning) return false;
      return Boolean(it.reasoning || (live.id === it.id && live.hasReasoning));
    }
    if (it.kind === "phase") return true;
    if (it.kind === "notice") return true;
    if (it.kind === "compaction") return true;
    if (it.kind !== "tool") return false;
    if (it.parentId || it.name === "todo_write" || it.name === "exit_plan_mode") return false;
    return true;
  });
}

function segmentHasRunningWork(displayItems: readonly Item[], turnActive: boolean, live: TranscriptLiveFlags): boolean {
  const hasRunningProcess = displayItems.some((it) => {
    if (it.kind === "tool") return it.status === "running";
    if (it.kind !== "assistant") return false;
    if (live.id === it.id) return !live.reasoningComplete;
    return it.streaming && !it.reasoningComplete;
  });
  const hasLiveAssistant = displayItems.some((it) => it.kind === "assistant" && live.id === it.id);
  return turnActive || hasRunningProcess || hasLiveAssistant;
}

export function buildTurnModels(
  items: readonly Item[],
  live: TranscriptLiveFlags = NO_LIVE,
  running = false,
  hideReasoning = false,
): TurnModel[] {
  const turns: TurnModel[] = [];
  let currentUser: UserItem | undefined;
  let currentItems: Item[] = [];
  let turn = 0;
  const flush = () => {
    if (!currentUser && currentItems.length === 0) return;
    turns.push({
      user: currentUser,
      turn: currentUser ? turn++ : undefined,
      isActive: false,
      turnItems: currentItems,
      segments: [],
      actionText: "",
    });
    currentUser = undefined;
    currentItems = [];
  };
  for (const item of items) {
    if (item.kind === "user") {
      flush();
      currentUser = item;
    } else {
      currentItems.push(item);
    }
  }
  flush();

  for (let index = 0; index < turns.length; index += 1) {
    const model = turns[index];
    model.isActive = running && index === turns.length - 1;
    const segments = partitionTurnItems(model.turnItems, live);
    const turnHasOutsideContent = segments.some((segment) => segment.outsideItems.length > 0);
    const turnIdentity = turnStableIdentity(model);
    model.segments = segments.map((segment, segmentIndex) => {
      const isLastSegment = segmentIndex === segments.length - 1;
      const displayItems = foldDisplayItems(segment.processItems, live, hideReasoning);
      const turnActive = model.isActive && isLastSegment;
      const hasRunningWork = segmentHasRunningWork(displayItems, turnActive, live);
      return {
        // Duplicate raw ids occur in imported/merged histories. Derive the
        // disambiguator from stable turn/item identity rather than occurrence
        // order, so prepending older history never renames an already mounted row.
        key: `${segment.processItems[0]?.id || "seg"}@${stableStringHash(
          JSON.stringify([
            turnIdentity,
            segmentIndex,
            segment.processItems[0]?.kind ?? "",
            segment.processItems[0]?.id ?? "",
          ]),
        )}`,
        processItems: segment.processItems,
        outsideItems: segment.outsideItems,
        displayItems,
        hasOutsideContent: turnHasOutsideContent,
        foldActive: model.isActive || hasRunningWork,
        hasRunningWork,
        durationMs: isLastSegment ? turnWorkDurationMs(model.turnItems) : 0,
        labelStyle: isLastSegment ? "full" : "counts",
        turnActive,
      } satisfies SegmentModel;
    });
    let actionText = "";
    for (const item of model.turnItems) {
      if (item.kind !== "assistant" || item.streaming || !item.text.trim()) continue;
      actionText = appendTurnActionCopyText(actionText, item.text);
    }
    model.actionText = actionText;
  }
  return turns;
}

// ── Fold state ────────────────────────────────────────────────────────────────

export interface FoldEntry {
  open: boolean;
  userOverridden: boolean;
  running: boolean;
  keepReasoningExpanded?: boolean;
}

export type FoldMap = ReadonlyMap<string, FoldEntry>;

export const EMPTY_FOLDS: FoldMap = new Map();

export function defaultFoldOpen(
  segment: { hasOutsideContent: boolean; hasRunningWork: boolean; foldActive?: boolean; keepReasoningExpanded?: boolean },
  preference: ProcessFoldPreference,
): boolean {
  return preference === "expanded" || segment.keepReasoningExpanded === true || !segment.hasOutsideContent || segment.foldActive === true || segment.hasRunningWork;
}

export interface FoldSegmentState {
  key: string;
  hasOutsideContent: boolean;
  hasRunningWork: boolean;
  keepReasoningExpanded: boolean;
}

export function foldSegmentStates(models: readonly TurnModel[], keepReasoningExpanded = false): FoldSegmentState[] {
  const out: FoldSegmentState[] = [];
  for (const model of models) {
    for (const segment of model.segments) {
      if (segment.displayItems.length === 0) continue;
      out.push({
        key: segment.key,
        hasOutsideContent: segment.hasOutsideContent,
        hasRunningWork: segment.foldActive,
        keepReasoningExpanded: keepReasoningExpanded && segment.displayItems.some((item) => item.kind === "assistant"),
      });
    }
  }
  return out;
}

/**
 * Advance the fold map to match the current segments. Mirrors the old
 * per-TurnCollapse effects: a fold auto-opens while its turn runs and
 * auto-closes on completion unless the user toggled it, it has nothing
 * outside, or the preference pins every fold open. A preference switch clears
 * per-fold overrides so the whole transcript lands in one consistent state.
 * Returns null when nothing changed (so callers can skip a re-render).
 */
export function reconcileFoldEntries(
  prev: FoldMap,
  segments: readonly FoldSegmentState[],
  preference: ProcessFoldPreference,
  preferenceChanged: boolean,
): Map<string, FoldEntry> | null {
  let next: Map<string, FoldEntry> | null = null;
  const write = (key: string, entry: FoldEntry) => {
    if (!next) next = new Map(prev);
    next.set(key, entry);
  };
  const seen = new Set<string>();
  for (const segment of segments) {
    seen.add(segment.key);
    const entry = prev.get(segment.key);
    if (!entry) {
      write(segment.key, {
        open: defaultFoldOpen(segment, preference),
        userOverridden: false,
        running: segment.hasRunningWork,
        keepReasoningExpanded: segment.keepReasoningExpanded,
      });
      continue;
    }
    const reasoningPinChanged = Boolean(entry.keepReasoningExpanded) !== segment.keepReasoningExpanded;
    if (preferenceChanged || reasoningPinChanged) {
      const open = preference === "expanded" || segment.keepReasoningExpanded
        ? true
        : !segment.hasRunningWork && segment.hasOutsideContent
          ? false
          : entry.open;
      if (open !== entry.open || entry.userOverridden || entry.running !== segment.hasRunningWork || reasoningPinChanged) {
        write(segment.key, {
          open,
          userOverridden: false,
          running: segment.hasRunningWork,
          keepReasoningExpanded: segment.keepReasoningExpanded,
        });
      }
      continue;
    }
    if (segment.hasRunningWork) {
      // A fresh run clears the previous manual toggle; while running the fold
      // stays open unless the user closed it during THIS run.
      const userOverridden = entry.running ? entry.userOverridden : false;
      const open = userOverridden ? entry.open : true;
      if (open !== entry.open || userOverridden !== entry.userOverridden || !entry.running) {
        write(segment.key, { open, userOverridden, running: true, keepReasoningExpanded: segment.keepReasoningExpanded });
      }
      continue;
    }
    if (entry.running) {
      const open = !entry.userOverridden && segment.hasOutsideContent && preference !== "expanded" && !segment.keepReasoningExpanded ? false : entry.open;
      write(segment.key, {
        open,
        userOverridden: entry.userOverridden,
        running: false,
        keepReasoningExpanded: segment.keepReasoningExpanded,
      });
    }
  }
  for (const key of prev.keys()) {
    if (!seen.has(key)) {
      if (!next) next = new Map(prev);
      next.delete(key);
    }
  }
  return next;
}

/** User clicked a fold header: flip it and mark the choice as deliberate. */
export function foldMapWithToggle(prev: FoldMap, key: string, currentlyOpen: boolean): Map<string, FoldEntry> {
  const next = new Map(prev);
  const entry = prev.get(key);
  next.set(key, {
    open: !currentlyOpen,
    userOverridden: true,
    running: entry?.running ?? false,
    keepReasoningExpanded: entry?.keepReasoningExpanded,
  });
  return next;
}

/** Preserve an inner reasoning expansion when the enclosing process fold settles. */
export function foldMapWithReasoningOpen(prev: FoldMap, key: string, running: boolean): Map<string, FoldEntry> {
  const next = new Map(prev);
  const entry = prev.get(key);
  next.set(key, {
    open: true,
    userOverridden: true,
    running: entry?.running ?? running,
    keepReasoningExpanded: entry?.keepReasoningExpanded,
  });
  return next;
}

// ── Virtual rows ──────────────────────────────────────────────────────────────

type TranscriptRowContent =
  | { kind: "older-history"; key: string }
  | { kind: "user"; key: string; item: UserItem; turn: number | undefined }
  | { kind: "process-header"; key: string; segment: SegmentModel; open: boolean }
  | { kind: "reasoning"; key: string; item: AssistantItem; segmentKey: string; autoFollowActive?: boolean }
  | { kind: "tool"; key: string; item: ToolItem }
  | { kind: "tool-batch"; key: string; items: ToolItem[] }
  | { kind: "tool-group"; key: string; items: ToolItem[]; groupKind: ToolGroupKind }
  | { kind: "phase"; key: string; item: PhaseItem }
  | { kind: "process-notice"; key: string; item: NoticeItem }
  | { kind: "compaction"; key: string; item: CompactionItem }
  | { kind: "answer"; key: string; item: AssistantItem }
  | { kind: "notice"; key: string; item: NoticeItem }
  | { kind: "extension"; key: string; item: ExtensionItem }
  | { kind: "turn-actions"; key: string; turn: number; text: string };

export type TranscriptRow = TranscriptRowContent & { layoutVariant?: TranscriptRowLayoutVariant };

/** Initial geometry is present on every row built by buildTranscriptRows.
 * Optional keeps compatibility with focused tests/extensions that construct
 * a minimal TranscriptRow directly; consumers resolve their safe fallback. */
export type TranscriptRowWithLayout = TranscriptRowContent & { layoutVariant: TranscriptRowLayoutVariant };

// Derived reasoning-only assistant items intentionally blank the answer text;
// retain the source semantic version for that projection without caching
// mutable bridge objects themselves.
const itemMeasurementVersionOverrides = new WeakMap<object, string>();

function hashGeometryParts(parts: readonly string[]): string {
  let hash = 2166136261;
  for (const part of parts) {
    for (let index = 0; index < part.length; index += 1) {
      hash ^= part.charCodeAt(index);
      hash = Math.imul(hash, 16777619);
    }
    hash ^= 31;
    hash = Math.imul(hash, 16777619);
  }
  return `${hash >>> 0}:${parts.map((part) => part.length).join(",")}`;
}

/** Semantic content version: equal projections keep the same geometry even
 * when the controller creates fresh objects for an unrelated UI update. */
function itemMeasurementVersion(item: Item): string {
  const override = itemMeasurementVersionOverrides.get(item);
  if (override) return override;
  const parts: string[] = [String(item.kind ?? ""), String(item.id ?? "")];
  // Focused callers and older bridge payloads may provide a minimal item
  // without the discriminant. Preserve its text in the semantic version so a
  // late preview -> resolved patch cannot reuse the preview measurement.
  if (!item.kind) {
    const legacy = item as unknown as { text?: unknown; reasoning?: unknown; output?: unknown; args?: unknown };
    parts.push(String(legacy.text ?? ""), String(legacy.reasoning ?? ""), String(legacy.args ?? ""), String(legacy.output ?? ""));
  }
  switch (item.kind) {
    case "user":
      parts.push(String(item.text ?? ""), String(item.submitText ?? ""), item.failed ? "1" : "0", String(item.createdAt ?? ""));
      break;
    case "assistant":
      parts.push(String(item.text ?? ""), String(item.reasoning ?? ""), item.streaming ? "1" : "0", item.reasoningComplete ? "1" : "0", String(item.reasoningDurationMs ?? ""), String(item.workDurationMs ?? ""), JSON.stringify(item.searchSources ?? []), JSON.stringify(item.memoryCitations ?? []));
      break;
    case "phase":
      parts.push(String(item.text ?? ""));
      break;
    case "notice":
      parts.push(String(item.text ?? ""), String(item.detail ?? ""), String(item.title ?? ""), String(item.level ?? ""), String(item.variant ?? ""), String(item.action ?? ""), JSON.stringify(item.completionSummary ?? {}));
      break;
    case "compaction":
      parts.push(item.pending ? "1" : "0", String(item.trigger ?? ""), String(item.messages ?? ""), String(item.summary ?? ""), String(item.archive ?? ""));
      break;
    case "tool":
      parts.push(String(item.name ?? ""), String(item.args ?? ""), String(item.output ?? ""), String(item.error ?? ""), String(item.status ?? ""), String(item.subject ?? ""), String(item.summary ?? ""), item.truncated ? "1" : "0", item.dataArchived ? "1" : "0", JSON.stringify(item.fileDiff ?? {}), JSON.stringify(item.execution ?? {}), item.subagentProgress ? JSON.stringify(item.subagentProgress) : "");
      break;
    case "extension":
      parts.push(String(item.surfaceKey ?? ""), String(item.pluginId ?? ""), String(item.surfaceId ?? ""), String(item.generation ?? ""), JSON.stringify(item.card ?? null));
      break;
  }
  return hashGeometryParts(parts);
}

function measurementVersionForItems(items: readonly Item[]): string {
  return hashGeometryParts(items.map((item) => itemMeasurementVersion(item)));
}

export function transcriptRowMeasurementVersion(row: TranscriptRow): string {
  switch (row.kind) {
    case "older-history":
    case "turn-actions":
      return "0:0";
    case "process-header":
      return measurementVersionForItems(row.segment.processItems);
    case "tool-batch":
    case "tool-group":
      return measurementVersionForItems(row.items);
    default:
      return `1:${itemMeasurementVersion(row.item)}`;
  }
}

export const OLDER_HISTORY_ROW_KEY = "older-history";

export function userRowKey(itemId: string): string {
  return `u:${itemId}`;
}

/** Body rows of one expanded process fold: read-only batches, creation tool
 *  groups, single tool cards, phases, info notices, compactions, reasoning. */
function processBodyRows(
  segment: SegmentModel,
  creationMode: boolean,
  reasoningDisplayMode: ResolvedReasoningDisplayMode,
  subcallsByParent: ReadonlyMap<string, readonly ToolItem[]>,
): TranscriptRowWithLayout[] {
  const rows: TranscriptRowWithLayout[] = [];
  let roBatch: ToolItem[] = [];
  let toolBatch: ToolItem[] = [];
  let toolBatchKind: ToolGroupKind | null = null;
  const flushRO = () => {
    if (roBatch.length === 0) return;
    rows.push({ kind: "tool-batch", key: `tb:${roBatch[0].id}`, items: [...roBatch], layoutVariant: "tool-batch-collapsed" });
    roBatch = [];
  };
  const flushToolBatch = () => {
    if (!toolBatchKind || toolBatch.length === 0) return;
    rows.push({ kind: "tool-group", key: `tg:${toolBatch[0].id}`, items: [...toolBatch], groupKind: toolBatchKind, layoutVariant: "tool-group-collapsed" });
    toolBatch = [];
    toolBatchKind = null;
  };
  for (const it of segment.displayItems) {
    if (creationMode && it.kind === "tool" && isCreationGroupableTool(it as ToolItem)) {
      const kind = toolGroupKind(it as ToolItem);
      if (kind) {
        if (toolBatchKind && toolBatchKind !== kind) flushToolBatch();
        toolBatchKind = kind;
        toolBatch.push(it as ToolItem);
        continue;
      }
    }
    if (it.kind !== "tool") {
      flushToolBatch();
      flushRO();
    }
    if (!creationMode && it.kind === "tool" && it.status !== "running" && isBatchedReadOnlyTool(it.name, it.readOnly)) {
      roBatch.push(it as ToolItem);
      continue;
    }
    if (it.kind === "tool") {
      flushToolBatch();
      flushRO();
    }
    switch (it.kind) {
      case "tool":
        rows.push({
          kind: "tool",
          key: `t:${it.id}`,
          item: it as ToolItem,
          layoutVariant: resolveToolCardDefaultOpen(
            it as ToolItem,
            subcallsByParent.get(it.id)?.length ?? 0,
            reasoningDisplayMode,
          ) ? "tool-expanded" : "tool-collapsed",
        });
        break;
      case "phase":
        rows.push({ kind: "phase", key: `p:${it.id}`, item: it as PhaseItem, layoutVariant: "static" });
        break;
      case "notice":
        rows.push({ kind: "process-notice", key: `pn:${it.id}`, item: it as NoticeItem, layoutVariant: "static" });
        break;
      case "compaction":
        rows.push({
          kind: "compaction",
          key: `c:${it.id}`,
          item: it as CompactionItem,
          layoutVariant: (it as CompactionItem).pending ? "static" : "compaction-collapsed",
        });
        break;
      case "assistant":
        // Answer text renders outside the fold (partitionTurnItems strips it),
        // so the fold only ever shows the reasoning segment.
        rows.push({
          kind: "reasoning",
          key: `r:${it.id}`,
          item: it as AssistantItem,
          segmentKey: segment.key,
          autoFollowActive: segment.foldActive,
          layoutVariant: resolveReasoningLayoutVariant(
            reasoningDisplayMode,
            segment.foldActive,
          ) ?? "reasoning-summary",
        });
        break;
    }
  }
  flushToolBatch();
  flushRO();
  return rows;
}

export interface BuildRowsOptions {
  folds: FoldMap;
  foldPreference: ProcessFoldPreference;
  hasOlderHistory: boolean;
  creationMode: boolean;
  /** Checkpoint-aware turn number for a user item (questionTurnsById). */
  turnForUser: (item: UserItem) => number | undefined;
  /** A checkpoint may make a turn actionable even without assistant text. */
  hasCheckpointForTurn?: (turn: number) => boolean;
  reasoningDisplayMode?: ResolvedReasoningDisplayMode;
  subcallsByParent?: ReadonlyMap<string, readonly ToolItem[]>;
}

export function buildTranscriptRows(models: readonly TurnModel[], options: BuildRowsOptions): TranscriptRowWithLayout[] {
  const rows: TranscriptRowWithLayout[] = [];
  const rowGroups: TranscriptRowWithLayout[][] = [];
  const usedKeys = new Set<string>();
  const reasoningDisplayMode = options.reasoningDisplayMode ?? "auto";
  const subcallsByParent = options.subcallsByParent ?? new Map<string, readonly ToolItem[]>();
  for (let modelIndex = models.length - 1; modelIndex >= 0; modelIndex -= 1) {
    const model = models[modelIndex];
    const modelRows: TranscriptRowWithLayout[] = [];
    const user = model.user;
    // Turn numbers come from the checkpoint-aware map, not the raw question
    // index, so rewind targets survive history paging.
    const turn = user ? options.turnForUser(user) : undefined;
    if (user) {
      modelRows.push({ kind: "user", key: userRowKey(user.id), item: user, turn, layoutVariant: "text-flow" });
    }
    for (const segment of model.segments) {
      if (segment.displayItems.length > 0) {
        const open = options.folds.get(segment.key)?.open ?? defaultFoldOpen(segment, options.foldPreference);
        modelRows.push({ kind: "process-header", key: `ph:${segment.key}`, segment, open, layoutVariant: "static" });
        if (open) modelRows.push(...processBodyRows(segment, options.creationMode, reasoningDisplayMode, subcallsByParent));
      }
      for (const item of segment.outsideItems) {
        if (item.kind === "extension") {
          modelRows.push({ kind: "extension", key: `x:${item.id}`, item, layoutVariant: "text-flow" });
        } else if (item.kind === "notice") {
          modelRows.push({ kind: "notice", key: `n:${item.id}`, item, layoutVariant: "text-flow" });
        } else {
          modelRows.push({ kind: "answer", key: `a:${item.id}`, item, layoutVariant: "text-flow" });
        }
      }
    }
    // The active turn's actions appear only once it settles — mid-turn there is
    // nothing final to copy or rewind to. The row key follows the user item id
    // (turn NUMBERS shift when older history pages prepend; ids don't).
    if (
      !model.isActive &&
      turn != null &&
      (model.actionText.trim() || options.hasCheckpointForTurn?.(turn)) &&
      user
    ) {
      modelRows.push({ kind: "turn-actions", key: `ta:${user.id}`, turn, text: model.actionText, layoutVariant: "static" });
    }
    for (let index = modelRows.length - 1; index >= 0; index -= 1) {
      const row = modelRows[index];
      if (usedKeys.has(row.key)) modelRows[index] = { ...row, key: `${row.key}@${stableStringHash(`${turnStableIdentity(model)}|${row.kind}|${row.key}`)}` };
      usedKeys.add(modelRows[index].key);
    }
    rowGroups.push(modelRows);
  }
  for (let index = rowGroups.length - 1; index >= 0; index -= 1) rows.push(...rowGroups[index]);
  if (options.hasOlderHistory) rows.unshift({ kind: "older-history", key: OLDER_HISTORY_ROW_KEY, layoutVariant: "static" });
  return rows;
}

// ── Measurement / identity helpers ────────────────────────────────────────────

/** Ballpark row heights; Virtuoso replaces them with real measurements on mount. */
export function estimateTranscriptRowSize(row: TranscriptRow | undefined, contentWidth?: number): number {
  const environment: TranscriptGeometryEnvironment = { contentWidth, typographySignature: "default" };
  return estimateTranscriptRowGeometry(row, environment);
}

/**
 * History-backed items carry ids derived from their backend entry
 * (`he:<entryId>`, tool calls `he:<entryId>:tc<index>`, or a bare toolCallId).
 * Returns the entryId for rows that may carry unresolved lazy-content refs.
 */
export function historyEntryIdForItemId(id: string | undefined): string | undefined {
  if (!id || !id.startsWith("he:")) return undefined;
  return id.slice(3).replace(/:tc\d+$/, "");
}

/** The entry a row can trigger lazy full-content resolution for, if any. */
export function historyEntryIdForRow(row: TranscriptRow): string | undefined {
  switch (row.kind) {
    case "user":
    case "reasoning":
    case "tool":
    case "phase":
    case "process-notice":
    case "compaction":
    case "answer":
    case "notice":
      return historyEntryIdForItemId(row.item.id);
    case "tool-batch":
    case "tool-group":
      return historyEntryIdForItemId(row.items[0]?.id);
    default:
      return undefined;
  }
}
