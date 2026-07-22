import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, ClipboardEvent, DragEvent, KeyboardEvent, MouseEvent as ReactMouseEvent, PointerEvent as ReactPointerEvent } from "react";
import { ArrowRight, ArrowUp, AtSign, Check, ChevronDown, ChevronUp, ChevronsUpDown, CornerDownRight, Equal, Eye, FilePlus2, FileText, Flag, Folder, Gauge, Hash, List, MessageSquare, Plus, Search, Shield, ShieldAlert, ShieldCheck, Square, Target, Trash2, X } from "lucide-react";
import { asArray } from "../lib/array";
import { filterAtMatches } from "../lib/atMatches";
import { DedupIndex, sha256 } from "../lib/attachDedup";
import { app, onFilesDropped } from "../lib/bridge";
import { canUsePromptHistory, composerEnterAction, insertComposerNewline, isFnKeyEvent, promptHistoryDirectionFromEvent } from "../lib/composerKeyboard";
import { cacheGeneration, loadOlder } from "../lib/composerHistory";
import { SPINNER_WORDS, useI18n } from "../lib/i18n";
import { detectShortcutPlatform, formatShortcutCombo, matchesShortcut, useShortcutComboLabel } from "../lib/keyboardShortcuts";
import { fallbackCopyText } from "../lib/clipboard";
import {
  commandUsesStructuredInvocation,
  invocationRequests,
  replaceInvocationTextRange,
  serializeInvocationSubmit,
  trimInvocationDraft,
  type ComposerInvocation,
  type StructuredInvocationSubmit,
} from "../lib/invocationDisplay";
import { clearLayoutSize, loadOptionalLayoutSize, saveLayoutSize } from "../lib/layoutPreferences";
import { createRafResizeUpdater } from "../lib/resizeDrag";
import { useToast } from "../lib/toast";
import { type CollaborationMode, type CommandInfo, type ComposerInsertRequest, type ContextInfo, type DirEntry, type EffortInfo, type HistoryMessage, type Mode, type PromptHistoryEntry, type SessionMeta, type SessionReference, type SlashArgItem, type SlashArgsResult, type TokenMode, type ToolApprovalMode, type BalanceInfo } from "../lib/types";
import {
  formatWorkspaceReference,
  parseWorkspaceReference,
  readWorkspaceReferenceDrag,
  WORKSPACE_REF_DRAG_TYPE,
} from "../lib/workspaceDrag";
import { SlashMenu, sortSlashCommandsForMenu } from "./SlashMenu";
import { ArgMenu } from "./ArgMenu";
import { ANCHORED_POPOVER_CLOSE_MS, AnchoredPopover } from "./AnchoredPopover";
import { EffortSwitcher } from "./EffortSwitcher";
import { ModelSwitcher } from "./ModelSwitcher";
import { Tooltip } from "./Tooltip";
import { ComposerContextCard } from "./ComposerContextCard";
import { Markdown } from "./Markdown";
import { CodeViewer } from "./CodeViewer";
import { ContextWindowRing } from "./ContextWindowRing";
import { ImageViewer } from "./ImageViewer";
import {
  RichComposerInput,
  type RichComposerInputHandle,
  type RichComposerSelection,
  type RichSlashQuery,
} from "./RichComposerInput";
import { VirtualMenu } from "./VirtualMenu";
import { activeFileReferenceToken, dirEntryMenuLabel, dirEntrySubmitPath } from "./FileReferenceMenu";
import { activeRefTokenRe, escapeRefPath, unescapeRefPath } from "../lib/refToken";
import { ContextMenu, contextMenuPointFromEvent, type ContextMenuItem, type ContextMenuPoint } from "./ContextMenu";
import {
  formatSelectedTextContext,
  formatSelectionLabel,
  languageFor,
  normalizeSelectedText,
  selectedTextSnippet,
  type SelectedTextInsertRequest,
  type SelectedTextReference,
} from "../lib/selectedTextContext";
interface Attachment {
  path: string;
  previewUrl?: string;
  displayName?: string;
}

interface AttachmentDedupKey {
  hash: string;
  source: string;
}

export interface WorkspaceReference {
  path: string;
  isDir?: boolean;
  displayPath?: string;
}

const LONG_PASTE_MIN_CHARS = 2000;
const LONG_PASTE_MIN_LINES = 20;
const COMPOSER_MIN_HEIGHT = 104;
const COMPOSER_MAX_HEIGHT = 360;
// Height reserved for the in-card run strip while a turn runs; applied via a
// CSS calc so --composer-height always stays in "logical height" space.
const COMPOSER_RUN_STRIP_RESERVED = 30;
const COMPOSER_MAX_VIEWPORT_RATIO = 0.4;
const COMPOSER_AUTO_RESERVED_HEIGHT = 58;
const PROMPT_HISTORY_PREFETCH_REMAINING = 3;
// Grace after compositionend to swallow a confirm-Enter that lands just after
// it; the real gap is a few ms, so keep it short or a deliberate quick second
// Enter (submit) gets eaten too.
const IME_CONFIRM_GRACE_MS = 100;
const FILE_REF_SEARCH_CACHE_TTL_MS = 5000;

type PastedBlock = {
  label: string;
  text: string;
};

type PendingGuidance = {
  id: number;
  text: string;
  submitText: string;
  structured?: StructuredInvocationSubmit;
};

type FileRefSearchCacheEntry = {
  entries: DirEntry[];
  cachedAt: number;
};

type ComposerDraft = {
  text: string;
  invocations: ComposerInvocation[];
  attachments: Attachment[];
  workspaceRefs: WorkspaceReference[];
  pastedBlocks: PastedBlock[];
  openPastedLabels: string[];
  sessionRefs: SessionReference[];
  selectedTextRefs: SelectedTextReference[];
  attachmentDedupKeys: Record<string, AttachmentDedupKey>;
  nextPasteId: number;
  historyIndex: number;
  savedText: string;
  pendingGuidance: PendingGuidance[];
  guidanceExpanded: boolean;
  guidanceSendingId: number | null;
  pendingPaste: number;
  submitting: boolean;
};

type WebkitFileEntry = {
  isDirectory?: boolean;
};

const DEFAULT_COMPOSER_DRAFT_KEY = "__default_composer_draft__";

function lineCount(s: string): number {
  if (s === "") return 0;
  return s.split(/\r\n|\r|\n/).length;
}

function shouldFoldPaste(s: string): boolean {
  return s.length >= LONG_PASTE_MIN_CHARS || lineCount(s) >= LONG_PASTE_MIN_LINES;
}

function renderPastedBlock(block: PastedBlock): string {
  return `${block.label}\n\n--- Begin ${block.label} ---\n${block.text}\n--- End ${block.label} ---`;
}

function baseName(path: string): string {
  const clean = path.replace(/[\\/]+$/, "");
  return clean.split(/[\\/]/).filter(Boolean).pop() ?? path;
}

function attachmentName(attachment: Attachment): string {
  return (attachment.displayName || baseName(attachment.path) || "attachment").trim();
}

function attachmentExt(name: string): string {
  const dot = name.lastIndexOf(".");
  return dot >= 0 ? name.slice(dot + 1).toUpperCase() : "";
}

function hasImageAttachments(items: Attachment[]): boolean {
  return items.some((attachment) => Boolean(attachment.previewUrl));
}

function displayRefName(name: string): string {
  return name.replace(/[\[\]\(\)\r\n]+/g, " ").replace(/\s+/g, " ").trim() || "attachment";
}

function formatAttachmentDisplayReference(attachment: Attachment): string {
  return `@[${displayRefName(attachmentName(attachment))}](${attachment.path})`;
}

function sortComposerAttachments(items: Attachment[]): Attachment[] {
  return [...items].sort((a, b) => {
    const ai = a.previewUrl ? 0 : 1;
    const bi = b.previewUrl ? 0 : 1;
    return ai - bi;
  });
}

function workspaceReferenceKey(ref: WorkspaceReference): string {
  return `${ref.isDir ? "dir" : "file"}:${ref.path}`;
}

type PastChatToken = {
  from: number;
  query: string;
};

function activePastChatToken(text: string): PastChatToken | null {
  const queryText = text.replace(/[\r\n]+$/u, "");
  const match = /(?:^|\s)#([^\s#]*)$/u.exec(queryText);
  if (!match) return null;
  return { from: match.index, query: match[1] };
}

export function composerPickFileEntry(
  text: string,
  atRaw: string | null,
  atDir: string,
  entry: DirEntry,
): { text: string; workspaceRef?: WorkspaceReference } {
  const queryText = text.replace(/[\r\n]+$/u, "");
  const atPos = queryText.length - (atRaw?.length ?? 0) - 1; // index of '@'
  const prefix = queryText.slice(0, Math.max(0, atPos));
  const refPath = dirEntrySubmitPath(entry, atDir);
  if (entry.path || entry.displayPath) {
    return { text: prefix, workspaceRef: { path: refPath, isDir: entry.isDir, displayPath: entry.displayPath } };
  }
  // Inline fallback: escape whitespace so the ref survives @-token parsing.
  return { text: prefix + "@" + escapeRefPath(refPath) + (entry.isDir ? "/" : " ") };
}

function emptyComposerDraft(): ComposerDraft {
  return {
    text: "",
    invocations: [],
    attachments: [],
    workspaceRefs: [],
    pastedBlocks: [],
    openPastedLabels: [],
    sessionRefs: [],
    selectedTextRefs: [],
    attachmentDedupKeys: {},
    nextPasteId: 1,
    historyIndex: -1,
    savedText: "",
    pendingGuidance: [],
    guidanceExpanded: false,
    guidanceSendingId: null,
    pendingPaste: 0,
    submitting: false,
  };
}

// Exact (trimmed) equality only: the consumed-steer notice carries the steer
// text verbatim, and substring matching removed the wrong queue item when one
// queued text contained another (#6238).
function guidanceTextMatches(queued: string, consumed: string): boolean {
  const left = queued.trim();
  const right = consumed.trim();
  if (!left || !right) return false;
  return left === right;
}

function cloneComposerDraft(draft: ComposerDraft): ComposerDraft {
  return {
    text: draft.text,
    invocations: draft.invocations.map((invocation) => ({ ...invocation, command: { ...invocation.command } })),
    attachments: [...draft.attachments],
    workspaceRefs: [...draft.workspaceRefs],
    pastedBlocks: [...draft.pastedBlocks],
    openPastedLabels: [...draft.openPastedLabels],
    sessionRefs: [...draft.sessionRefs],
    selectedTextRefs: draft.selectedTextRefs.map((reference) => ({ ...reference })),
    attachmentDedupKeys: { ...draft.attachmentDedupKeys },
    nextPasteId: draft.nextPasteId,
    historyIndex: draft.historyIndex,
    savedText: draft.savedText,
    pendingGuidance: draft.pendingGuidance.map((item) => ({ ...item })),
    guidanceExpanded: draft.guidanceExpanded,
    guidanceSendingId: draft.guidanceSendingId,
    pendingPaste: draft.pendingPaste,
    submitting: draft.submitting,
  };
}

function attachmentDedupFromKeys(keys: Record<string, AttachmentDedupKey>): DedupIndex {
  const index = new DedupIndex();
  for (const key of Object.values(keys)) {
    index.add(key.hash, key.source);
  }
  return index;
}

function draftHasAttachmentDedupKey(draft: ComposerDraft, key: AttachmentDedupKey): boolean {
  return Object.values(draft.attachmentDedupKeys).some((existing) => existing.hash === key.hash && existing.source === key.source);
}

function fileKey(file: File): string {
  return `${file.name}:${file.type}:${file.size}:${file.lastModified}`;
}

function clipboardFiles(data: DataTransfer): File[] {
  const files = Array.from(data.files);
  const seen = new Set(files.map(fileKey));
  for (const item of Array.from(data.items)) {
    if (item.kind !== "file") continue;
    const file = item.getAsFile();
    if (!file) continue;
    const key = fileKey(file);
    if (seen.has(key)) continue;
    seen.add(key);
    files.push(file);
  }
  return files;
}

function clipboardHasImageHint(data: DataTransfer): boolean {
  const imageType = (value: string) => {
    const type = value.toLowerCase();
    return type.startsWith("image/") || type.includes("png") || type.includes("jpeg") || type.includes("jpg") || type.includes("tiff");
  };
  return Array.from(data.items).some((item) => imageType(item.type)) || Array.from(data.types).some(imageType);
}

function isPasteShortcut(e: KeyboardEvent<HTMLElement>): boolean {
  return e.key.toLowerCase() === "v" && (e.metaKey || e.ctrlKey) && !e.altKey;
}

async function dataURLHash(dataUrl: string): Promise<string> {
  try {
    const res = await fetch(dataUrl);
    return sha256(await res.blob());
  } catch {
    return "";
  }
}

function composerMaxHeight(): number {
  if (typeof window === "undefined") return COMPOSER_MAX_HEIGHT;
  return Math.max(COMPOSER_MIN_HEIGHT, Math.min(COMPOSER_MAX_HEIGHT, Math.floor(window.innerHeight * COMPOSER_MAX_VIEWPORT_RATIO)));
}

// The rendered card includes the run strip while a turn runs; subtract it to
// recover the user's logical height when measuring from the DOM.
function composerLogicalHeight(card: HTMLElement): number {
  const strip = card.querySelector(".composer-run-strip");
  const stripHeight = strip ? strip.getBoundingClientRect().height : 0;
  return card.getBoundingClientRect().height - stripHeight;
}

function clampComposerHeight(height: number): number {
  return Math.min(Math.max(Math.round(height), COMPOSER_MIN_HEIGHT), composerMaxHeight());
}

function composerAutoInputMaxHeight(extraReservedHeight = 0): number {
  return Math.max(32, composerMaxHeight() - COMPOSER_AUTO_RESERVED_HEIGHT - extraReservedHeight);
}

function loadComposerHeight(): number | null {
  return loadOptionalLayoutSize("composerHeight", clampComposerHeight);
}

function fmtTokens(n: number): string {
  if (n >= 1000) return (n / 1000).toFixed(1).replace(/\.0$/, "") + "k";
  return String(n);
}

function fmtElapsed(ms: number): string {
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  return `${Math.floor(s / 60)}m ${s % 60}s`;
}

// --- past:chats hover preview helpers (PR-C2) ---
// Pure formatting helpers used by the past:chats list tooltip. They never read
// from disk, never call PreviewSession — they only shape the data that already
// lives in the SessionMeta snapshot we fetched on entry.
const PAST_CHAT_PREVIEW_MAX = 200;

function truncatePreview(value?: string, max = PAST_CHAT_PREVIEW_MAX): string {
  const text = (value || "").trim();
  if (text.length <= max) return text;
  return `${text.slice(0, max)}...`;
}

function fmtSessionTime(value?: number): string {
  if (!value) return "";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "";
  const yyyy = d.getFullYear();
  const mm = String(d.getMonth() + 1).padStart(2, "0");
  const dd = String(d.getDate()).padStart(2, "0");
  const hh = String(d.getHours()).padStart(2, "0");
  const mi = String(d.getMinutes()).padStart(2, "0");
  return `${yyyy}-${mm}-${dd} ${hh}:${mi}`;
}

function pastChatTitle(session: SessionMeta): string {
  return session.title || session.topicTitle || session.preview || "Untitled";
}

function useTick(on: boolean): number {
  const [, setN] = useState(0);
  useEffect(() => {
    if (!on) return;
    const id = window.setInterval(() => setN((n) => n + 1), 1000);
    return () => window.clearInterval(id);
  }, [on]);
  return Date.now();
}

function isImeKeyEvent(
  e: KeyboardEvent<HTMLElement>,
  composing: boolean,
  lastCompositionEndAt: number,
): boolean {
  const native = e.nativeEvent as globalThis.KeyboardEvent & {
    isComposing?: boolean;
    keyCode?: number;
  };
  return (
    composing ||
    native.isComposing === true ||
    native.keyCode === 229 ||
    Date.now() - lastCompositionEndAt < IME_CONFIRM_GRACE_MS
  );
}

// --- past:chats session reference → prompt context (PR-B) ---
// Send-side helpers for "@past:chats" session references. PR-A wired the menu and
// the composer-context card; this layer reads each referenced session through the
// existing PreviewSession API and prepends a compact "user / 助手" transcript to
// submitText so the model sees the referenced chat as background context.
const SESSION_REF_MAX_MESSAGES = 30;
const SESSION_REF_MAX_CHARS = 20_000;
const SESSION_CONTEXT_HEADER = "以下是用户引用的历史会话上下文：";
const SESSION_CONTEXT_FOOTER = "当前用户问题：";
const PAST_CHATS_MENU_ITEM = "past:chats";

// limitSessionMessages keeps the most recent useful messages within a char budget.
// Walks from the end so the truncation is always "drop the oldest", which matches
// the intuition that the latest turns are the relevant ones for follow-up.
function limitSessionMessages(
  messages: HistoryMessage[],
  maxMessages = SESSION_REF_MAX_MESSAGES,
  maxChars = SESSION_REF_MAX_CHARS,
): { messages: HistoryMessage[]; truncated: boolean } {
  const useful = messages
    .filter(
      (m) =>
        (m.role === "user" || m.role === "assistant") &&
        typeof m.content === "string" &&
        m.content.trim().length > 0,
    )
    .slice(-maxMessages);
  const result: HistoryMessage[] = [];
  let total = 0;
  let truncated = useful.length >= maxMessages;
  for (let i = useful.length - 1; i >= 0; i--) {
    const msg = useful[i];
    const content = msg.content.trim();
    if (total + content.length > maxChars) {
      truncated = true;
      break;
    }
    result.unshift({ ...msg, content });
    total += content.length;
  }
  if (result.length < useful.length) truncated = true;
  return { messages: result, truncated };
}

// formatSessionContext renders one referenced session as a labelled transcript.
// Falls back to a "no usable messages" note when filtering empties the list so
// the model still sees that something was referenced.
function formatSessionContext(
  ref: SessionReference,
  messages: HistoryMessage[],
  truncated: boolean,
): string {
  const body = messages
    .map((m) => `${m.role === "user" ? "用户" : "助手"}：${m.content.trim()}`)
    .join("\n\n");
  return [
    `[会话：${ref.title}]`,
    truncated ? "注意：该会话内容较长，以下只包含最近部分内容。" : "",
    body || "注意：该会话没有可引用的用户/助手消息。",
  ]
    .filter(Boolean)
    .join("\n");
}

// buildSessionContext reads each referenced session, formats the most recent
// slice, and joins them with a separator. A single failed read must not block
// the others; the user gets a clear "读取失败" note for the bad one and the
// remaining refs still flow through.
async function buildSessionContext(refs: SessionReference[]): Promise<string> {
  if (refs.length === 0) return "";
  let context = `${SESSION_CONTEXT_HEADER}\n\n`;
  for (const ref of refs) {
    try {
      const raw = await app.PreviewSession(ref.path);
      const limited = limitSessionMessages(asArray(raw));
      context += `${formatSessionContext(ref, limited.messages, limited.truncated)}\n\n---\n\n`;
    } catch (error) {
      console.error("[past:chats] failed to preview session", ref.path, error);
      context += `[会话：${ref.title}]\n注意：该会话读取失败，已跳过。\n\n---\n\n`;
    }
  }
  context += `${SESSION_CONTEXT_FOOTER}\n`;
  return context;
}

export function Composer({
  running,
  collaborationMode,
  toolApprovalMode,
  tokenMode,
  goal,
  cwd,
  modelLabel,
  imageInputEnabled = true,
  tabId,
  effort,
  onSend,
  onSteer,
  onCancel,
  onCycleMode,
  onSetMode,
  onSetCollaborationMode,
  onSetToolApprovalMode,
  onToggleYoloApprovalMode,
  onClearGoal,
  onSwitchModel,
  onSetEffort,
  onSetTokenMode,
  insertRequest,
  selectedTextRequest,
  disabled,
  submitDisabled = false,
  readOnly = false,
  decisionPending = false,
  ready,
  turnStartAt,
  turnWaitAccumMs = 0,
  promptWaitStartedAt,
  turnTokens,
  turnArgChars = 0,
  retry,
  suspendedByDecision = false,
  pendingApprovalLabel,
  pendingAsk = false,
  transientDismissSignal,
  sessionKey,
  workspaceScopeKey,
  fileRefRefreshKey,
  guidanceConsumedKey,
  guidanceConsumedText,
  guidanceQueuePreviewItems,
  showContextWindowRing = false,
  context,
  turnCost,
  cacheHitTokens,
  cacheMissTokens,
  balance,
  onInvocationMetadataChange,
}: {
  running: boolean;
  collaborationMode: CollaborationMode;
  toolApprovalMode: ToolApprovalMode;
  tokenMode: TokenMode;
  goal?: string;
  cwd?: string;
  modelLabel: string;
  imageInputEnabled?: boolean;
  tabId?: string;
  effort?: EffortInfo;
  onSend: (displayText: string, submitText?: string, tabId?: string, structured?: StructuredInvocationSubmit) => void | Promise<void>;
  onInvocationMetadataChange?: (metadata: Record<string, { kind: "skill" | "subagent"; color?: string }>) => void;
  onSteer?: (submitText: string, tabId?: string) => void | Promise<void>;
  // Returns the un-sent text when cancelling before the server replied (so it can
  // be restored to the input); undefined for a normal cancel.
  onCancel: () => string | undefined;
  onCycleMode: () => void;
  onSetMode: (mode: Mode) => void;
  onSetCollaborationMode: (mode: CollaborationMode) => void;
  onSetToolApprovalMode: (mode: ToolApprovalMode) => void;
  onToggleYoloApprovalMode: () => void;
  onClearGoal: () => void;
  onSwitchModel: (name: string) => void;
  onSetEffort: (level: string) => void;
  onSetTokenMode: (mode: TokenMode) => void;
  insertRequest?: ComposerInsertRequest | null;
  selectedTextRequest?: SelectedTextInsertRequest | null;
  disabled?: boolean;
  submitDisabled?: boolean;
  readOnly?: boolean;
  decisionPending?: boolean;
  // ready/cwd/running/workspaceScopeKey re-trigger the command fetch: Commands() returns only
  // built-ins until boot.Build finishes (the controller, hence skills/custom/MCP,
  // is nil before then), the available set changes when the workspace switches,
  // and a completed turn may have installed skills or MCP prompts.
  ready?: boolean;
  turnStartAt?: number;
  // Tab-scoped user-wait from the controller (approval/ask). Counts while the
  // tab is in the background so Composer does not invent a wait start on focus.
  turnWaitAccumMs?: number;
  promptWaitStartedAt?: number;
  turnTokens?: number;
  // Streaming tool-call argument chars (no usage event yet) — folded into the
  // pill as an estimated-token tail so a long write_file body reads as
  // progress, not a stall.
  turnArgChars?: number;
  retry?: { attempt: number; max: number };
  // True while a footer decision surface (approval / ask / clear context) owns
  // the UI. Pauses the model-work ticker without rendering a "waiting approval"
  // run strip (the decision card already conveys that state).
  suspendedByDecision?: boolean;
  // Legacy strip labels kept for isolated unit tests; App prefers
  // suspendedByDecision so the decision card is not duplicated in the strip.
  pendingApprovalLabel?: string | null;
  pendingAsk?: boolean;
  transientDismissSignal?: number;
  sessionKey?: string;
  workspaceScopeKey?: string;
  fileRefRefreshKey?: number | string;
  guidanceConsumedKey?: string;
  guidanceConsumedText?: string;
  guidanceQueuePreviewItems?: readonly string[];
  showContextWindowRing?: boolean;
  context?: ContextInfo;
  turnCost?: number;
  cacheHitTokens?: number;
  cacheMissTokens?: number;
  balance?: BalanceInfo;
}) {
  const { t, locale } = useI18n();
  const { showToast } = useToast();
  const shortcutPlatform = useMemo(() => detectShortcutPlatform(), []);
  const sendComboLabel = useShortcutComboLabel("composer.send");
  const draftKey = sessionKey || tabId || DEFAULT_COMPOSER_DRAFT_KEY;
  const now = useTick(running);
  const [text, setText] = useState("");
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [imageViewer, setImageViewer] = useState<{ open: boolean; url: string; name: string }>({ open: false, url: "", name: "" });
  const openComposerImageViewer = useCallback((url: string, name: string) => {
    setImageViewer({ open: true, url, name });
  }, []);

  const closeComposerImageViewer = useCallback(() => {
    setImageViewer((prev) => (prev.open ? { ...prev, open: false } : prev));
  }, []);

  const [workspaceRefs, setWorkspaceRefs] = useState<WorkspaceReference[]>([]);
  const [invocations, setInvocations] = useState<ComposerInvocation[]>([]);
  const [richSelection, setRichSelection] = useState<RichComposerSelection>({ start: 0, end: 0 });
  const [richSlashQuery, setRichSlashQuery] = useState<RichSlashQuery | null>(null);
  const [pastedBlocks, setPastedBlocks] = useState<PastedBlock[]>([]);
  const [openPastedLabels, setOpenPastedLabels] = useState<string[]>([]);
  const [pendingPaste, setPendingPaste] = useState(0);
  const pendingPasteRef = useRef(0);
  const pastedBlocksRef = useRef<PastedBlock[]>([]);
  const nextPasteId = useRef(1);
  const nextInvocationId = useRef(1);
  const [active, setActive] = useState(0);
  const [dismissed, setDismissed] = useState(false);
  const [dragOver, setDragOver] = useState(false);
  const [composerHeight, setComposerHeight] = useState<number | null>(loadComposerHeight);
  const [composerResizing, setComposerResizing] = useState(false);
  const [textareaAutoHeight, setTextareaAutoHeight] = useState<number | null>(null);
  const [textareaAutoOverflow, setTextareaAutoOverflow] = useState(false);
  const [intentMenuOpen, setIntentMenuOpen] = useState(false);
  const [intentMenuClosing, setIntentMenuClosing] = useState(false);
  const [profileMenuOpen, setProfileMenuOpen] = useState(false);
  const [profileMenuClosing, setProfileMenuClosing] = useState(false);
  const [moreMenuOpen, setMoreMenuOpen] = useState(false);
  const [moreMenuClosing, setMoreMenuClosing] = useState(false);
  const [contentMenuOpen, setContentMenuOpen] = useState(false);
  const [showPastChats, setShowPastChats] = useState(false);
  const [directPastChats, setDirectPastChats] = useState(false);
  const [pastChats, setPastChats] = useState<SessionMeta[]>([]);
  const [pastChatQuery, setPastChatQuery] = useState("");
  const [sessionRefs, setSessionRefs] = useState<SessionReference[]>([]);
  const [selectedTextRefs, setSelectedTextRefs] = useState<SelectedTextReference[]>([]);
  const [pendingGuidance, setPendingGuidance] = useState<PendingGuidance[]>([]);
  const [guidanceExpanded, setGuidanceExpanded] = useState(false);
  const [guidanceSendingId, setGuidanceSendingId] = useState<number | null>(null);
  const [guidanceDraftKey, setGuidanceDraftKey] = useState(draftKey);
  const pendingGuidanceRef = useRef<PendingGuidance[]>([]);
  const guidanceExpandedRef = useRef(false);
  const guidanceSendingIdRef = useRef<number | null>(null);
  const nextGuidanceId = useRef(1);
  const [loadingPastChats, setLoadingPastChats] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [inputMenuPoint, setInputMenuPoint] = useState<ContextMenuPoint | null>(null);
  const [composerPrompt, setComposerPrompt] = useState<string | null>(null);
  // Prompt history navigation (plain ↑/↓)
  // Use refs for values read inside async closures to avoid stale captures
  // on rapid key presses (the React closure trap).
  const historyIndexRef = useRef(-1);
  const historyEntriesRef = useRef<PromptHistoryEntry[]>([]);
  const historyLoadRef = useRef<Promise<void> | null>(null);
  const historyGenerationRef = useRef(cacheGeneration());
  // historyIndex state is written (via setHistoryIndex) for potential future
  // UI feedback (e.g. "3/200" indicator); currently unused in render.
  const [, setHistoryIndex] = useState(-1);
  const savedTextRef = useRef("");
  const taRef = useRef<HTMLTextAreaElement>(null);
  const richInputRef = useRef<RichComposerInputHandle>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const composerCardRef = useRef<HTMLDivElement>(null);
  const contentMenuAnchorRef = useRef<HTMLButtonElement>(null);
  const intentMenuAnchorRef = useRef<HTMLButtonElement>(null);
  const profileMenuAnchorRef = useRef<HTMLButtonElement>(null);
  const moreMenuAnchorRef = useRef<HTMLButtonElement>(null);
  const intentCloseTimerRef = useRef<number | null>(null);
  const profileCloseTimerRef = useRef<number | null>(null);
  const moreCloseTimerRef = useRef<number | null>(null);
  const wasRunningByDraftRef = useRef<Record<string, boolean>>({ [draftKey]: running });
  const composingRef = useRef(false);
  const lastCompositionEndAt = useRef(0);
  const lastSelectionRef = useRef({ start: 0, end: 0 });
  const consumedInsertIdByDraftRef = useRef<Record<string, number>>({});
  const consumedSelectedTextIdByDraftRef = useRef<Record<string, number>>({});
  const lastTransientDismissSignal = useRef(transientDismissSignal);
  const lastGuidanceConsumedKeyByDraftRef = useRef<Record<string, string | undefined>>(
    guidanceConsumedKey ? { [draftKey]: guidanceConsumedKey } : {},
  );
  const selfDispatchedGuidanceByDraftRef = useRef<Record<string, string[]>>({});
  const submittingRef = useRef(false);
  const nativeClipboardPasteTimerRef = useRef<number | null>(null);
  // Snapshot of the current cwd so async callbacks (openPastChats) can detect
  // workspace switches and discard stale responses (issue #3601).
  const cwdRef = useRef(cwd);
  cwdRef.current = cwd;
  const attachmentDedupRef = useRef(new DedupIndex());
  const attachmentDedupKeysRef = useRef<Record<string, AttachmentDedupKey>>({});
  const guidanceQueuePreviewKey = (guidanceQueuePreviewItems ?? []).map((item) => item.trim()).filter(Boolean).join("\n");
  const draftsBySessionRef = useRef<Record<string, ComposerDraft>>({});
  const activeDraftKeyRef = useRef(draftKey);
  const textRef = useRef(text);
  const invocationsRef = useRef(invocations);
  const attachmentsRef = useRef(attachments);
  const workspaceRefsRef = useRef(workspaceRefs);
  const openPastedLabelsRef = useRef(openPastedLabels);
  const sessionRefsRef = useRef(sessionRefs);
  const selectedTextRefsRef = useRef(selectedTextRefs);
  textRef.current = text;
  invocationsRef.current = invocations;
  attachmentsRef.current = attachments;
  workspaceRefsRef.current = workspaceRefs;
  pastedBlocksRef.current = pastedBlocks;
  openPastedLabelsRef.current = openPastedLabels;
  sessionRefsRef.current = sessionRefs;
  selectedTextRefsRef.current = selectedTextRefs;
  pendingGuidanceRef.current = pendingGuidance;
  guidanceExpandedRef.current = guidanceExpanded;
  guidanceSendingIdRef.current = guidanceSendingId;
  pendingPasteRef.current = pendingPaste;
  submittingRef.current = submitting;

  const snapshotComposerDraft = (): ComposerDraft => ({
    text: textRef.current,
    invocations: invocationsRef.current.map((invocation) => ({ ...invocation, command: { ...invocation.command } })),
    attachments: [...attachmentsRef.current],
    workspaceRefs: [...workspaceRefsRef.current],
    pastedBlocks: [...pastedBlocksRef.current],
    openPastedLabels: [...openPastedLabelsRef.current],
    sessionRefs: [...sessionRefsRef.current],
    selectedTextRefs: selectedTextRefsRef.current.map((reference) => ({ ...reference })),
    attachmentDedupKeys: { ...attachmentDedupKeysRef.current },
    nextPasteId: nextPasteId.current,
    historyIndex: historyIndexRef.current,
    savedText: savedTextRef.current,
    pendingGuidance: pendingGuidanceRef.current.map((item) => ({ ...item })),
    guidanceExpanded: guidanceExpandedRef.current,
    guidanceSendingId: guidanceSendingIdRef.current,
    pendingPaste: pendingPasteRef.current,
    submitting: submittingRef.current,
  });

  const restoreComposerDraft = (draft: ComposerDraft) => {
    const next = cloneComposerDraft(draft);
    textRef.current = next.text;
    invocationsRef.current = next.invocations;
    attachmentsRef.current = next.attachments;
    workspaceRefsRef.current = next.workspaceRefs;
    openPastedLabelsRef.current = next.openPastedLabels;
    sessionRefsRef.current = next.sessionRefs;
    selectedTextRefsRef.current = next.selectedTextRefs;
    setText(next.text);
    setInvocations(next.invocations);
    setRichSlashQuery(null);
    setAttachments(next.attachments);
    setWorkspaceRefs(next.workspaceRefs);
    pastedBlocksRef.current = next.pastedBlocks;
    setPastedBlocks(next.pastedBlocks);
    setOpenPastedLabels(next.openPastedLabels);
    setSessionRefs(next.sessionRefs);
    setSelectedTextRefs(next.selectedTextRefs);
    attachmentDedupKeysRef.current = next.attachmentDedupKeys;
    attachmentDedupRef.current = attachmentDedupFromKeys(next.attachmentDedupKeys);
    nextPasteId.current = next.nextPasteId;
    historyIndexRef.current = next.historyIndex;
    savedTextRef.current = next.savedText;
    pendingGuidanceRef.current = next.pendingGuidance;
    guidanceExpandedRef.current = next.guidanceExpanded;
    guidanceSendingIdRef.current = next.guidanceSendingId;
    pendingPasteRef.current = next.pendingPaste;
    submittingRef.current = next.submitting;
    setPendingGuidance(next.pendingGuidance);
    setGuidanceExpanded(next.guidanceExpanded);
    setGuidanceSendingId(next.guidanceSendingId);
    setPendingPaste(next.pendingPaste);
    setSubmitting(next.submitting);
    setHistoryIndex(next.historyIndex);
    lastSelectionRef.current = { start: next.text.length, end: next.text.length };
    setComposerPrompt(null);
    setShowPastChats(false);
    setDirectPastChats(false);
    setContentMenuOpen(false);
    setPastChatQuery("");
    setLoadingPastChats(false);
    setActive(0);
    setInputMenuPoint(null);
    setDragOver(false);
    setImageViewer((current) => current.open ? { ...current, open: false } : current);
    setIntentMenuOpen(false);
    setIntentMenuClosing(false);
    setMoreMenuOpen(false);
    setMoreMenuClosing(false);
  };

  const updatePendingGuidanceForDraft = (
    targetDraftKey: string,
    update: (items: PendingGuidance[]) => PendingGuidance[],
  ) => {
    if (targetDraftKey === activeDraftKeyRef.current) {
      const next = update(pendingGuidanceRef.current);
      pendingGuidanceRef.current = next;
      setPendingGuidance(next);
      return;
    }
    const draft = cloneComposerDraft(draftsBySessionRef.current[targetDraftKey] ?? emptyComposerDraft());
    draft.pendingGuidance = update(draft.pendingGuidance);
    draftsBySessionRef.current[targetDraftKey] = draft;
  };

  const updateGuidanceSendingIdForDraft = (targetDraftKey: string, next: number | null) => {
    if (targetDraftKey === activeDraftKeyRef.current) {
      guidanceSendingIdRef.current = next;
      setGuidanceSendingId(next);
      return;
    }
    const draft = cloneComposerDraft(draftsBySessionRef.current[targetDraftKey] ?? emptyComposerDraft());
    draft.guidanceSendingId = next;
    draftsBySessionRef.current[targetDraftKey] = draft;
  };

  const updatePendingPasteForDraft = (targetDraftKey: string, delta: number) => {
    if (targetDraftKey === activeDraftKeyRef.current) {
      const next = Math.max(0, pendingPasteRef.current + delta);
      pendingPasteRef.current = next;
      setPendingPaste(next);
      return;
    }
    const draft = cloneComposerDraft(draftsBySessionRef.current[targetDraftKey] ?? emptyComposerDraft());
    draft.pendingPaste = Math.max(0, draft.pendingPaste + delta);
    draftsBySessionRef.current[targetDraftKey] = draft;
  };

  const updateSubmittingForDraft = (targetDraftKey: string, next: boolean) => {
    if (targetDraftKey === activeDraftKeyRef.current) {
      submittingRef.current = next;
      setSubmitting(next);
      return;
    }
    const draft = cloneComposerDraft(draftsBySessionRef.current[targetDraftKey] ?? emptyComposerDraft());
    draft.submitting = next;
    draftsBySessionRef.current[targetDraftKey] = draft;
  };

  const draftIsSubmitting = (targetDraftKey: string): boolean =>
    targetDraftKey === activeDraftKeyRef.current
      ? submittingRef.current
      : Boolean(draftsBySessionRef.current[targetDraftKey]?.submitting);

  const draftHasPendingPaste = (targetDraftKey: string): boolean =>
    targetDraftKey === activeDraftKeyRef.current
      ? pendingPasteRef.current > 0
      : (draftsBySessionRef.current[targetDraftKey]?.pendingPaste ?? 0) > 0;

  useLayoutEffect(() => {
    const previousKey = activeDraftKeyRef.current;
    if (previousKey === draftKey) return;
    draftsBySessionRef.current[previousKey] = snapshotComposerDraft();
    activeDraftKeyRef.current = draftKey;
    setGuidanceDraftKey(draftKey);
    restoreComposerDraft(draftsBySessionRef.current[draftKey] ?? emptyComposerDraft());
  }, [draftKey]);

  useEffect(() => {
    return () => {
      draftsBySessionRef.current[activeDraftKeyRef.current] = snapshotComposerDraft();
    };
  }, []);

  const clearNativeClipboardPasteTimer = () => {
    if (nativeClipboardPasteTimerRef.current === null) return;
    window.clearTimeout(nativeClipboardPasteTimerRef.current);
    nativeClipboardPasteTimerRef.current = null;
  };

  useEffect(() => () => clearNativeClipboardPasteTimer(), []);

  useEffect(() => {
    const wasRunning = wasRunningByDraftRef.current[draftKey] ?? running;
    if (wasRunning && !running) {
      setGuidanceExpanded(false);
      if (text.trim() === "") {
        pastedBlocksRef.current = [];
        setPastedBlocks([]);
        setOpenPastedLabels([]);
      }
    }
    wasRunningByDraftRef.current[draftKey] = running;
  }, [draftKey, running, text]);

  // A message queued while a turn was running (without the explicit "guide"
  // steer click) is the user's next turn, not scratch text to discard — send
  // it once the turn is done. Gated on submitDisabled, not just running:
  // if the turn ends while the controller is still activating/hydrating,
  // App's onSend silently no-ops on !controllerReady, but sendQueuedGuidance
  // still removes the item as if it had sent — so wait for submitDisabled to
  // clear instead of firing into that no-op window (#6210 follow-up). Once
  // both conditions hold, a successful send removes the head and starts a
  // new turn, which flips `running` true then false again, re-running this
  // effect to drain the shelf one item at a time; a failed send is left in
  // place (dismissible via the trash button) rather than silently dropped.
  // guidanceDraftKey identifies which session the rendered queue belongs to:
  // during a tab switch React still renders once with the previous queue, and
  // that stale render must never submit through the new session's onSend.
  useEffect(() => {
    // Never auto-send guidance while a decision surface owns the footer —
    // the draft must stay intact until the user finishes the decision.
    if (guidanceDraftKey !== draftKey || running || submitDisabled || suspendedByDecision) return;
    const next = pendingGuidance[0];
    if (next) void sendQueuedGuidance(next, draftKey);
  }, [draftKey, guidanceDraftKey, running, submitDisabled, pendingGuidance, suspendedByDecision]);

  useEffect(() => {
    if (guidanceDraftKey !== draftKey || !running || !guidanceQueuePreviewKey) return;
    setGuidanceExpanded(false);
    updatePendingGuidanceForDraft(
      draftKey,
      () =>
        guidanceQueuePreviewKey
          .split("\n")
          .map((text) => ({ id: nextGuidanceId.current++, text, submitText: text })),
    );
  }, [draftKey, guidanceDraftKey, guidanceQueuePreviewKey, running]);

  useEffect(() => {
    if (guidanceExpanded && pendingGuidance.length <= 2) setGuidanceExpanded(false);
  }, [guidanceExpanded, pendingGuidance.length]);

  // --- slash commands ---
  const [commands, setCommands] = useState<CommandInfo[]>([]);
  useEffect(() => {
    let live = true;
    app.Commands()
      .then((next) => {
        if (live) setCommands(asArray(next));
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [ready, cwd, running, workspaceScopeKey]);
  useEffect(() => {
    onInvocationMetadataChange?.(Object.fromEntries(
      commands
        .filter(commandUsesStructuredInvocation)
        .map((command) => [command.name, {
          kind: command.kind === "subagent" ? "subagent" : "skill",
          color: command.color,
        }]),
    ));
  }, [commands, onInvocationMetadataChange]);

  const slashText = useMemo(() => text.replace(/[\r\n]+$/u, ""), [text]);
  const slashQuery = useMemo(() => {
    if (invocations.length > 0) return richSlashQuery?.query ?? null;
    if (!slashText.startsWith("/") || /\s/.test(slashText)) return null;
    return slashText.slice(1).toLowerCase();
  }, [invocations.length, richSlashQuery, slashText]);
  const slashMatches = useMemo(
    () => slashQuery === null
      ? []
      : sortSlashCommandsForMenu(commands.filter((c) => c.name.toLowerCase().includes(slashQuery))),
    [slashQuery, commands],
  );

  // --- slash argument completion ("/cmd <args>") --- mirrors the CLI: once past
  // the command word, the backend suggests sub-commands (/skill → list/show/…,
  // /mcp → add/remove, /model → refs). Fetched from app.SlashArgs. Debounced
  // by 120ms so rapid typing doesn't flood the backend with IPC calls — the
  // menu only updates after the user pauses.
  const [argRes, setArgRes] = useState<SlashArgsResult | null>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => {
    if (invocations.length > 0 || !slashText.startsWith("/") || !/\s/.test(slashText)) {
      setArgRes(null);
      return;
    }
    let live = true;
    clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      app
        .SlashArgs(slashText)
        .then((r) => {
          if (!live) return;
          // Drop suggestions that wouldn't change the input — the token is already
          // fully typed (e.g. "/skill list" offering "list"). Otherwise the menu
          // lingers on a complete command and Enter keeps "accepting" a no-op
          // instead of sending. (Defense-in-depth: the backend filters these too.)
          // r.items can arrive as null (an empty Go slice serializes to JSON null),
          // so guard before filtering — otherwise the throw is swallowed and the
          // stale menu from the previous keystroke lingers (the /skill list bug).
          const items = asArray(r?.items);
          const from = r?.from ?? 0;
          const useful = items.filter((it) => slashText.slice(0, from) + it.insert !== slashText);
          setArgRes(useful.length > 0 ? { items: useful, from } : null);
          setActive(0);
        })
        .catch(() => {});
    }, 120);
    return () => {
      live = false;
      clearTimeout(debounceRef.current);
    };
  }, [invocations.length, slashText]);

  // --- @ file references (token at the end of the text) ---
  // atRaw is everything after a trailing "@token"; atDir is its path up to the
  // last "/", atFrag the part after. The menu lists one directory level (atDir)
  // and filters by atFrag — descending one level per pick.
  const activeAtToken = useMemo(() => activeFileReferenceToken(text), [text]);
  const atRaw = activeAtToken?.raw ?? null;
  const atDir = activeAtToken?.dir ?? "";
  const atFrag = activeAtToken?.frag ?? "";
  const pastChatToken = useMemo(() => activePastChatToken(text), [text]);
  const pastChatTokenQuery = pastChatToken?.query ?? null;

  const [entries, setEntries] = useState<DirEntry[]>([]);
  const [searchEntries, setSearchEntries] = useState<DirEntry[]>([]);
  const dirCache = useRef<Record<string, DirEntry[]>>({});
  const searchCache = useRef<Record<string, FileRefSearchCacheEntry>>({});
  const fileRefTabId = tabId ?? "";
  const fileRefScopeKey = workspaceScopeKey ?? `${fileRefTabId}\u0000${cwd ?? ""}`;

  const clearFileRefState = useCallback(() => {
    dirCache.current = {};
    searchCache.current = {};
    setEntries([]);
    setSearchEntries([]);
    setShowPastChats(false);
    setPastChats([]);
    setPastChatQuery("");
    setLoadingPastChats(false);
    setActive(0);
    setDismissed(false);
  }, []);

  // Controller/session changes invalidate @ mention state even when tab and
  // workspace identities stay the same (saved-session rebinds and rebuilds).
  const prevFileRefScopeRef = useRef(fileRefScopeKey);
  useEffect(() => {
    if (prevFileRefScopeRef.current === fileRefScopeKey) return;
    prevFileRefScopeRef.current = fileRefScopeKey;
    clearFileRefState();
  }, [clearFileRefState, fileRefScopeKey]);

  const prevFileRefRefreshKeyRef = useRef(fileRefRefreshKey);
  useEffect(() => {
    if (prevFileRefRefreshKeyRef.current === fileRefRefreshKey) return;
    prevFileRefRefreshKeyRef.current = fileRefRefreshKey;
    clearFileRefState();
  }, [clearFileRefState, fileRefRefreshKey]);

  useEffect(() => {
    if (atRaw === null) return;
    const cached = dirCache.current[atDir];
    if (cached) {
      setEntries(cached);
    } else {
      setEntries([]);
    }
    let live = true;
    app
      .ListDirForTab(fileRefTabId, unescapeRefPath(atDir))
      .then((es) => {
        const list = asArray(es);
        if (!live) return;
        dirCache.current[atDir] = list;
        setEntries(list);
      })
      .catch(() => {});
    return () => {
      live = false;
    };
    // Re-fetch when the menu opens, the directory level changes, or the
    // workspace tree refreshes; cached data is only a fast first paint.
  }, [atRaw === null, atDir, fileRefRefreshKey, fileRefScopeKey, fileRefTabId]);
  useEffect(() => {
    if (atRaw === null || atDir !== "" || atFrag === "") {
      setSearchEntries([]);
      return;
    }
    const cached = searchCache.current[atFrag];
    if (cached) {
      setSearchEntries(cached.entries);
      if (Date.now() - cached.cachedAt < FILE_REF_SEARCH_CACHE_TTL_MS) return;
    } else {
      setSearchEntries([]);
    }
    let live = true;
    app
      .SearchFileRefsForTab(fileRefTabId, atFrag)
      .then((es) => {
        const list = asArray(es);
        if (!live) return;
        searchCache.current[atFrag] = { entries: list, cachedAt: Date.now() };
        setSearchEntries(list);
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [atRaw === null, atDir, atFrag, fileRefRefreshKey, fileRefScopeKey, fileRefTabId]);
  const atMatches = useMemo(
    () => {
      if (atRaw === null) return [];
      return filterAtMatches(entries, searchEntries, atFrag);
    },
    [atRaw, atFrag, entries, searchEntries],
  );

  // Unified menu item model for the @ menu. "past:chats" is a real selectable
  // item (kind "pastChats"), not an active===0 special case.
  type AtMenuItem =
    | { kind: "pastChats" }
    | { kind: "file"; entry: DirEntry };

  const includePastChatsItem = atRaw !== null && atDir === "" && (atFrag === "" || PAST_CHATS_MENU_ITEM.startsWith(atFrag));

  const atMenuItems = useMemo<AtMenuItem[]>(
    () => [
      ...(includePastChatsItem ? [{ kind: "pastChats" as const }] : []),
      ...atMatches.map((entry) => ({ kind: "file" as const, entry })),
    ],
    [includePastChatsItem, atMatches],
  );


  // --- which menu (if any) is open --- (slash command names win; then slash
  // arguments; then @-refs — they're rarely valid at once)
  const menuMode: "slash" | "slasharg" | "at" | "pastChats" | null =
    directPastChats
      ? "pastChats"
      : slashMatches.length > 0 && !dismissed
        ? "slash"
        : argRes && argRes.items.length > 0 && !dismissed
          ? "slasharg"
          : atRaw !== null && !dismissed
            ? "at"
            : null;
  const countBase =
    menuMode === "slash"
      ? slashMatches.length
      : menuMode === "slasharg"
        ? argRes!.items.length
        : menuMode === "at"
          ? atMenuItems.length
          : menuMode === "pastChats"
            ? pastChats.length
            : 0;

  // Reset highlight + un-dismiss whenever the active query changes.
  useEffect(() => {
    setActive(0);
    setDismissed(false);
  }, [slashQuery, atRaw, pastChatTokenQuery]);

  useEffect(() => {
    if (transientDismissSignal === undefined || transientDismissSignal === lastTransientDismissSignal.current) return;
    lastTransientDismissSignal.current = transientDismissSignal;
    setDismissed(true);
  }, [transientDismissSignal]);

  const takeSelfDispatchedGuidance = useCallback((text: string, targetDraftKey: string): boolean => {
    const selfDispatched = selfDispatchedGuidanceByDraftRef.current[targetDraftKey] ?? [];
    const idx = selfDispatched.findIndex((queued) => guidanceTextMatches(queued, text));
    if (idx < 0) return false;
    selfDispatched.splice(idx, 1);
    if (selfDispatched.length === 0) delete selfDispatchedGuidanceByDraftRef.current[targetDraftKey];
    return true;
  }, []);

  useEffect(() => {
    if (guidanceDraftKey !== draftKey || !guidanceConsumedKey) return;
    if (guidanceConsumedKey === lastGuidanceConsumedKeyByDraftRef.current[draftKey]) return;
    lastGuidanceConsumedKeyByDraftRef.current[draftKey] = guidanceConsumedKey;
    const consumed = (guidanceConsumedText ?? "").trim();
    if (consumed && takeSelfDispatchedGuidance(consumed, draftKey)) return;
    updatePendingGuidanceForDraft(draftKey, (items) => {
      if (items.length === 0) return items;
      const idx = consumed
        ? items.findIndex((item) => guidanceTextMatches(item.submitText, consumed) || guidanceTextMatches(item.text, consumed))
        : -1;
      // Only remove on a real match. Steer notices also fire for guidance this
      // client never queued (another window, bot bridge, turn-end flush) —
      // falling back to dropping items[0] silently deleted unrelated queued
      // guidance (#6238).
      if (idx < 0) return items;
      return items.filter((_, index) => index !== idx);
    });
  }, [draftKey, guidanceDraftKey, guidanceConsumedKey, guidanceConsumedText, takeSelfDispatchedGuidance]);

  // When the @ trigger disappears (user deleted the @), close the past:chats
  // sub-menu and reset related state. Without this, showPastChats can outlive
  // the @ token and leave the session list visible with no way to dismiss it.
  useEffect(() => {
    if (menuMode !== "at" && menuMode !== "pastChats" && showPastChats) {
      setShowPastChats(false);
      setPastChatQuery("");
      setActive(0);
    }
  }, [menuMode]);

  useEffect(() => {
    if (menuMode && menuMode !== "pastChats") setContentMenuOpen(false);
  }, [menuMode]);

  // A starting run closes the transient content surfaces. Without this the
  // popover state survives the run (its open prop gates on !running) and the
  // menu would pop back unprompted the moment the turn finishes.
  useEffect(() => {
    if (!running) return;
    setContentMenuOpen(false);
    setDirectPastChats(false);
    setShowPastChats(false);
    setPastChatQuery("");
    if (pastChatToken) setDismissed(true);
  }, [pastChatToken, running]);

  const resetPromptHistoryNavigation = () => {
    if (historyIndexRef.current === -1) return;
    historyIndexRef.current = -1;
    setHistoryIndex(-1);
  };

  const syncPromptHistoryGeneration = () => {
    const nextGeneration = cacheGeneration();
    if (historyGenerationRef.current === nextGeneration) return;
    historyGenerationRef.current = nextGeneration;
    historyEntriesRef.current = [];
    historyLoadRef.current = null;
    historyIndexRef.current = -1;
    setHistoryIndex(-1);
  };

  const ensurePromptHistoryIndex = async (index: number): Promise<boolean> => {
    if (index < historyEntriesRef.current.length) return true;
    if (historyLoadRef.current) await historyLoadRef.current;
    while (index >= historyEntriesRef.current.length) {
      let loaded = 0;
      const task = loadOlder().then((entries) => {
        loaded = entries.length;
        if (loaded > 0) {
          historyEntriesRef.current = historyEntriesRef.current.concat(entries);
        }
      });
      historyLoadRef.current = task;
      await task;
      historyLoadRef.current = null;
      if (loaded === 0) return index < historyEntriesRef.current.length;
    }
    return true;
  };

  const prefetchPromptHistoryTail = () => {
    if (historyLoadRef.current) return;
    void ensurePromptHistoryIndex(historyEntriesRef.current.length);
  };

  const focusComposerInput = () => {
    if (invocationsRef.current.length > 0) richInputRef.current?.focus();
    else taRef.current?.focus();
  };

  const getComposerSelection = () => {
    if (invocationsRef.current.length > 0) return richInputRef.current?.getSelection() ?? richSelection;
    const ta = taRef.current;
    const start = ta?.selectionStart ?? textRef.current.length;
    const end = ta?.selectionEnd ?? start;
    return { start: Math.min(start, end), end: Math.max(start, end) };
  };

  const setComposerSelection = (start: number, end = start) => {
    requestAnimationFrame(() => {
      if (invocationsRef.current.length > 0) {
        richInputRef.current?.setSelectionRange(start, end);
        return;
      }
      const ta = taRef.current;
      if (!ta) return;
      ta.focus();
      ta.setSelectionRange(start, end);
      lastSelectionRef.current = { start, end };
    });
  };

  const focusComposerFromContentBlank = (event: ReactMouseEvent<HTMLDivElement>) => {
    if (event.target !== event.currentTarget || disabled || readOnly) return;
    event.preventDefault();
    setComposerSelection(textRef.current.length);
  };

  const setTextCaretEnd = (next: string) => {
    textRef.current = next;
    setText(next);
    setComposerSelection(next.length);
  };

  const rememberCaret = () => {
    if (invocationsRef.current.length > 0) {
      const selection = richInputRef.current?.getSelection();
      if (selection) lastSelectionRef.current = { start: selection.start, end: selection.end };
      return;
    }
    const ta = taRef.current;
    if (!ta) return;
    lastSelectionRef.current = { start: ta.selectionStart ?? text.length, end: ta.selectionEnd ?? text.length };
  };

  const insertNewlineAtCaret = () => {
    const selection = getComposerSelection();
    const updated = insertComposerNewline(textRef.current, invocationsRef.current, selection);
    textRef.current = updated.text;
    invocationsRef.current = updated.invocations;
    setText(updated.text);
    setInvocations(updated.invocations);
    setComposerSelection(selection.start + 1);
  };

  const insertTextAtCaret = (snippet: string) => {
    const selection = getComposerSelection();
    const start = selection.start;
    const end = selection.end;
    const before = text.slice(0, start);
    const after = text.slice(end);
    const leading = before.length === 0 || before.endsWith("\n\n") ? "" : before.endsWith("\n") ? "\n" : "\n\n";
    const body = snippet.trimEnd();
    const trailing = after.length === 0 ? "\n" : after.startsWith("\n") ? "" : "\n\n";
    const inserted = leading + body + trailing;
    const pos = before.length + inserted.length;
    const updated = replaceInvocationTextRange(text, invocationsRef.current, start, end, inserted);
    textRef.current = updated.text;
    invocationsRef.current = updated.invocations;
    setText(updated.text);
    setInvocations(updated.invocations);
    setComposerSelection(pos);
  };

  const replaceComposerText = (next: string) => {
    clearAttachments();
    setWorkspaceRefs([]);
    setSessionRefs([]);
    selectedTextRefsRef.current = [];
    setSelectedTextRefs([]);
    pastedBlocksRef.current = [];
    setPastedBlocks([]);
    setOpenPastedLabels([]);
    setTextCaretEnd(next);
  };

  const addWorkspaceReference = (ref: WorkspaceReference) => {
    setWorkspaceRefs((prev) => {
      const key = workspaceReferenceKey(ref);
      if (prev.some((item) => workspaceReferenceKey(item) === key)) return prev;
      const next = [...prev, ref];
      workspaceRefsRef.current = next;
      return next;
    });
    requestAnimationFrame(focusComposerInput);
  };

  useEffect(() => {
    if (!insertRequest || insertRequest.id === consumedInsertIdByDraftRef.current[draftKey]) return;
    consumedInsertIdByDraftRef.current[draftKey] = insertRequest.id;
    if (insertRequest.mode === "replace") {
      replaceComposerText(insertRequest.text);
      return;
    }
    if (insertRequest.mode === "prefix") {
      const prefix = `${insertRequest.text.trimEnd()} `;
      const current = textRef.current;
      setTextCaretEnd(current ? prefix + current : prefix);
      return;
    }
    const ref = parseWorkspaceReference(insertRequest.text);
    if (ref) {
      addWorkspaceReference(ref);
      return;
    }
    insertTextAtCaret(insertRequest.text);
  }, [draftKey, insertRequest]);

  useEffect(() => {
    if (!selectedTextRequest || selectedTextRequest.id === consumedSelectedTextIdByDraftRef.current[draftKey]) return;
    consumedSelectedTextIdByDraftRef.current[draftKey] = selectedTextRequest.id;
    const normalized = normalizeSelectedText(selectedTextRequest.text);
    if (!normalized.text) return;
    if (normalized.truncated) showToast(t("composer.selectedTextTruncated"), "warn");
    const path = selectedTextRequest.path;
    const duplicate = selectedTextRefsRef.current.some(
      (reference) => reference.text === normalized.text && (reference.path ?? "") === (path ?? ""),
    );
    if (!duplicate) {
      const next = [
        ...selectedTextRefsRef.current,
        {
          id: `${path ? "code" : "chat"}-selection-${selectedTextRequest.id}`,
          text: normalized.text,
          ...(path ? { path } : {}),
        },
      ];
      selectedTextRefsRef.current = next;
      setSelectedTextRefs(next);
    }
    requestAnimationFrame(focusComposerInput);
  }, [draftKey, selectedTextRequest, showToast, t]);

  const expandPastedBlocks = (displayText: string, blocks = pastedBlocksRef.current): string => {
    let expanded = displayText;
    for (const block of blocks) {
      if (expanded.includes(block.label)) {
        expanded = expanded.split(block.label).join(renderPastedBlock(block));
      }
    }
    return expanded;
  };

  const rememberAttachment = (path: string, key: AttachmentDedupKey) => {
    attachmentDedupRef.current.add(key.hash, key.source);
    attachmentDedupKeysRef.current[path] = key;
  };

  const forgetAttachment = (path: string) => {
    const key = attachmentDedupKeysRef.current[path];
    if (key) {
      attachmentDedupRef.current.forget(key.hash, key.source);
      delete attachmentDedupKeysRef.current[path];
    }
  };

  const clearAttachments = () => {
    attachmentsRef.current = [];
    setAttachments([]);
    attachmentDedupRef.current.clear();
    attachmentDedupKeysRef.current = {};
  };

  const removeAttachment = (path: string) => {
    forgetAttachment(path);
    setAttachments(attachmentsRef.current.filter((x) => x.path !== path));
    requestAnimationFrame(focusComposerInput);
  };

  const attachmentSeenInDraft = (targetDraftKey: string, key: AttachmentDedupKey): boolean => {
    if (targetDraftKey === activeDraftKeyRef.current) return attachmentDedupRef.current.seen(key.hash, key.source);
    const draft = draftsBySessionRef.current[targetDraftKey];
    return draft ? draftHasAttachmentDedupKey(draft, key) : false;
  };

  const addAttachmentToDraft = (targetDraftKey: string, attachment: Attachment, key: AttachmentDedupKey): boolean => {
    if (targetDraftKey === activeDraftKeyRef.current) {
      if (attachmentDedupRef.current.seen(key.hash, key.source)) return false;
      rememberAttachment(attachment.path, key);
      const next = [...attachmentsRef.current, attachment];
      attachmentsRef.current = next;
      setAttachments(next);
      return true;
    }
    const draft = cloneComposerDraft(draftsBySessionRef.current[targetDraftKey] ?? emptyComposerDraft());
    if (draftHasAttachmentDedupKey(draft, key)) return false;
    draft.attachmentDedupKeys[attachment.path] = key;
    draft.attachments = [...draft.attachments, attachment];
    draftsBySessionRef.current[targetDraftKey] = draft;
    return true;
  };

  const addWorkspaceReferenceToDraft = (targetDraftKey: string, ref: WorkspaceReference) => {
    if (targetDraftKey === activeDraftKeyRef.current) {
      addWorkspaceReference(ref);
      return;
    }
    const draft = cloneComposerDraft(draftsBySessionRef.current[targetDraftKey] ?? emptyComposerDraft());
    const key = workspaceReferenceKey(ref);
    if (draft.workspaceRefs.some((item) => workspaceReferenceKey(item) === key)) return;
    draft.workspaceRefs = [...draft.workspaceRefs, ref];
    draftsBySessionRef.current[targetDraftKey] = draft;
  };

  const clearSubmittedDraft = (targetDraftKey: string) => {
    if (targetDraftKey === activeDraftKeyRef.current) {
      textRef.current = "";
      setText("");
      invocationsRef.current = [];
      setInvocations([]);
      setRichSlashQuery(null);
      historyIndexRef.current = -1;
      setHistoryIndex(-1);
      clearAttachments();
      workspaceRefsRef.current = [];
      setWorkspaceRefs([]);
      sessionRefsRef.current = [];
      setSessionRefs([]);
      selectedTextRefsRef.current = [];
      setSelectedTextRefs([]);
      pastedBlocksRef.current = [];
      setPastedBlocks([]);
      openPastedLabelsRef.current = [];
      setOpenPastedLabels([]);
      savedTextRef.current = "";
      return;
    }
    const draft = cloneComposerDraft(draftsBySessionRef.current[targetDraftKey] ?? emptyComposerDraft());
    draft.text = "";
    draft.invocations = [];
    draft.attachments = [];
    draft.workspaceRefs = [];
    draft.pastedBlocks = [];
    draft.openPastedLabels = [];
    draft.sessionRefs = [];
    draft.selectedTextRefs = [];
    draft.attachmentDedupKeys = {};
    draft.historyIndex = -1;
    draft.savedText = "";
    draftsBySessionRef.current[targetDraftKey] = draft;
  };

  const clearIntentCloseTimer = useCallback(() => {
    if (intentCloseTimerRef.current === null) return;
    window.clearTimeout(intentCloseTimerRef.current);
    intentCloseTimerRef.current = null;
  }, []);

  const openIntentMenu = useCallback(() => {
    clearIntentCloseTimer();
    setContentMenuOpen(false);
    setDirectPastChats(false);
    setDismissed(true);
    setIntentMenuClosing(false);
    setIntentMenuOpen(true);
  }, [clearIntentCloseTimer]);

  const closeIntentMenu = useCallback((afterClose?: () => void) => {
    clearIntentCloseTimer();
    setIntentMenuClosing(true);
    window.requestAnimationFrame(() => setIntentMenuOpen(false));
    const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    intentCloseTimerRef.current = window.setTimeout(() => {
      intentCloseTimerRef.current = null;
      setIntentMenuClosing(false);
      afterClose?.();
    }, reduceMotion ? 0 : ANCHORED_POPOVER_CLOSE_MS);
  }, [clearIntentCloseTimer]);

  useEffect(() => () => clearIntentCloseTimer(), [clearIntentCloseTimer]);

  const clearProfileCloseTimer = useCallback(() => {
    if (profileCloseTimerRef.current === null) return;
    window.clearTimeout(profileCloseTimerRef.current);
    profileCloseTimerRef.current = null;
  }, []);

  const openProfileMenu = useCallback(() => {
    clearProfileCloseTimer();
    setContentMenuOpen(false);
    setDirectPastChats(false);
    setDismissed(true);
    setProfileMenuClosing(false);
    setProfileMenuOpen(true);
  }, [clearProfileCloseTimer]);

  const closeProfileMenu = useCallback((afterClose?: () => void) => {
    clearProfileCloseTimer();
    setProfileMenuClosing(true);
    window.requestAnimationFrame(() => setProfileMenuOpen(false));
    const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    profileCloseTimerRef.current = window.setTimeout(() => {
      profileCloseTimerRef.current = null;
      setProfileMenuClosing(false);
      afterClose?.();
    }, reduceMotion ? 0 : ANCHORED_POPOVER_CLOSE_MS);
  }, [clearProfileCloseTimer]);

  useEffect(() => () => clearProfileCloseTimer(), [clearProfileCloseTimer]);

  const clearMoreCloseTimer = useCallback(() => {
    if (moreCloseTimerRef.current === null) return;
    window.clearTimeout(moreCloseTimerRef.current);
    moreCloseTimerRef.current = null;
  }, []);

  const openMoreMenu = useCallback(() => {
    clearMoreCloseTimer();
    setContentMenuOpen(false);
    setDirectPastChats(false);
    setDismissed(true);
    setMoreMenuClosing(false);
    setMoreMenuOpen(true);
  }, [clearMoreCloseTimer]);

  const closeMoreMenu = useCallback((afterClose?: () => void) => {
    clearMoreCloseTimer();
    setMoreMenuClosing(true);
    window.requestAnimationFrame(() => setMoreMenuOpen(false));
    const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    moreCloseTimerRef.current = window.setTimeout(() => {
      moreCloseTimerRef.current = null;
      setMoreMenuClosing(false);
      afterClose?.();
    }, reduceMotion ? 0 : ANCHORED_POPOVER_CLOSE_MS);
  }, [clearMoreCloseTimer]);

  useEffect(() => () => clearMoreCloseTimer(), [clearMoreCloseTimer]);

  const fileDedupKey = async (file: File): Promise<AttachmentDedupKey> => ({
    hash: await sha256(file),
    source: `file:${file.name}:${file.size}:${file.lastModified}`,
  });

  const planModeOn = collaborationMode === "plan";
  const activeGoal = (goal ?? "").trim();
  const goalModeOn = collaborationMode === "goal";
  const warnImageInputFallback = useCallback((message = t("composer.imageInputUnsupported")) => {
    showToast(message, "warn");
  }, [showToast, t]);

  const submit = async () => {
    if (disabled || (!running && submitDisabled) || readOnly) return;
    const submitDraftKey = activeDraftKeyRef.current;
    const submitTabId = tabId;
    if (draftIsSubmitting(submitDraftKey)) return;
    const currentText = textRef.current;
    const trimmedDraft = trimInvocationDraft(currentText, invocationsRef.current);
    const trimmedText = trimmedDraft.text;
    if (draftHasPendingPaste(submitDraftKey)) return;
    if (!imageInputEnabled && hasImageAttachments(attachmentsRef.current)) {
      warnImageInputFallback();
    }
    const currentAttachments = attachmentsRef.current;
    const currentWorkspaceRefs = workspaceRefsRef.current;
    const inlineInvocationCount = trimmedDraft.invocations.filter((invocation) => invocation.command.kind === "skill").length;
    const subagentInvocationCount = trimmedDraft.invocations.filter((invocation) => invocation.command.kind === "subagent").length;
    if (goalModeOn && !activeGoal && trimmedDraft.invocations.length > 0) {
      // The first goal-mode message becomes the goal itself (App wraps it in
      // /goal ...), which would swallow entity invocations as goal prose and
      // never run them. Ask for a plain-text goal first.
      setComposerPrompt(t("composer.goalEntityBlocked"));
      requestAnimationFrame(focusComposerInput);
      return;
    }
    if (!trimmedText && currentAttachments.length === 0 && currentWorkspaceRefs.length === 0 && inlineInvocationCount === 0) {
      if (goalModeOn && !activeGoal) {
        setComposerPrompt(t("composer.goalInputRequired"));
        requestAnimationFrame(focusComposerInput);
      } else if (subagentInvocationCount > 0) {
        setComposerPrompt(t("composer.subagentTaskRequired"));
        requestAnimationFrame(focusComposerInput);
      }
      return;
    }
    setComposerPrompt(null);
    updateSubmittingForDraft(submitDraftKey, true);
    try {
      const orderedAttachments = sortComposerAttachments(currentAttachments);
      const refs = [
        ...currentWorkspaceRefs.map((ref) => formatWorkspaceReference(ref.path, ref.isDir)),
        ...orderedAttachments.map((a) => `@${a.path}`),
      ].join(" ");
      const displayRefs = [
        ...currentWorkspaceRefs.map((ref) => formatWorkspaceReference(ref.displayPath || ref.path, ref.isDir)),
        ...orderedAttachments.map(formatAttachmentDisplayReference),
        ...selectedTextRefsRef.current.map(formatSelectionLabel),
      ].join(" ");
      const displayText = [trimmedText, displayRefs].filter(Boolean).join(trimmedText && displayRefs ? " " : "");
      // PR-B: when past:chats refs are attached, prepend their formatted transcript
      // to submitText only (displayText stays unchanged so the user still sees their
      // original prompt in the input preview). With no refs we keep the original
      // submitText verbatim — no header, no rewording, byte-identical to pre-PR-B.
      const currentSessionRefs = sessionRefsRef.current;
      const currentSelectedTextRefs = selectedTextRefsRef.current;
      const currentPastedBlocks = [...pastedBlocksRef.current];
      const sessionContext = currentSessionRefs.length === 0 ? "" : await buildSessionContext(currentSessionRefs);
      const selectedTextContext = formatSelectedTextContext(currentSelectedTextRefs);
      const invocationText = serializeInvocationSubmit(trimmedText, trimmedDraft.invocations);
      const baseSubmitText = [expandPastedBlocks(invocationText, currentPastedBlocks), refs].filter(Boolean).join(" ");
      const submitBase = sessionContext ? `${sessionContext}${baseSubmitText}` : baseSubmitText;
      const submitText = [submitBase, selectedTextContext].filter(Boolean).join("\n\n");
      const structuredInput = [expandPastedBlocks(trimmedText, currentPastedBlocks), refs].filter(Boolean).join(" ");
      const structured = trimmedDraft.invocations.length > 0 ? {
        display: [invocationText, displayRefs].filter(Boolean).join(invocationText && displayRefs ? " " : ""),
        input: [sessionContext ? `${sessionContext}${structuredInput}` : structuredInput, selectedTextContext].filter(Boolean).join("\n\n"),
        invocations: invocationRequests(trimmedDraft.invocations),
      } satisfies StructuredInvocationSubmit : undefined;
      if (running) {
        // An entity-only submit has an empty displayText (entities live
        // outside the text model); fall back to the serialized slash form so
        // the queue shows the invocation instead of silently dropping it
        // while clearSubmittedDraft wipes the composer.
        const guidanceText = displayText.trim() || (structured?.display.trim() ?? "");
        const guidanceSubmitText = submitText.trim();
        if (guidanceText) {
          const id = nextGuidanceId.current++;
          updatePendingGuidanceForDraft(submitDraftKey, (items) => [
            ...items,
            { id, text: guidanceText, submitText: guidanceSubmitText || guidanceText, structured },
          ]);
        }
        clearSubmittedDraft(submitDraftKey);
        return;
      }
      await onSend(displayText, submitText, submitTabId, structured);
      clearSubmittedDraft(submitDraftKey);
    } catch (error) {
      showToast(error instanceof Error ? error.message : String(error), "warn");
    } finally {
      updateSubmittingForDraft(submitDraftKey, false);
    }
  };

  const sendQueuedGuidance = async (
    item: PendingGuidance,
    targetDraftKey = activeDraftKeyRef.current,
    targetTabId = tabId,
  ) => {
    if (targetDraftKey !== activeDraftKeyRef.current || disabled || readOnly || guidanceSendingIdRef.current !== null) return;
    if (running && item.structured) return;
    const displayText = item.text.trim();
    const submitText = item.submitText.trim() || displayText;
    if (!displayText || !submitText) return;
    const selfDispatched = selfDispatchedGuidanceByDraftRef.current[targetDraftKey] ?? [];
    selfDispatched.push(submitText);
    selfDispatchedGuidanceByDraftRef.current[targetDraftKey] = selfDispatched;
    updateGuidanceSendingIdForDraft(targetDraftKey, item.id);
    try {
      if (running && onSteer) await onSteer(submitText, targetTabId);
      else await onSend(displayText, submitText, targetTabId, item.structured);
      updatePendingGuidanceForDraft(targetDraftKey, (items) => items.filter((queued) => queued.id !== item.id));
      window.setTimeout(() => {
        takeSelfDispatchedGuidance(submitText, targetDraftKey);
      }, 5000);
    } catch (error) {
      takeSelfDispatchedGuidance(submitText, targetDraftKey);
      showToast(error instanceof Error ? error.message : String(error), "warn");
    } finally {
      const current = targetDraftKey === activeDraftKeyRef.current
        ? guidanceSendingIdRef.current
        : draftsBySessionRef.current[targetDraftKey]?.guidanceSendingId;
      if (current === item.id) updateGuidanceSendingIdForDraft(targetDraftKey, null);
    }
  };

  const readFileAsDataURL = (file: File) =>
    new Promise<string>((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result));
      reader.onerror = () => reject(reader.error);
      reader.readAsDataURL(file);
    });

  const attachImageFiles = async (files: File[], sourceDraftKey: string) => {
    const images = files.filter((f) => f.type.startsWith("image/"));
    if (images.length === 0) return;
    for (const file of images) {
      updatePendingPasteForDraft(sourceDraftKey, 1);
      try {
        const key = await fileDedupKey(file);
        if (attachmentSeenInDraft(sourceDraftKey, key)) continue;
        const dataUrl = await readFileAsDataURL(file);
        const path = await app.SavePastedImage(dataUrl);
        const previewUrl = await app.AttachmentDataURL(path);
        addAttachmentToDraft(sourceDraftKey, { path, previewUrl, displayName: file.name }, key);
      } catch (error) {
        console.warn("[composer] failed to attach pasted image", error);
        showToast(t("composer.attachImageFailed"), "warn");
        // non-fatal: a failed image attach must not block normal text input
      } finally {
        updatePendingPasteForDraft(sourceDraftKey, -1);
      }
    }
  };

  // Non-image pastes (PDFs, docs): the clipboard hands us bytes, not a path, so
  // the kernel stores them and we reference the saved path — attached, not ignored.
  const attachOtherFiles = async (files: File[], sourceDraftKey: string) => {
    const others = files.filter((f) => !f.type.startsWith("image/"));
    if (others.length === 0) return;
    for (const file of others) {
      updatePendingPasteForDraft(sourceDraftKey, 1);
      try {
        const key = await fileDedupKey(file);
        if (attachmentSeenInDraft(sourceDraftKey, key)) continue;
        const dataUrl = await readFileAsDataURL(file);
        const path = await app.SavePastedFile(file.name, dataUrl);
        addAttachmentToDraft(sourceDraftKey, { path, displayName: file.name }, key);
      } catch {
        console.warn("[composer] failed to attach pasted file");
        showToast(t("composer.attachFileFailed"), "warn");
        // non-fatal: a failed attach must not block normal text input
      } finally {
        updatePendingPasteForDraft(sourceDraftKey, -1);
      }
    }
  };

  const attachFiles = (files: File[]) => {
    const sourceDraftKey = activeDraftKeyRef.current;
    void attachImageFiles(files, sourceDraftKey);
    void attachOtherFiles(files, sourceDraftKey);
  };

  const attachNativeClipboardImage = async (notifyOnError: boolean, sourceDraftKey: string) => {
    updatePendingPasteForDraft(sourceDraftKey, 1);
    try {
      const path = await app.SaveClipboardImage();
      const previewUrl = await app.AttachmentDataURL(path);
      const key = { hash: await dataURLHash(previewUrl), source: `native-clipboard:${path}` };
      if (attachmentSeenInDraft(sourceDraftKey, key)) return;
      addAttachmentToDraft(sourceDraftKey, { path, previewUrl }, key);
    } catch (error) {
      console.warn("[composer] failed to read native clipboard image", error);
      if (notifyOnError) showToast(t("composer.pasteImageFailed"), "warn");
    } finally {
      updatePendingPasteForDraft(sourceDraftKey, -1);
    }
  };

  // OS file drops arrive as absolute paths through the native bridge (the webview
  // withholds them from the HTML drop event); the kernel resolves each into a
  // workspace @reference or a stored attachment.
  const attachDroppedPaths = async (paths: string[], sourceDraftKey = activeDraftKeyRef.current) => {
    setDragOver(false);
    for (const path of paths) {
      updatePendingPasteForDraft(sourceDraftKey, 1);
      try {
        const key = { hash: "", source: `path:${path}` };
        if (attachmentSeenInDraft(sourceDraftKey, key)) continue;
        const item = await app.AttachDropped(path);
        if (item.kind === "workspace") {
          addWorkspaceReferenceToDraft(sourceDraftKey, { path: item.path, isDir: item.isDir, displayPath: item.displayPath });
        } else {
          addAttachmentToDraft(sourceDraftKey, { path: item.path, previewUrl: item.previewUrl, displayName: baseName(path) }, key);
        }
      } catch {
        console.warn("[composer] failed to attach dropped file");
        showToast(t("composer.attachDropFailed"), "warn");
        // non-fatal: a failed drop attach must not block normal text input
      } finally {
        updatePendingPasteForDraft(sourceDraftKey, -1);
      }
    }
  };

  useEffect(() => {
    return onFilesDropped((paths) => void attachDroppedPaths(paths, activeDraftKeyRef.current));
  }, []);

  const onPaste = (e: ClipboardEvent<HTMLTextAreaElement | HTMLDivElement>) => {
    clearNativeClipboardPasteTimer();
    const files = clipboardFiles(e.clipboardData);
    if (files.length > 0) {
      e.preventDefault();
      attachFiles(files);
      return;
    }

    const pasted = e.clipboardData.getData("text");
    const hasImageHint = clipboardHasImageHint(e.clipboardData);
    if (hasImageHint || pasted === "") {
      e.preventDefault();
      void attachNativeClipboardImage(hasImageHint, activeDraftKeyRef.current);
      return;
    }

    // Always prevent the browser default paste so React's controlled-input
    // reconciliation cannot race with the native DOM update and lose the
    // pasted content (WebView2 / Windows). We insert the text manually below.
    e.preventDefault();
    const selection = getComposerSelection();
    const start = selection.start;
    const end = selection.end;

    // Normalize CRLF from Windows clipboard so caret offsets match the
    // textarea's normalized value. The raw text (with CRLF) is preserved
    // in the PastedBlock for long pastes so block content is lossless.
    const normalizedPasted = pasted.replace(/\r\n/g, "\n");

    if (shouldFoldPaste(pasted)) {
      // Long paste: fold into a collapsible block so the composer stays compact.
      const id = nextPasteId.current++;
      const lines = lineCount(pasted);
      const label = t("composer.pastedLabel", { id, lines });
      const block: PastedBlock = { label, text: pasted }; // keep raw text (CRLF preserved)
      const next = replaceInvocationTextRange(text, invocationsRef.current, start, end, label, selection.afterInvocationId);
      pastedBlocksRef.current = [...pastedBlocksRef.current, block];
      setPastedBlocks((prev) => [...prev, block]);
      textRef.current = next.text;
      invocationsRef.current = next.invocations;
      setText(next.text);
      setInvocations(next.invocations);
      setComposerSelection(start + label.length);
    } else {
      // Short paste: insert the raw text directly into state.
      resetPromptHistoryNavigation();
      const next = replaceInvocationTextRange(text, invocationsRef.current, start, end, normalizedPasted, selection.afterInvocationId);
      textRef.current = next.text;
      invocationsRef.current = next.invocations;
      setText(next.text);
      setInvocations(next.invocations);
      setComposerSelection(start + normalizedPasted.length);
    }
  };

  const getInputSelection = () => {
    const selection = getComposerSelection();
    const start = selection.start;
    const end = selection.end;
    const from = Math.min(start, end);
    const to = Math.max(start, end);
    return {
      from,
      to,
      selected: text.slice(from, to),
    };
  };

  const focusInputRange = (start: number, end = start) => {
    setComposerSelection(start, end);
  };

  const replaceInputRange = (
    value: string,
    start: number,
    end: number,
    targetDraftKey = activeDraftKeyRef.current,
  ) => {
    if (targetDraftKey === activeDraftKeyRef.current) {
      const current = textRef.current;
      const next = replaceInvocationTextRange(current, invocationsRef.current, start, end, value);
      textRef.current = next.text;
      invocationsRef.current = next.invocations;
      setText(next.text);
      setInvocations(next.invocations);
      focusInputRange(start + value.length);
      return;
    }
    const draft = cloneComposerDraft(draftsBySessionRef.current[targetDraftKey] ?? emptyComposerDraft());
    const next = replaceInvocationTextRange(draft.text, draft.invocations, start, end, value);
    draft.text = next.text;
    draft.invocations = next.invocations;
    draftsBySessionRef.current[targetDraftKey] = draft;
  };

  const insertPastedText = (
    pasted: string,
    start: number,
    end: number,
    targetDraftKey = activeDraftKeyRef.current,
  ) => {
    const normalizedPasted = pasted.replace(/\r\n/g, "\n");
    if (targetDraftKey !== activeDraftKeyRef.current) {
      const draft = cloneComposerDraft(draftsBySessionRef.current[targetDraftKey] ?? emptyComposerDraft());
      if (shouldFoldPaste(pasted)) {
        const id = draft.nextPasteId++;
        const lines = lineCount(pasted);
        const label = t("composer.pastedLabel", { id, lines });
        draft.pastedBlocks = [...draft.pastedBlocks, { label, text: pasted }];
        draft.text = draft.text.slice(0, start) + label + draft.text.slice(end);
      } else {
        draft.historyIndex = -1;
        draft.text = draft.text.slice(0, start) + normalizedPasted + draft.text.slice(end);
      }
      draftsBySessionRef.current[targetDraftKey] = draft;
      return;
    }

    if (shouldFoldPaste(pasted)) {
      const id = nextPasteId.current++;
      const lines = lineCount(pasted);
      const label = t("composer.pastedLabel", { id, lines });
      const block: PastedBlock = { label, text: pasted };
      const current = textRef.current;
      const next = current.slice(0, start) + label + current.slice(end);
      pastedBlocksRef.current = [...pastedBlocksRef.current, block];
      setPastedBlocks((prev) => [...prev, block]);
      textRef.current = next;
      setText(next);
      focusInputRange(start + label.length);
    } else {
      resetPromptHistoryNavigation();
      const current = textRef.current;
      const next = current.slice(0, start) + normalizedPasted + current.slice(end);
      textRef.current = next;
      setText(next);
      focusInputRange(start + normalizedPasted.length);
    }
  };

  const copyComposerSelection = async (cut = false) => {
    const selection = getInputSelection();
    const sourceDraftKey = activeDraftKeyRef.current;
    setInputMenuPoint(null);
    if (!selection.selected) {
      focusInputRange(selection.from, selection.to);
      return;
    }
    try {
      await navigator.clipboard.writeText(selection.selected);
    } catch {
      // Fall back to Wails desktop runtime, then execCommand
      try {
        if (typeof window !== "undefined" && (await window.runtime?.ClipboardSetText?.(selection.selected))) {
          /* ok */
        } else if (!fallbackCopyText(selection.selected)) {
          // Every clipboard path failed. Cutting now would delete text that
          // never reached the clipboard, so keep the draft intact.
          if (sourceDraftKey === activeDraftKeyRef.current) focusInputRange(selection.from, selection.to);
          return;
        }
      } catch {
        if (sourceDraftKey === activeDraftKeyRef.current) focusInputRange(selection.from, selection.to);
        return;
      }
    }
    if (cut) {
      if (sourceDraftKey === activeDraftKeyRef.current) resetPromptHistoryNavigation();
      replaceInputRange("", selection.from, selection.to, sourceDraftKey);
    } else if (sourceDraftKey === activeDraftKeyRef.current) {
      focusInputRange(selection.from, selection.to);
    }
  };

  const pasteIntoComposer = async () => {
    const selection = getInputSelection();
    const sourceDraftKey = activeDraftKeyRef.current;
    setInputMenuPoint(null);

    // Try reading clipboard items for image detection (no event in menu path)
    try {
      const items = await navigator.clipboard.read();
      if (items.some((item) => item.types.some((t) => t.startsWith("image/")))) {
        void attachNativeClipboardImage(true, sourceDraftKey);
        return;
      }
    } catch {
      /* clipboard.read() not supported or permission denied; fall through */
    }

    if (!navigator.clipboard?.readText) {
      if (sourceDraftKey === activeDraftKeyRef.current) focusInputRange(selection.from, selection.to);
      return;
    }
    try {
      const pasted = await navigator.clipboard.readText();
      if (pasted === "") {
        // Match the keyboard paste handler: an empty text read means "nothing
        // to insert" (empty clipboard, files, or unsupported types) — never
        // replace the current selection with nothing. An image may still be
        // attachable through the native clipboard path.
        if (sourceDraftKey === activeDraftKeyRef.current) focusInputRange(selection.from, selection.to);
        void attachNativeClipboardImage(false, sourceDraftKey);
        return;
      }
      insertPastedText(pasted, selection.from, selection.to, sourceDraftKey);
    } catch {
      if (sourceDraftKey === activeDraftKeyRef.current) focusInputRange(selection.from, selection.to);
    }
  };

  const selectAllComposerText = () => {
    setInputMenuPoint(null);
    focusInputRange(0, text.length);
  };

  const openInputMenu = (event: ReactMouseEvent<HTMLTextAreaElement>) => {
    event.preventDefault();
    event.stopPropagation();
    rememberCaret();
    setInputMenuPoint(contextMenuPointFromEvent(event));
  };

  const hasWorkspaceReferenceDrag = (dataTransfer: DataTransfer): boolean =>
    Array.from(dataTransfer.types).includes(WORKSPACE_REF_DRAG_TYPE);

  const hasFileDrag = (dataTransfer: DataTransfer): boolean =>
    Array.from(dataTransfer.items).some((it) => it.kind === "file") || dataTransfer.files.length > 0;

  const fileDragItems = (dataTransfer: DataTransfer): DataTransferItem[] =>
    Array.from(dataTransfer.items).filter((item) => item.kind === "file");

  const getWebkitFileEntry = (item: DataTransferItem): WebkitFileEntry | null => {
    const getAsEntry = (item as DataTransferItem & { webkitGetAsEntry?: () => WebkitFileEntry | null }).webkitGetAsEntry;
    return typeof getAsEntry === "function" ? getAsEntry.call(item) : null;
  };

  const hasPathlessFileDrop = (dataTransfer: DataTransfer): boolean => {
    const items = fileDragItems(dataTransfer);
    if (items.length === 0) return dataTransfer.files.length > 0;
    return items.some((item) => getWebkitFileEntry(item) === null);
  };

  const clearWailsDropTarget = () => {
    document.querySelectorAll(".wails-drop-target-active").forEach((el) => el.classList.remove("wails-drop-target-active"));
  };

  const stopNativeFileDrop = (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    e.stopPropagation();
    e.nativeEvent.stopImmediatePropagation();
    clearWailsDropTarget();
  };

  const onFileDropCapture = (e: DragEvent<HTMLDivElement>) => {
    if (hasWorkspaceReferenceDrag(e.dataTransfer) || !hasFileDrag(e.dataTransfer)) return;
    e.preventDefault();
    if (!hasPathlessFileDrop(e.dataTransfer)) return;
    const files = Array.from(e.dataTransfer.files);
    if (files.length === 0) return;
    stopNativeFileDrop(e);
    setDragOver(false);
    attachFiles(files);
  };

  const onDrop = (e: DragEvent<HTMLDivElement>) => {
    const droppedWorkspaceRef = readWorkspaceReferenceDrag(e.dataTransfer);
    if (droppedWorkspaceRef) {
      e.preventDefault();
      setDragOver(false);
      addWorkspaceReference(droppedWorkspaceRef);
      return;
    }

    // OS file drops deliver no usable bytes/paths here; the native bridge
    // (onFilesDropped -> AttachDropped) handles them. Prevent webview navigation.
    if (hasFileDrag(e.dataTransfer)) {
      e.preventDefault();
      setDragOver(false);
    }
  };

  const onDragOver = (e: DragEvent<HTMLDivElement>) => {
    if (!hasWorkspaceReferenceDrag(e.dataTransfer) && !hasFileDrag(e.dataTransfer)) return;
    e.preventDefault(); // required for the drop event to fire
    e.dataTransfer.dropEffect = "copy";
    setDragOver(true);
  };

  const onDragLeave = () => setDragOver(false);

  // handleCancel stops the in-flight turn; if it was cancelled before the server
  // replied, the just-sent text is handed back so we drop it back into the input.
  const handleCancel = () => {
    const restored = onCancel();
    if (goalModeOn && activeGoal) onClearGoal();
    // A user-requested cancel must not let the natural-completion effect submit
    // the queued follow-up. Fold it back into the draft: cancelling means "stop
    // acting", not "discard what I typed" — the same contract onCancel already
    // honors for un-sent text. Structured items fold back as their slash form
    // (structured.display is valid /name syntax) so the invocation survives the
    // round trip instead of degrading to its bare task text.
    const queued = pendingGuidance
      .map((item) => item.structured?.display ?? item.text)
      .filter((part) => part.trim() !== "");
    if (queued.length === 0) {
      if (typeof restored === "string") setTextCaretEnd(restored);
      return;
    }
    updatePendingGuidanceForDraft(activeDraftKeyRef.current, () => []);
    setGuidanceExpanded(false);
    const base = typeof restored === "string" ? restored : text;
    setTextCaretEnd([base, ...queued].filter((part) => part.trim() !== "").join("\n"));
  };

  const pickCommand = (c: CommandInfo) => {
    if (!commandUsesStructuredInvocation(c)) {
      if (invocationsRef.current.length > 0 && richSlashQuery) {
        richInputRef.current?.replaceRange(`/${c.name} `, richSlashQuery.from, richSlashQuery.to);
      } else {
        setTextCaretEnd("/" + c.name + " ");
      }
      return;
    }
    if (invocationsRef.current.length > 0 && richSlashQuery) {
      richInputRef.current?.insertInvocation(c, richSlashQuery);
      setRichSlashQuery(null);
      return;
    }
    const invocation: ComposerInvocation = {
      id: `composer-invocation-${nextInvocationId.current++}`,
      offset: 0,
      command: c,
    };
    textRef.current = "";
    invocationsRef.current = [invocation];
    setText("");
    setInvocations([invocation]);
    setRichSlashQuery(null);
    requestAnimationFrame(() => richInputRef.current?.setSelectionRange(0));
  };

  const activePastedBlocks = pastedBlocks.filter((block) => text.includes(block.label));
  const shellModeActive = text.trimStart().startsWith("!");

  const removeWorkspaceReference = (target: WorkspaceReference) => {
    const key = workspaceReferenceKey(target);
    setWorkspaceRefs((prev) => prev.filter((ref) => workspaceReferenceKey(ref) !== key));
    requestAnimationFrame(focusComposerInput);
  };

  const togglePastedPreview = (label: string) => {
    setOpenPastedLabels((prev) => (prev.includes(label) ? prev.filter((x) => x !== label) : [...prev, label]));
  };

  const replacePastedBlockLabel = (block: PastedBlock, replacement: string) => {
    const current = textRef.current;
    const start = current.indexOf(block.label);
    if (start < 0) return;
    const next = replaceInvocationTextRange(
      current,
      invocationsRef.current,
      start,
      start + block.label.length,
      replacement,
    );
    textRef.current = next.text;
    invocationsRef.current = next.invocations;
    setText(next.text);
    setInvocations(next.invocations);
    setComposerSelection(next.text.length);
  };

  const removePastedBlock = (block: PastedBlock) => {
    pastedBlocksRef.current = pastedBlocksRef.current.filter((x) => x.label !== block.label);
    setPastedBlocks((prev) => prev.filter((x) => x.label !== block.label));
    setOpenPastedLabels((prev) => prev.filter((x) => x !== block.label));
    replacePastedBlockLabel(block, "");
  };

  const expandPastedBlock = (block: PastedBlock) => {
    pastedBlocksRef.current = pastedBlocksRef.current.filter((x) => x.label !== block.label);
    setPastedBlocks((prev) => prev.filter((x) => x.label !== block.label));
    setOpenPastedLabels((prev) => prev.filter((x) => x !== block.label));
    replacePastedBlockLabel(block, block.text);
  };

  useEffect(() => {
    const onResize = () => setComposerHeight((height) => (height === null ? null : clampComposerHeight(height)));
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  const measureTextareaAutoHeight = useCallback(() => {
    if (composerHeight !== null) {
      setTextareaAutoHeight(null);
      setTextareaAutoOverflow(false);
      return;
    }
    const richHeight = invocationsRef.current.length > 0 ? richInputRef.current?.scrollHeight() : 0;
    const node = taRef.current;
    if (!richHeight && !node) return;
    const previousHeight = node?.style.height;
    if (node) node.style.height = "auto";
    const scrollHeight = richHeight || node?.scrollHeight || 0;
    const maxHeight = composerAutoInputMaxHeight();
    const nextHeight = Math.min(scrollHeight, maxHeight);
    const nextOverflow = scrollHeight > maxHeight + 1;
    if (node && previousHeight !== undefined) node.style.height = previousHeight;
    setTextareaAutoHeight((current) => (current === nextHeight ? current : nextHeight));
    setTextareaAutoOverflow((current) => (current === nextOverflow ? current : nextOverflow));
  }, [composerHeight, invocations.length]);

  useLayoutEffect(() => {
    measureTextareaAutoHeight();
  }, [text, measureTextareaAutoHeight]);

  useEffect(() => {
    if (composerHeight !== null) return;
    let frame = 0;
    const update = () => {
      if (frame) window.cancelAnimationFrame(frame);
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        measureTextareaAutoHeight();
      });
    };
    window.addEventListener("resize", update);
    const observer = new MutationObserver(update);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-text-size", "data-font-family", "data-mono-font-family", "style"],
    });
    return () => {
      if (frame) window.cancelAnimationFrame(frame);
      window.removeEventListener("resize", update);
      observer.disconnect();
    };
  }, [composerHeight, measureTextareaAutoHeight]);

  const saveComposerHeight = (height: number) => {
    saveLayoutSize("composerHeight", height, clampComposerHeight);
  };

  const resetComposerHeight = () => {
    setComposerHeight(null);
    clearLayoutSize("composerHeight");
  };

  const onComposerResizeStart = (e: ReactPointerEvent<HTMLButtonElement>) => {
    if (e.button !== 0) return;
    const card = composerCardRef.current;
    if (!card) return;

    e.preventDefault();
    const startY = e.clientY;
    const startHeight = composerHeight ?? composerLogicalHeight(card);
    let nextHeight = clampComposerHeight(startHeight);
    let moved = false;
    card.style.setProperty("--composer-height", `${nextHeight}px`);
    e.currentTarget.setAttribute("aria-valuenow", String(nextHeight));
    const liveResize = createRafResizeUpdater({
      target: card,
      separator: e.currentTarget,
      cssVar: "--composer-height",
    });
    setComposerResizing(true);
    document.body.classList.add("composer-resizing");

    const onMove = (event: PointerEvent) => {
      moved = true;
      nextHeight = clampComposerHeight(startHeight + startY - event.clientY);
      liveResize.schedule(nextHeight);
    };
    const onUp = () => {
      liveResize.flush();
      setComposerResizing(false);
      document.body.classList.remove("composer-resizing");
      if (moved) {
        setComposerHeight(nextHeight);
        saveComposerHeight(nextHeight);
      }
      document.removeEventListener("pointermove", onMove);
      document.removeEventListener("pointerup", onUp);
      document.removeEventListener("pointercancel", onUp);
    };

    document.addEventListener("pointermove", onMove);
    document.addEventListener("pointerup", onUp);
    document.addEventListener("pointercancel", onUp);
  };

  const onComposerResizeKeyDown = (e: KeyboardEvent<HTMLButtonElement>) => {
    const card = composerCardRef.current;
    const current = composerHeight ?? (card ? composerLogicalHeight(card) : COMPOSER_MIN_HEIGHT);
    const step = e.shiftKey ? 32 : 16;
    let next: number | null = null;
    if (e.key === "ArrowUp" || e.key === "PageUp") next = current + step;
    else if (e.key === "ArrowDown" || e.key === "PageDown") next = current - step;
    else if (e.key === "Home") next = COMPOSER_MIN_HEIGHT;
    else if (e.key === "End") next = composerMaxHeight();
    if (next === null) return;
    e.preventDefault();
    const height = clampComposerHeight(next);
    setComposerHeight(height);
    saveComposerHeight(height);
  };

  const pickEntry = (e: DirEntry) => {
    const picked = composerPickFileEntry(text, atRaw, atDir, e);
    if (picked.workspaceRef) {
      setTextCaretEnd(picked.text);
      addWorkspaceReference(picked.workspaceRef);
      return;
    }
    // A directory keeps the menu open (trailing "/"); a file completes it (space).
    setTextCaretEnd(picked.text);
  };

  // --- past:chats session reference ---
  const openPastChats = useCallback(async (initialQuery = "") => {
    const snapshotCwd = cwdRef.current;
    const sourceDraftKey = activeDraftKeyRef.current;
    setShowPastChats(true);
    setActive(0);
    setPastChatQuery(initialQuery);
    setLoadingPastChats(true);
    try {
      const sessions = await app.ListSessions();
      // Discard stale response if workspace changed while the request was in-flight.
      if (cwdRef.current !== snapshotCwd || activeDraftKeyRef.current !== sourceDraftKey) return;
      const sorted = asArray(sessions)
        .filter((s) => !s.current)
        .sort((a, b) => {
          const at = a.lastActivityAt || a.modTime || a.createdAt || 0;
          const bt = b.lastActivityAt || b.modTime || b.createdAt || 0;
          return bt - at;
        })
        .slice(0, 50);
      setPastChats(sorted);
    } catch {
      if (cwdRef.current !== snapshotCwd || activeDraftKeyRef.current !== sourceDraftKey) return;
      setPastChats([]);
    } finally {
      if (cwdRef.current === snapshotCwd && activeDraftKeyRef.current === sourceDraftKey) setLoadingPastChats(false);
    }
  }, []);

  useEffect(() => {
    if (!pastChatToken || directPastChats || dismissed || running || disabled || readOnly) return;
    setDirectPastChats(true);
    void openPastChats(pastChatToken.query);
  }, [directPastChats, disabled, dismissed, openPastChats, pastChatToken, readOnly, running]);

  const clearDirectPastChatToken = () => {
    const current = textRef.current;
    const token = activePastChatToken(current);
    if (!token) return current.length;
    const next = replaceInvocationTextRange(current, invocationsRef.current, token.from, current.length, "");
    textRef.current = next.text;
    invocationsRef.current = next.invocations;
    setText(next.text);
    setInvocations(next.invocations);
    return token.from;
  };

  const dismissDirectPastChats = () => {
    // Keep the literal token text — "#6310" may be an issue number or a
    // heading, not a session query. Dismissing only closes the panel;
    // `dismissed` suppresses reopening until the query changes, the same
    // contract as the slash and @ menus. Selecting a session (pickSession)
    // is the only path that consumes the token.
    setDismissed(true);
    setDirectPastChats(false);
    setShowPastChats(false);
    setPastChatQuery("");
    setActive(0);
    requestAnimationFrame(focusComposerInput);
  };

  // The typed panel follows the live token: typing in the composer extends
  // the query, and deleting the token (or ending it with whitespace) closes
  // the panel instead of leaving it open on a stale query.
  useEffect(() => {
    if (!directPastChats) return;
    if (pastChatTokenQuery === null) {
      setDirectPastChats(false);
      setShowPastChats(false);
      setPastChatQuery("");
      setActive(0);
      return;
    }
    setPastChatQuery(pastChatTokenQuery);
  }, [directPastChats, pastChatTokenQuery]);

  const insertContentTrigger = (trigger: "@" | "#" | "/") => {
    const selection = getInputSelection();
    const current = textRef.current;
    const needsSpace = selection.from > 0 && !/\s/.test(current.charAt(selection.from - 1));
    const value = `${needsSpace ? " " : ""}${trigger}`;
    setContentMenuOpen(false);
    setDirectPastChats(false);
    setShowPastChats(false);
    setDismissed(false);
    replaceInputRange(value, selection.from, selection.to);
    if (trigger === "#") {
      setDirectPastChats(true);
      void openPastChats();
    }
  };

  const openContentMenu = () => {
    if (intentMenuOpen || intentMenuClosing) closeIntentMenu();
    if (profileMenuOpen || profileMenuClosing) closeProfileMenu();
    if (moreMenuOpen || moreMenuClosing) closeMoreMenu();
    setDirectPastChats(false);
    setShowPastChats(false);
    setDismissed(true);
    setContentMenuOpen(true);
  };

  const chooseAttachmentFiles = () => {
    setContentMenuOpen(false);
    fileInputRef.current?.click();
  };

  // PR-C1: client-side filter for the past:chats list. Matches against the
  // human-visible fields (title, topic, preview, path, workspace) so users
  // can narrow long session lists without a backend round-trip. Lowercased
  // substring match keeps the behaviour predictable across locales.
  const filteredPastChats = useMemo(() => {
    const q = pastChatQuery.trim().toLowerCase();
    if (!q) return pastChats;
    return pastChats.filter((session) =>
      [
        session.title,
        session.topicTitle,
        session.preview,
        session.path,
        session.workspaceRoot,
      ]
        .map((value) => String(value ?? "").toLowerCase())
        .some((value) => value.includes(q)),
    );
  }, [pastChats, pastChatQuery]);

  // Final menu item count: when the past:chats list is open, count the
  // filtered sessions instead of file entries + the "past:chats" row.
  const count = (menuMode === "at" && showPastChats) || menuMode === "pastChats"
    ? filteredPastChats.length
    : countBase;

  // Clamp active index when the menu item count changes (e.g. switching
  // between file list and past:chats list, or filtering sessions).
  useEffect(() => {
    const maxIdx = Math.max(0, count - 1);
    setActive((prev) => (prev > maxIdx ? 0 : prev));
  }, [count]);


  const removeAtToken = (value: string) => {
    return value.replace(/[\r\n]+$/u, "").replace(activeRefTokenRe, "").trimEnd();
  };

  const pickSession = (session: SessionMeta) => {
    setSessionRefs((prev) => {
      if (prev.some((x) => x.path === session.path)) {
        return prev;
      }
      return [
        ...prev,
        {
          path: session.path,
          title: session.title || session.topicTitle || session.preview || "Untitled",
          preview: session.preview,
          turns: session.turns,
          createdAt: session.createdAt,
          lastActivityAt: session.lastActivityAt,
        },
      ];
    });
    const caret = directPastChats ? clearDirectPastChatToken() : null;
    if (!directPastChats) setText((prev) => removeAtToken(prev));
    setDirectPastChats(false);
    setPastChatQuery("");
    setShowPastChats(false);
    setActive(0);
    setComposerSelection(caret ?? textRef.current.length);
  };

  const removeSessionRef = (path: string) => {
    setSessionRefs((prev) => prev.filter((ref) => ref.path !== path));
  };

  // pickArg replaces just the current token with the suggestion. A "descend" item
  // (e.g. "/skill show ") ends with a space, so the effect re-fetches the next
  // level; a terminal item leaves the menu (next fetch returns nothing).
  const pickArg = (it: SlashArgItem) => {
    if (!argRes) return;
    setTextCaretEnd(slashText.slice(0, argRes.from) + it.insert);
  };

  const pickActive = () => {
    if (menuMode === "slash") {
      const item = slashMatches[active];
      if (item) pickCommand(item);
      return;
    }
    if (menuMode === "slasharg" && argRes) {
      const item = argRes.items[active];
      if (item) pickArg(item);
      return;
    }
    if (menuMode === "at" || menuMode === "pastChats") {
      if (showPastChats) {
        const session = filteredPastChats[active];
        if (session) pickSession(session);
        return;
      }
      if (menuMode === "pastChats") return;
      const item = atMenuItems[active];
      if (!item) return;
      if (item.kind === "pastChats") {
        void openPastChats();
        return;
      }
      pickEntry(item.entry);
    }
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement | HTMLDivElement>) => {
    const composing = isImeKeyEvent(e, composingRef.current, lastCompositionEndAt.current);
    const native = e.nativeEvent as globalThis.KeyboardEvent & {
      keyCode?: number;
      which?: number;
      code?: string;
    };
    const fnKey = isFnKeyEvent(native);
    const historyDirection = promptHistoryDirectionFromEvent({
      key: e.key,
      code: native.code,
      keyCode: native.keyCode,
      which: native.which,
    });

    if (e.key === "Enter" && composing) return;
    if (fnKey) return;

    if (isPasteShortcut(e) && !composing) {
      clearNativeClipboardPasteTimer();
      const sourceDraftKey = activeDraftKeyRef.current;
      nativeClipboardPasteTimerRef.current = window.setTimeout(() => {
        nativeClipboardPasteTimerRef.current = null;
        void attachNativeClipboardImage(false, sourceDraftKey);
      }, 160);
    }

    // Shift+Tab toggles plan mode only. Tool access is deliberately changed via
    // the access menu so keyboard cycling never crosses a permission boundary.
    if (e.key === "Tab" && e.shiftKey && !composing) {
      e.preventDefault();
      onCycleMode();
      return;
    }

    if (matchesShortcut(e.nativeEvent, "toolApproval.yolo", shortcutPlatform) && !composing) {
      e.preventDefault();
      onToggleYoloApprovalMode();
      return;
    }

    syncPromptHistoryGeneration();

    const inputSelection = getComposerSelection();
    const inputValue = textRef.current;

    const canUseCurrentPromptHistory = () => canUsePromptHistory({
      direction: historyDirection,
      menuOpen: Boolean(menuMode),
      composing,
      altKey: e.altKey,
      ctrlKey: e.ctrlKey,
      metaKey: e.metaKey,
      shiftKey: e.shiftKey,
      fnKey,
      value: inputValue,
      selectionStart: inputSelection.start,
      selectionEnd: inputSelection.end,
      historyIndex: historyIndexRef.current,
    }) && invocationsRef.current.length === 0;

    // Prompt history navigation: plain ↑/↓ only. Fn/Page/Home/End are left to
    // the native textarea/OS so macOS dictation and text navigation keep working.

    // When navigating history, any other key (letter, Backspace, etc.) resets
    // back to the saved draft when another key is used.
    if (historyIndexRef.current !== -1 && !canUseCurrentPromptHistory()) {
      historyIndexRef.current = -1;
      setHistoryIndex(-1);
    }

    if (canUseCurrentPromptHistory()) {
      e.preventDefault();
      const sourceDraftKey = activeDraftKeyRef.current;
      void (async () => {
        // Keep the navigation result with the draft where the key was pressed;
        // loading older history may outlive a tab switch.
        if (historyIndexRef.current === -1) {
          savedTextRef.current = text; // save current draft
        }
        const sourceIndex = historyIndexRef.current;
        const target =
          historyDirection === "up"
            ? sourceIndex + 1
            : historyDirection === "down"
              ? sourceIndex - 1
              : sourceIndex;
        if (target >= historyEntriesRef.current.length && !(await ensurePromptHistoryIndex(target))) {
          return;
        }
        const next =
          historyDirection === "up"
            ? Math.min(target, historyEntriesRef.current.length - 1)
            : historyDirection === "down"
              ? Math.max(target, -1)
              : sourceIndex;
        const historyText = next === -1 ? null : historyEntriesRef.current[next]?.text ?? "";
        if (sourceDraftKey === activeDraftKeyRef.current) {
          historyIndexRef.current = next;
          setHistoryIndex(next);
          setTextCaretEnd(historyText ?? savedTextRef.current);
        } else {
          const draft = cloneComposerDraft(draftsBySessionRef.current[sourceDraftKey] ?? emptyComposerDraft());
          draft.historyIndex = next;
          draft.text = historyText ?? draft.savedText;
          draftsBySessionRef.current[sourceDraftKey] = draft;
        }
        if (historyDirection === "up" && historyEntriesRef.current.length - 1 - next <= PROMPT_HISTORY_PREFETCH_REMAINING) {
          prefetchPromptHistoryTail();
        }
      })();
      return;
    }

    if (menuMode && !composing) {
      if (e.key === "ArrowDown" && count > 0) {
        e.preventDefault();
        setActive((i) => (i + 1) % count);
        return;
      }
      if (e.key === "ArrowUp" && count > 0) {
        e.preventDefault();
        setActive((i) => (i - 1 + count) % count);
        return;
      }
      if (e.key === "Enter" || e.key === "Tab") {
        e.preventDefault();
        pickActive();
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        if (menuMode === "pastChats") {
          dismissDirectPastChats();
        } else if (showPastChats) {
          setPastChatQuery("");
          setShowPastChats(false);
          setActive(0);
        } else {
          setDismissed(true);
        }
        return;
      }
    }

    // The send chord (default Enter) sends and the newline chord (default
    // Shift+Enter) breaks the line — both configurable in Settings →
    // Shortcuts. The default send layout retains legacy modified-Enter send
    // aliases; explicit custom bindings are exact. `composing` guards IME confirms.
    if (e.key === "Enter" && !composing) {
      const enterAction = composerEnterAction(e.nativeEvent, shortcutPlatform);
      if (enterAction === "newline-insert") {
        e.preventDefault();
        insertNewlineAtCaret();
        return;
      }
      if (enterAction === "send") {
        e.preventDefault();
        submit();
        return;
      }
      if (enterAction !== "newline-native") {
        e.preventDefault();
        return;
      }
      // "newline-native" falls through so the input inserts the break itself.
    }
    // Esc interrupts the in-flight turn (matches the Stop button's hint), and
    // restores the text if the server hadn't replied yet.
    if (e.key === "Escape" && running) {
      e.preventDefault();
      handleCancel();
    }
  };

  // Keydown handler for the past:chats search <input>. The search input is a
  // sibling of the <textarea>, so keyboard events never reach the textarea's
  // onKeyDown. We intercept navigation keys here and delegate to the same
  // menu logic. Regular typing keys (letters, Backspace, etc.) pass through
  // so the user can type a search query.
  const onPastChatSearchKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp" || e.key === "Enter" || e.key === "Tab" || e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      if (e.key === "ArrowDown" && count > 0) {
        setActive((i) => (i + 1) % count);
      } else if (e.key === "ArrowUp" && count > 0) {
        setActive((i) => (i - 1 + count) % count);
      } else if (e.key === "Enter" || e.key === "Tab") {
        pickActive();
      } else if (e.key === "Escape") {
        if (menuMode === "pastChats") dismissDirectPastChats();
        else {
          setPastChatQuery("");
          setShowPastChats(false);
          setActive(0);
        }
      }
    }
  };

  // When the run strip is visible inside a user-resized card, the card grows
  // by the strip's reserved height so the meta row stays fully visible.
  // --composer-height carries only the user's logical height; the reservation
  // is a separate variable consumed by the CSS calc, so the live resize drag
  // (which writes raw logical heights) stays consistent with this render path.
  const showRunStrip = Boolean(retry || running);
  const composerCardStyle = composerHeight === null
    ? undefined
    : ({
        "--composer-height": `${composerHeight}px`,
        "--composer-run-strip-reserved": `${showRunStrip ? COMPOSER_RUN_STRIP_RESERVED : 0}px`,
      } as CSSProperties);
  const textareaStyle = composerHeight === null && textareaAutoHeight !== null
    ? ({ height: `${textareaAutoHeight}px`, overflowY: textareaAutoOverflow ? "auto" : "hidden" } as CSSProperties)
    : undefined;
  const composerAutoExpanded = composerHeight === null && textareaAutoHeight !== null && textareaAutoHeight > 40;
  const composerResizeValue = composerHeight ?? clampComposerHeight((textareaAutoHeight ?? 0) + COMPOSER_AUTO_RESERVED_HEIGHT);
  void onSetMode;
  const chooseApprovalMode = (nextMode: ToolApprovalMode) => {
    onSetToolApprovalMode(nextMode);
    requestAnimationFrame(focusComposerInput);
  };
  const chooseTaskMode = (nextMode: CollaborationMode) => {
    closeIntentMenu(() => {
      if (nextMode !== collaborationMode) onSetCollaborationMode(nextMode);
      requestAnimationFrame(focusComposerInput);
    });
  };
  const stopGoalMode = () => {
    closeIntentMenu(() => {
      onClearGoal();
      requestAnimationFrame(focusComposerInput);
    });
  };
  const chooseTokenMode = (mode: TokenMode) => {
    closeProfileMenu(() => {
      if (mode !== tokenMode) onSetTokenMode(mode);
      requestAnimationFrame(focusComposerInput);
    });
  };
  const runtimeProfileShortKey = tokenMode === "economy"
    ? "composer.runtimeProfileEconomyShort"
    : tokenMode === "delivery"
      ? "composer.runtimeProfileDeliveryShort"
      : "composer.runtimeProfileBalancedShort";
  const runtimeProfileTooltipSummaryKey = tokenMode === "economy"
    ? "composer.runtimeProfileEconomyTooltipSummary"
    : tokenMode === "delivery"
      ? "composer.runtimeProfileDeliveryTooltipSummary"
      : "composer.runtimeProfileBalancedTooltipSummary";
  const RuntimeProfileIcon = tokenMode === "economy" ? Gauge : tokenMode === "delivery" ? Flag : Equal;
  const runtimeProfileTriggerLabel = t("composer.runtimeProfileTrigger", { mode: t(runtimeProfileShortKey) });
  const runtimeProfileTooltipLabel = t("composer.controlTooltip", {
    category: t("composer.runtimeProfileTitle"),
    mode: t(runtimeProfileShortKey),
    summary: t(runtimeProfileTooltipSummaryKey),
  });
  const taskModeShortKey = collaborationMode === "plan"
    ? "composer.taskModePlanShort"
    : collaborationMode === "goal"
      ? "composer.taskModeGoalShort"
      : "composer.taskModeDirectShort";
  const taskModeTooltipSummaryKey = collaborationMode === "plan"
    ? "composer.taskModePlanTooltipSummary"
    : collaborationMode === "goal"
      ? "composer.taskModeGoalTooltipSummary"
      : "composer.taskModeDirectTooltipSummary";
  const TaskModeIcon = collaborationMode === "plan" ? List : collaborationMode === "goal" ? Target : ArrowRight;
  const taskModeTriggerLabel = t("composer.taskModeTrigger", { mode: t(taskModeShortKey) });
  const taskModeTooltipLabel = t("composer.controlTooltip", {
    category: t("composer.intentMenuTitle"),
    mode: t(taskModeShortKey),
    summary: t(taskModeTooltipSummaryKey),
  });
  const effortLevels = asArray(effort?.levels);
  const currentEffort = effort?.current || "auto";
  const compactEffortTitle = currentEffort === "auto"
    ? t("status.effortAutoTitle", { def: effort?.default || "auto" })
    : `${t("status.effortTitle")}: ${currentEffort}`;
  const hasEffort = Boolean(effort?.supported && effortLevels.length > 0);
  const chooseEffortLevel = (level: string) => {
    closeMoreMenu(() => {
      if (level !== currentEffort) onSetEffort(level);
      requestAnimationFrame(focusComposerInput);
    });
  };
  // Run-strip state machine: retry > waiting-approval > waiting-ask > streaming.
  // Decision surfaces own the "waiting on user" UI; while suspendedByDecision
  // is true we still pause the work clock but do not render a waiting strip.
  const waitingPrompt = suspendedByDecision
    ? null
    : pendingApprovalLabel
      ? "approval"
      : pendingAsk
        ? "ask"
        : null;
  const pauseWorkClock = suspendedByDecision || Boolean(waitingPrompt);
  // Decision surfaces hide the whole composer, so mode controls stay disabled.
  // Legacy tests that pass pendingApprovalLabel without suspendedByDecision
  // still keep the approval bar usable mid-prompt.
  const approvalBarDisabled = Boolean(disabled) && !(pendingApprovalLabel && !suspendedByDecision);
  // Waiting on the user is not model work. Approval/ask wait is owned by the
  // per-tab controller (turnWaitAccumMs + promptWaitStartedAt) so background
  // tabs keep accumulating. Composer only tracks local pauses for surfaces the
  // controller does not know about (clear-context, legacy strip tests).
  const controllerTracksWait = typeof promptWaitStartedAt === "number" && promptWaitStartedAt > 0;
  const controllerWaitMs = Math.max(0, turnWaitAccumMs || 0)
    + (controllerTracksWait ? Math.max(0, now - promptWaitStartedAt) : 0);
  const trackLocalPause = pauseWorkClock && !controllerTracksWait;
  const [localWaitAccumMs, setLocalWaitAccumMs] = useState(0);
  const localPauseSinceRef = useRef<number | null>(null);
  useEffect(() => {
    localPauseSinceRef.current = null;
    setLocalWaitAccumMs(0);
    if (trackLocalPause) localPauseSinceRef.current = Date.now();
    // trackLocalPause is read from the render that changed draft/turn.
    // eslint-disable-next-line react-hooks/exhaustive-deps -- intentional scope-only reset
  }, [draftKey, turnStartAt]);
  useEffect(() => {
    if (trackLocalPause) {
      if (localPauseSinceRef.current == null) localPauseSinceRef.current = Date.now();
      return;
    }
    if (localPauseSinceRef.current == null) return;
    const delta = Date.now() - localPauseSinceRef.current;
    localPauseSinceRef.current = null;
    if (delta > 0) setLocalWaitAccumMs((total) => total + delta);
  }, [trackLocalPause]);
  const localOpenWaitMs = localPauseSinceRef.current != null
    ? Math.max(0, now - localPauseSinceRef.current)
    : 0;
  const waitAccumMs = controllerWaitMs + localWaitAccumMs + localOpenWaitMs;
  // Close menus/popovers while a decision surface owns the footer.
  useEffect(() => {
    if (!suspendedByDecision) return;
    setDismissed(true);
    setContentMenuOpen(false);
    setDirectPastChats(false);
    setShowPastChats(false);
    closeIntentMenu();
    closeProfileMenu();
    closeMoreMenu();
  }, [suspendedByDecision, closeIntentMenu, closeProfileMenu, closeMoreMenu]);
  const runStateText = retry
    ? t("status.retrying", { attempt: retry.attempt, max: retry.max })
    : waitingPrompt === "approval"
      ? t("composer.runWaitingApproval", { tool: pendingApprovalLabel ?? "" })
      : waitingPrompt === "ask"
        ? t("composer.runWaitingAsk")
        : running && !suspendedByDecision
          ? t("composer.runAnnounceRunning")
          : null;
  const runTicker = !retry && !pauseWorkClock && running && turnStartAt
    ? (() => {
        const elapsedMs = Math.max(0, now - turnStartAt - waitAccumMs);
        const words = SPINNER_WORDS[locale];
        const word = words[Math.floor(elapsedMs / 3000) % words.length];
        const liveTokens = (turnTokens ?? 0) + Math.round((turnArgChars ?? 0) / 4);
        const tok = liveTokens > 0 ? ` · ↓ ${fmtTokens(liveTokens)} ${t("status.tokens")}` : "";
        return `${word}… ${fmtElapsed(elapsedMs)}${tok}`;
      })()
    : null;
  const submitEmpty = !text.trim() && attachments.length === 0 && workspaceRefs.length === 0 &&
    !invocations.some((invocation) => invocation.command.kind === "skill");
  const submitBlocked = submitting || pendingPaste > 0 || (submitEmpty && !(goalModeOn && !activeGoal)) || disabled || (!running && submitDisabled) || readOnly;
  const submitTooltip = running
    ? t("composer.queueGuidance", { combo: sendComboLabel })
    : t("composer.send", { combo: sendComboLabel });
  const composerPlaceholder = readOnly
    ? t("composer.readOnlyChannel")
    : disabled
      ? t("common.loading")
      : running
        ? t("composer.steerPlaceholder", { combo: sendComboLabel })
        : goalModeOn && !activeGoal
          ? t("composer.goalInputPlaceholder")
          : t("composer.placeholder");
  const hiddenGuidanceCount = Math.max(0, pendingGuidance.length - 2);
  const visibleGuidance = guidanceExpanded ? pendingGuidance : pendingGuidance.slice(0, 2);
  const showGuidanceExpander = pendingGuidance.length > 2;
  const composerMetaClass = [
    "composer-meta",
    hasEffort ? "composer-meta--has-effort" : "composer-meta--no-effort",
  ].join(" ");

  const inputSelection = getInputSelection();
  const hasInputSelection = inputSelection.from !== inputSelection.to;
  // Platform-correct hint: ⌘ on macOS, Ctrl elsewhere — same formatter the
  // shortcut settings UI uses.
  const editMenuShortcut = (key: string) =>
    formatShortcutCombo(
      shortcutPlatform === "darwin" ? { key, meta: true } : { key, ctrl: true },
      shortcutPlatform,
    );
  const inputMenuItems: ContextMenuItem[] = [
    {
      key: "cut",
      label: t("common.cut"),
      shortcut: editMenuShortcut("x"),
      disabled: disabled || !hasInputSelection,
      onSelect: () => void copyComposerSelection(true),
    },
    {
      key: "copy",
      label: t("common.copy"),
      shortcut: editMenuShortcut("c"),
      disabled: !hasInputSelection,
      onSelect: () => void copyComposerSelection(),
    },
    {
      key: "paste",
      label: t("common.paste"),
      shortcut: editMenuShortcut("v"),
      disabled,
      onSelect: () => void pasteIntoComposer(),
    },
    {
      key: "select-all",
      label: t("common.selectAll"),
      shortcut: editMenuShortcut("a"),
      disabled: text.length === 0,
      onSelect: selectAllComposerText,
    },
  ];

  return (
    <div
      className={`composer-wrap${decisionPending ? " composer-wrap--decision-pending" : ""}`}
      style={{ "--wails-drop-target": "drop" } as CSSProperties}
      onDropCapture={onFileDropCapture}
    >
      <input
        ref={fileInputRef}
        className="composer-content-file-input"
        type="file"
        multiple
        tabIndex={-1}
        aria-hidden="true"
        onChange={(event) => {
          const files = Array.from(event.currentTarget.files ?? []);
          event.currentTarget.value = "";
          if (files.length > 0) attachFiles(files);
          requestAnimationFrame(() => taRef.current?.focus());
        }}
      />
      <AnchoredPopover
        open={contentMenuOpen && !disabled && !readOnly && !running}
        anchorRef={contentMenuAnchorRef}
        onClose={() => setContentMenuOpen(false)}
        className="composer-access-menu composer-content-menu"
        align="start"
      >
        <div className="composer-access-menu__section" role="menu" aria-label={t("composer.contentMenuTitle")}>
          <button type="button" role="menuitem" className="composer-access-menu__item composer-content-menu__item" onClick={chooseAttachmentFiles}>
            <FilePlus2 size={16} aria-hidden="true" />
            <span className="composer-access-menu__copy">
              <span className="composer-access-menu__title">{t("composer.contentAddAttachment")}</span>
              <span className="composer-access-menu__desc">{t("composer.contentAddAttachmentDesc")}</span>
            </span>
          </button>
          <button type="button" role="menuitem" className="composer-access-menu__item composer-content-menu__item" onClick={() => insertContentTrigger("@")}>
            <AtSign size={16} aria-hidden="true" />
            <span className="composer-access-menu__copy">
              <span className="composer-access-menu__title">{t("composer.contentReferenceFiles")}</span>
              <span className="composer-access-menu__desc">{t("composer.contentReferenceFilesDesc")}</span>
            </span>
          </button>
          <button type="button" role="menuitem" className="composer-access-menu__item composer-content-menu__item" onClick={() => insertContentTrigger("#")}>
            <Hash size={16} aria-hidden="true" />
            <span className="composer-access-menu__copy">
              <span className="composer-access-menu__title">{t("composer.contentReferenceSessions")}</span>
              <span className="composer-access-menu__desc">{t("composer.contentReferenceSessionsDesc")}</span>
            </span>
          </button>
          <button
            type="button"
            role="menuitem"
            className="composer-access-menu__item composer-content-menu__item"
            onClick={() => insertContentTrigger("/")}
            disabled={text.trim().length > 0}
            title={text.trim().length > 0 ? t("composer.contentUseCommandsEmptyOnly") : undefined}
          >
            <span className="composer-content-menu__trigger-icon" aria-hidden="true">/</span>
            <span className="composer-access-menu__copy">
              <span className="composer-access-menu__title">{t("composer.contentUseCommands")}</span>
              <span className="composer-access-menu__desc">{text.trim().length > 0 ? t("composer.contentUseCommandsEmptyOnly") : t("composer.contentUseCommandsDesc")}</span>
            </span>
          </button>
        </div>
      </AnchoredPopover>
      <AnchoredPopover
        open={intentMenuOpen}
        closing={intentMenuClosing}
        anchorRef={intentMenuAnchorRef}
        onClose={() => closeIntentMenu()}
        className="composer-access-menu composer-intent-menu"
        align="start"
      >
        <div className="composer-access-menu__section" role="menu" aria-label={t("composer.intentMenuTitle")}>
          <div className="composer-access-menu__label">{t("composer.intentMenuTitle")}</div>
          <button
            type="button"
            role="menuitemradio"
            aria-checked={collaborationMode === "normal"}
            className={`composer-access-menu__item composer-intent-menu__item${collaborationMode === "normal" ? " composer-access-menu__item--active" : ""}`}
            onClick={() => chooseTaskMode("normal")}
            disabled={disabled || running}
          >
            <ArrowRight size={16} />
            <span className="composer-access-menu__copy">
              <span className="composer-access-menu__title">{t("composer.taskModeDirect")}</span>
              <span className="composer-access-menu__desc">{t("composer.taskModeDirectDesc")}</span>
            </span>
            {collaborationMode === "normal" && <Check className="composer-intent-menu__check" size={16} aria-hidden="true" />}
          </button>
          <button
            type="button"
            role="menuitemradio"
            aria-checked={planModeOn}
            className={`composer-access-menu__item composer-intent-menu__item${planModeOn ? " composer-access-menu__item--active" : ""}`}
            onClick={() => chooseTaskMode("plan")}
            disabled={disabled || running}
          >
            <List size={16} />
            <span className="composer-access-menu__copy">
              <span className="composer-access-menu__title">{t("composer.taskModePlan")}</span>
              <span className="composer-access-menu__desc">{t("composer.taskModePlanDesc")}</span>
            </span>
            {planModeOn && <Check className="composer-intent-menu__check" size={16} aria-hidden="true" />}
          </button>
          <button
            type="button"
            role="menuitemradio"
            aria-checked={goalModeOn}
            className={`composer-access-menu__item composer-intent-menu__item${goalModeOn ? " composer-access-menu__item--active" : ""}`}
            onClick={() => chooseTaskMode("goal")}
            disabled={disabled || running}
            title={activeGoal || undefined}
          >
            <Target size={16} />
            <span className="composer-access-menu__copy">
              <span className="composer-access-menu__title">{t("composer.taskModeGoal")}</span>
              <span className="composer-access-menu__desc">{activeGoal || t("composer.taskModeGoalDesc")}</span>
            </span>
            {goalModeOn && <Check className="composer-intent-menu__check" size={16} aria-hidden="true" />}
          </button>
          {goalModeOn && activeGoal && (
            <button
              type="button"
              className="composer-intent-menu__stop"
              onClick={stopGoalMode}
              disabled={disabled || running}
            >
              {t("composer.taskModeStopGoal")}
            </button>
          )}
        </div>
      </AnchoredPopover>
      <AnchoredPopover
        open={profileMenuOpen}
        closing={profileMenuClosing}
        anchorRef={profileMenuAnchorRef}
        onClose={() => closeProfileMenu()}
        className="composer-access-menu composer-profile-menu"
        align="start"
      >
        <div className="composer-access-menu__section" role="menu" aria-label={t("composer.runtimeProfileTitle")}>
          <div className="composer-access-menu__label">{t("composer.runtimeProfileTitle")}</div>
          {([
            ["economy", Gauge, "composer.runtimeProfileEconomy", "composer.runtimeProfileEconomyDesc"],
            ["full", Equal, "composer.runtimeProfileBalanced", "composer.runtimeProfileBalancedDesc"],
            ["delivery", Flag, "composer.runtimeProfileDelivery", "composer.runtimeProfileDeliveryDesc"],
          ] as const).map(([profile, Icon, titleKey, descKey]) => (
            <button
              key={profile}
              type="button"
              role="menuitemradio"
              className={`composer-access-menu__item composer-profile-menu__item${tokenMode === profile ? " composer-access-menu__item--active" : ""}`}
              onClick={() => chooseTokenMode(profile)}
              disabled={disabled || running}
              title={t(descKey)}
              aria-checked={tokenMode === profile}
            >
              <Icon size={16} strokeWidth={1.75} />
              <span className="composer-access-menu__copy">
                <span className="composer-access-menu__title">{t(titleKey)}</span>
                <span className="composer-access-menu__desc">{t(descKey)}</span>
              </span>
              {tokenMode === profile && <Check size={15} aria-hidden="true" />}
            </button>
          ))}
        </div>
      </AnchoredPopover>
      <AnchoredPopover
        open={moreMenuOpen && !disabled && !running}
        closing={moreMenuClosing}
        anchorRef={moreMenuAnchorRef}
        onClose={() => closeMoreMenu()}
        className="composer-access-menu composer-more-menu"
        align="end"
      >
        {hasEffort && (
          <div className="composer-access-menu__section">
            <div className="composer-access-menu__label">{t("status.effortTitle")}</div>
            <div className="composer-more-menu__items" role="listbox" aria-label={t("status.effortTitle")}>
              {effortLevels.map((level) => (
                <button
                  key={level}
                  type="button"
                  role="option"
                  aria-selected={level === currentEffort}
                  className={`composer-more-menu__item${level === currentEffort ? " composer-more-menu__item--active" : ""}`}
                  onClick={() => chooseEffortLevel(level)}
                  disabled={running}
                >
                  <Gauge size={14} />
                  <span>{level}</span>
                  {level === currentEffort && <Check size={13} />}
                </button>
              ))}
            </div>
          </div>
        )}
      </AnchoredPopover>
      {menuMode === "slash" && (
        <SlashMenu items={slashMatches} activeIndex={active} onPick={pickCommand} onHover={setActive} />
      )}
      {menuMode === "slasharg" && argRes && (
        <ArgMenu items={argRes.items} activeIndex={active} onPick={pickArg} onHover={setActive} />
      )}
      {(menuMode === "at" || menuMode === "pastChats") && (
        showPastChats ? (
          <div className="slashmenu" role="listbox">
            {loadingPastChats ? (
              <div className="slashmenu__item slashmenu__item--empty">
                <span className="slashmenu__name">正在加载历史会话...</span>
              </div>
            ) : pastChats.length === 0 ? (
              <div className="slashmenu__item slashmenu__item--empty">
                <span className="slashmenu__name">暂无历史会话</span>
              </div>
            ) : (
              <>
                <div className="slashmenu__item slashmenu__item--search" onMouseDown={(ev) => ev.preventDefault()}>
                  <Search size={13} className="filemenu__icon" />
                  <input
                    className="slashmenu__search"
                    type="text"
                    placeholder="搜索历史会话…"
                    value={pastChatQuery}
                    // In the token-driven flows (typed "#" or the content-menu
                    // action) focus must stay in the composer: typing there
                    // extends the token and filters the list, and stealing
                    // focus mid-word hijacks ordinary "#123" text. Only the
                    // @-flow subpanel, which has no composer token to type
                    // into, moves focus here.
                    autoFocus={!directPastChats}
                    onChange={(ev) => {
                      setPastChatQuery(ev.target.value);
                      setActive(0);
                    }}
                    onKeyDown={onPastChatSearchKeyDown}
                  />
                </div>
                {filteredPastChats.length === 0 ? (
                  <div className="slashmenu__item slashmenu__item--empty">
                    <span className="slashmenu__name">没有匹配的历史会话</span>
                  </div>
                ) : (
                  filteredPastChats.map((session, i) => {
                    // PR-C2: hover preview uses only the SessionMeta fields we
                    // already have on hand — no extra PreviewSession call, no
                    // backend round-trip, no read of the full transcript.
                    const turns = typeof session.turns === "number";
                    const ts = session.lastActivityAt || session.modTime || session.createdAt;
                    const preview = truncatePreview(session.preview);
                    const pathText = session.workspaceRoot || session.path;
                    const tooltipLabel =
                      turns || ts || preview || pathText ? (
                        <div className="past-chat-hover">
                          <div className="past-chat-hover__title">{pastChatTitle(session)}</div>
                          {preview && <div className="past-chat-hover__preview">{preview}</div>}
                          {(turns || ts) && (
                            <div className="past-chat-hover__meta">
                              {turns && <span>{session.turns} 轮</span>}
                              {ts && <span>· {fmtSessionTime(ts)}</span>}
                            </div>
                          )}
                          {pathText && <div className="past-chat-hover__path">{pathText}</div>}
                        </div>
                      ) : null;
                    return (
                      <Tooltip key={session.path} block label={tooltipLabel}>
                        <button
                          className={`slashmenu__item ${i === active ? "slashmenu__item--active" : ""}`}
                          onMouseDown={(ev) => {
                            ev.preventDefault();
                            pickSession(session);
                          }}
                          onMouseMove={() => setActive(i)}
                        >
                          <MessageSquare size={13} className="filemenu__icon" />
                          <span className="slashmenu__name slashmenu__name--file">
                            {pastChatTitle(session)}
                            {turns ? ` (${session.turns} 轮)` : ""}
                          </span>
                        </button>
                      </Tooltip>
                    );
                  })
                )}
              </>
            )}
            <button
              className="slashmenu__item slashmenu__item--back"
              onMouseDown={(ev) => {
                ev.preventDefault();
                if (menuMode === "pastChats") dismissDirectPastChats();
                else {
                  setPastChatQuery("");
                  setShowPastChats(false);
                  setActive(0);
                }
              }}
            >
              <span className="slashmenu__name">
                {menuMode === "pastChats" ? t("composer.contentCloseSessions") : "← 返回文件列表"}
              </span>
            </button>
          </div>
        ) : menuMode === "at" ? (
          <VirtualMenu
            items={atMenuItems}
            activeIndex={active}
            itemKey={(it) => (it.kind === "pastChats" ? "past:chats" : (it.entry.isDir ? "d:" : "f:") + (it.entry.path || it.entry.name))}
            renderItem={(it, i) =>
              it.kind === "pastChats" ? (
                <button
                  className={`slashmenu__item${i === active ? " slashmenu__item--active" : ""}`}
                  onMouseDown={(ev) => {
                    ev.preventDefault();
                    void openPastChats();
                  }}
                  onMouseMove={() => setActive(i)}
                >
                  <MessageSquare size={13} className="filemenu__icon" />
                  <span className="slashmenu__name">{PAST_CHATS_MENU_ITEM}</span>
                </button>
              ) : (
                <button
                  role="option"
                  aria-selected={i === active}
                  className={`slashmenu__item ${i === active ? "slashmenu__item--active" : ""}`}
                  onMouseDown={(ev) => {
                    ev.preventDefault();
                    pickEntry(it.entry);
                  }}
                  onMouseMove={() => setActive(i)}
                >
                  {it.entry.isDir ? (
                    <Folder size={13} className="filemenu__icon filemenu__icon--dir" />
                  ) : (
                    <FileText size={13} className="filemenu__icon" />
                  )}
                  <span className="slashmenu__name slashmenu__name--file">
                    {dirEntryMenuLabel(it.entry)}
                    {it.entry.isDir ? "/" : ""}
                  </span>
                </button>
              )
            }
          />
        ) : null
      )}
      {pendingGuidance.length > 0 && (
        <div className="composer-guidance-shelf" aria-label={t("composer.guidanceQueue")}>
          <div className="composer-guidance-head">
            <span className="composer-guidance-head__label">
              <CornerDownRight size={14} />
              <span>{t("composer.guidanceCount", { n: pendingGuidance.length })}</span>
            </span>
          </div>
          <div className="composer-guidance-list">
            {visibleGuidance.map((item) => (
              <div className="composer-guidance-item" key={item.id}>
                <CornerDownRight size={14} className="composer-guidance-item__icon" />
                <span className="composer-guidance-item__text">{item.text}</span>
                <Tooltip label={t("composer.guidanceSend")}>
                  <button
                    className="composer-guidance-item__guide"
                    type="button"
                    aria-label={t("composer.guidanceSend")}
                    disabled={!running || disabled || readOnly || guidanceSendingId !== null || Boolean(item.structured)}
                    onClick={() => void sendQueuedGuidance(item)}
                  >
                    <CornerDownRight size={13} />
                    <span>{t("composer.guidanceMode")}</span>
                  </button>
                </Tooltip>
                <Tooltip label={t("composer.guidanceDismiss")}>
                  <button
                    className="composer-guidance-item__action"
                    type="button"
                    aria-label={t("composer.guidanceDismiss")}
                    disabled={guidanceSendingId === item.id}
                    onClick={() => updatePendingGuidanceForDraft(
                      activeDraftKeyRef.current,
                      (items) => items.filter((queued) => queued.id !== item.id),
                    )}
                  >
                    <Trash2 size={14} />
                  </button>
                </Tooltip>
              </div>
            ))}
            {showGuidanceExpander && (
              <button
                className="composer-guidance-more"
                type="button"
                aria-expanded={guidanceExpanded}
                onClick={() => setGuidanceExpanded((value) => !value)}
              >
                {guidanceExpanded ? <ChevronUp size={13} /> : <ChevronDown size={13} />}
                <span>{guidanceExpanded ? t("composer.guidanceCollapse") : t("composer.guidanceRemaining", { n: hiddenGuidanceCount })}</span>
              </button>
            )}
          </div>
        </div>
      )}
      {(attachments.length > 0 || workspaceRefs.length > 0 || sessionRefs.length > 0 || selectedTextRefs.length > 0) && (
        <div className="composer-context" aria-label={t("composer.contextItems")}>
          {sortComposerAttachments(attachments).map((a) => {
            const imageOnly = Boolean(a.previewUrl) && attachments.every((item) => item.previewUrl) && workspaceRefs.length === 0 && sessionRefs.length === 0;
            return (
              <ComposerContextCard
                key={a.path}
                variant="attachment"
                tooltipLabel={a.previewUrl ? `${t("imageViewer.clickToPreview")} — ${a.path}` : a.path}
                removeLabel={t("composer.removeImage")}
                onRemove={() => removeAttachment(a.path)}
                previewUrl={a.previewUrl}
                onImageClick={a.previewUrl ? () => openComposerImageViewer(a.previewUrl!, attachmentName(a)) : undefined}
                imageOnly={imageOnly}
                name={attachmentName(a)}
                meta={attachmentExt(attachmentName(a)) || t("msg.fileAttachment")}
              />
            );
          })}
          {workspaceRefs.map((ref) => (
            <ComposerContextCard
              key={workspaceReferenceKey(ref)}
              variant="workspace"
              tooltipLabel={ref.displayPath ? formatWorkspaceReference(ref.displayPath, ref.isDir) : formatWorkspaceReference(ref.path, ref.isDir)}
              removeLabel={t("composer.removeReference")}
              onRemove={() => removeWorkspaceReference(ref)}
              folder={Boolean(ref.isDir)}
              label={ref.isDir ? `${baseName(ref.displayPath || ref.path)}/` : baseName(ref.displayPath || ref.path)}
            />
          ))}
          {sessionRefs.map((ref) => (
            <div
              className="composer-context__item composer-context__item--session"
              key={ref.path}
            >
              <Tooltip label={ref.preview || ref.title}>
                <span className="composer-context__label">
                  <MessageSquare size={15} />
                  <span>
                    {ref.title}
                    {typeof ref.turns === "number" ? ` (${ref.turns} 轮)` : ""}
                  </span>
                </span>
              </Tooltip>
              <Tooltip label="移除引用会话">
                <button
                  type="button"
                  onClick={() => removeSessionRef(ref.path)}
                >
                  <X size={13} />
                </button>
              </Tooltip>
            </div>
          ))}
          {selectedTextRefs.map((reference) => (
            <ComposerContextCard
              key={reference.id}
              variant="selection"
              tooltipLabel={reference.path
                ? <CodeViewer value={reference.text} language={languageFor(reference.path)} maxHeight={240} />
                : <Markdown text={reference.text} />}
              removeLabel={t("composer.removeSelectedText")}
              onRemove={() => {
                const next = selectedTextRefsRef.current.filter((item) => item.id !== reference.id);
                selectedTextRefsRef.current = next;
                setSelectedTextRefs(next);
                requestAnimationFrame(focusComposerInput);
              }}
              name={reference.path ? reference.path.split("/").filter(Boolean).pop() ?? reference.path : selectedTextSnippet(reference.text)}
              meta={reference.path ? t("composer.selectedCode") : t("composer.selectedText")}
              icon={reference.path ? <FileText size={20} /> : <MessageSquare size={20} />}
            />
          ))}
        </div>
      )}
      <ImageViewer
        open={imageViewer.open}
        imageUrl={imageViewer.url}
        imageName={imageViewer.name}
        onClose={closeComposerImageViewer}
      />
      {activePastedBlocks.length > 0 && (
        <div className="composer__pasted">
          {activePastedBlocks.map((block) => {
            const open = openPastedLabels.includes(block.label);
            return (
              <div className="composer__pasted-block" key={block.label}>
                <div className="composer__pasted-head">
                  <FileText size={15} />
                  <span className="composer__pasted-label">{block.label}</span>
                  <div className="composer__pasted-actions">
                    <Tooltip label={t(open ? "composer.pastedHidePreview" : "composer.pastedShowPreview")}>
                      <button type="button" onClick={() => togglePastedPreview(block.label)}>
                        <Eye size={14} />
                      </button>
                    </Tooltip>
                    <Tooltip label={t("composer.pastedExpand")}>
                      <button type="button" onClick={() => expandPastedBlock(block)}>
                        {t("composer.pastedExpand")}
                      </button>
                    </Tooltip>
                    <Tooltip label={t("composer.pastedRemove")}>
                      <button type="button" onClick={() => removePastedBlock(block)}>
                        <Trash2 size={14} />
                      </button>
                    </Tooltip>
                  </div>
                </div>
                {open && <pre className="composer__pasted-preview">{block.text}</pre>}
              </div>
            );
          })}
        </div>
      )}
      <div
        className={`composer-card${composerHeight !== null || composerResizing ? " composer-card--resized" : ""}${composerAutoExpanded ? " composer-card--autosized" : ""}${composerResizing ? " composer-card--resizing" : ""}${running ? (waitingPrompt ? " composer-card--waiting" : " composer-card--running") : ""}`}
        ref={composerCardRef}
        style={composerCardStyle}
      >
        <button
          className="composer-resize-handle"
          type="button"
          role="separator"
          aria-orientation="horizontal"
          aria-label={t("composer.resize")}
          aria-valuemin={COMPOSER_MIN_HEIGHT}
          aria-valuemax={composerMaxHeight()}
          aria-valuenow={composerResizeValue}
          title={t("composer.resize")}
          onPointerDown={onComposerResizeStart}
          onKeyDown={onComposerResizeKeyDown}
          onDoubleClick={resetComposerHeight}
        />
        {runStateText && (
          <div className={`composer-run-strip${waitingPrompt ? " composer-run-strip--waiting" : ""}`}>
            <span className="composer-run-strip__dot" aria-hidden="true" />
            {/* The ticker re-renders every second; keep it out of the accessibility
                tree and announce only the stable state text via the live region. */}
            <span className="composer-run-strip__text" aria-hidden={runTicker ? true : undefined}>
              {runTicker ?? runStateText}
            </span>
            <span className="sr-only" role="status">{runStateText}</span>
          </div>
        )}
        <div
          className={`composer${invocations.length > 0 ? " composer--has-invocation" : ""}${dragOver ? " composer--dragover" : ""}${disabled || readOnly ? " composer--disabled" : ""}${shellModeActive ? " composer--shell" : ""}`}
          onDrop={onDrop}
          onDragOver={onDragOver}
          onDragLeave={onDragLeave}
        >
          <div className="composer__input-row">
            <span className="composer__caret">{shellModeActive ? "$" : "›"}</span>
            <div className="composer__content" onMouseDown={focusComposerFromContentBlank}>
              {invocations.length > 0 ? (
                <RichComposerInput
                  ref={richInputRef}
                  text={text}
                  invocations={invocations}
                  placeholder={composerPlaceholder}
                  disabled={disabled || readOnly}
                  style={textareaStyle}
                  onChange={(nextText, nextInvocations) => {
                    resetPromptHistoryNavigation();
                    const hadInvocations = invocationsRef.current.length > 0;
                    textRef.current = nextText;
                    invocationsRef.current = nextInvocations;
                    setText(nextText);
                    setInvocations(nextInvocations);
                    if (composerPrompt) setComposerPrompt(null);
                    if (hadInvocations && nextInvocations.length === 0) {
                      // Removing the last entity unmounts the rich input and
                      // swaps the plain textarea back in; without an explicit
                      // handoff the focused editable disappears and the next
                      // keystrokes land on <body>. RichComposerInput reports
                      // the removal caret through onSelectionChange before
                      // this onChange fires.
                      setComposerSelection(Math.min(lastSelectionRef.current.start, nextText.length));
                    }
                  }}
                  onSelectionChange={(selection, query) => {
                    setRichSelection(selection);
                    setRichSlashQuery(query);
                    lastSelectionRef.current = { start: selection.start, end: selection.end };
                  }}
                  onKeyDown={onKeyDown}
                  onPaste={onPaste}
                  onCompositionStart={() => {
                    composingRef.current = true;
                  }}
                  onCompositionEnd={() => {
                    composingRef.current = false;
                    lastCompositionEndAt.current = Date.now();
                  }}
                />
              ) : (
                <textarea
                  id="composer-input"
                  ref={taRef}
                  className="composer__input"
                  aria-label={t("composer.placeholder")}
                  value={text}
                  onChange={(e) => {
                    resetPromptHistoryNavigation();
                    textRef.current = e.target.value;
                    setText(e.target.value);
                    if (composerPrompt) setComposerPrompt(null);
                  }}
                  onSelect={rememberCaret}
                  onClick={rememberCaret}
                  onKeyUp={rememberCaret}
                  onFocus={rememberCaret}
                  onContextMenu={openInputMenu}
                  onPaste={onPaste}
                  onKeyDown={onKeyDown}
                  onCompositionStart={() => {
                    composingRef.current = true;
                  }}
                  onCompositionEnd={() => {
                    composingRef.current = false;
                    lastCompositionEndAt.current = Date.now();
                  }}
                  style={textareaStyle}
                  placeholder={composerPlaceholder}
                  rows={1}
                  disabled={disabled || readOnly}
                />
              )}
            </div>
            {composerPrompt && (
              <span className="composer__prompt" role="status">
                {composerPrompt}
              </span>
            )}
            {running && (
              <Tooltip label={t("composer.stop")}>
                <button
                  className="composer__btn composer__btn--stop"
                  type="button"
                  onClick={handleCancel}
                  aria-label={t("composer.stop")}
                >
                  <Square size={12} fill="currentColor" />
                </button>
              </Tooltip>
            )}
            <Tooltip label={submitTooltip}>
              <button
                className={`composer__btn composer__btn--send${running ? " composer__btn--steer" : ""}`}
                onClick={submit}
                disabled={submitBlocked}
                aria-label={submitTooltip}
              >
                {running ? <CornerDownRight size={16} /> : <ArrowUp size={16} />}
              </button>
            </Tooltip>
          </div>
        </div>
        <ContextMenu
          open={inputMenuPoint !== null}
          point={inputMenuPoint}
          items={inputMenuItems}
          className="context-menu--composer-input"
          minWidth={64}
          ariaLabel={t("composer.inputActions")}
          onClose={() => setInputMenuPoint(null)}
        />
        <div className={composerMetaClass}>
          <div className="composer-meta__params">
            <div className="composer-meta__control composer-meta__control--content">
              <Tooltip label={t("composer.contentMenuTitle")} disabled={contentMenuOpen}>
                <button
                  ref={contentMenuAnchorRef}
                  type="button"
                  className={`composer-content-trigger${contentMenuOpen ? " composer-content-trigger--open" : ""}`}
                  onClick={() => (contentMenuOpen ? setContentMenuOpen(false) : openContentMenu())}
                  disabled={disabled || readOnly || running}
                  aria-haspopup="menu"
                  aria-expanded={contentMenuOpen}
                  aria-label={t("composer.contentMenuTitle")}
                >
                  <Plus size={17} strokeWidth={1.8} aria-hidden="true" />
                </button>
              </Tooltip>
            </div>
            <div className="composer-meta__control composer-meta__control--intent">
              <Tooltip label={taskModeTooltipLabel} disabled={intentMenuOpen || intentMenuClosing}>
                <button
                  ref={intentMenuAnchorRef}
                  type="button"
                  className={`composer-task-mode-trigger${intentMenuOpen || intentMenuClosing ? " composer-task-mode-trigger--open" : ""}`}
                  onClick={() => (intentMenuOpen || intentMenuClosing ? closeIntentMenu() : openIntentMenu())}
                  disabled={disabled || running}
                  aria-haspopup="menu"
                  aria-expanded={intentMenuOpen && !intentMenuClosing}
                  aria-label={taskModeTriggerLabel}
                  title={intentMenuOpen || intentMenuClosing ? undefined : taskModeTriggerLabel}
                >
                  <TaskModeIcon size={14} aria-hidden="true" />
                  <span className="composer-task-mode-trigger__value">{t(taskModeShortKey)}</span>
                  <ChevronsUpDown size={11} aria-hidden="true" />
                </button>
              </Tooltip>
            </div>
            <div className="composer-meta__control composer-meta__control--profile">
              <Tooltip label={runtimeProfileTooltipLabel} disabled={profileMenuOpen || profileMenuClosing}>
                <button
                  ref={profileMenuAnchorRef}
                  type="button"
                  data-profile={tokenMode}
                  className={`composer-profile-trigger${profileMenuOpen || profileMenuClosing ? " composer-profile-trigger--open" : ""}`}
                  onClick={() => (profileMenuOpen || profileMenuClosing ? closeProfileMenu() : openProfileMenu())}
                  disabled={disabled || running}
                  aria-haspopup="menu"
                  aria-expanded={profileMenuOpen && !profileMenuClosing}
                  aria-label={runtimeProfileTriggerLabel}
                  title={profileMenuOpen || profileMenuClosing ? undefined : runtimeProfileTriggerLabel}
                >
                  <RuntimeProfileIcon size={14} strokeWidth={1.75} aria-hidden="true" />
                  <span className="composer-profile-trigger__label">
                    <span className="composer-profile-trigger__value">{t(runtimeProfileShortKey)}</span>
                  </span>
                  <ChevronsUpDown size={11} aria-hidden="true" />
                </button>
              </Tooltip>
            </div>
            <div className="composer-meta__control composer-meta__control--approval">
              {/* A pending tool approval disables the composer, but the approval
                  bar stays usable so mode changes remain possible mid-prompt;
                  the approval card explains that the pending request still needs
                  an explicit decision. */}
              <div className="composer-modebar composer-modebar--approval" data-mode={toolApprovalMode} title={t("composer.accessMenuTitle")}>
                <span className="composer-modebar__thumb" aria-hidden="true" />
                <button
                  type="button"
                  className={`composer-modebar__item composer-modebar__item--ask${toolApprovalMode === "ask" ? " composer-modebar__item--active" : ""}`}
                  onClick={() => chooseApprovalMode("ask")}
                  disabled={approvalBarDisabled}
                  aria-pressed={toolApprovalMode === "ask"}
                  title={t("composer.accessAskTitle")}
                >
                  <Shield size={14} />
                  <span>{t("composer.modeAsk")}</span>
                </button>
                <button
                  type="button"
                  className={`composer-modebar__item composer-modebar__item--auto${toolApprovalMode === "auto" ? " composer-modebar__item--active" : ""}`}
                  onClick={() => chooseApprovalMode("auto")}
                  disabled={approvalBarDisabled}
                  aria-pressed={toolApprovalMode === "auto"}
                  title={t("composer.accessAutoTitle")}
                >
                  <ShieldCheck size={14} />
                  <span>{t("composer.modeNormal")}</span>
                </button>
                <button
                  type="button"
                  className={`composer-modebar__item composer-modebar__item--yolo${toolApprovalMode === "yolo" ? " composer-modebar__item--active" : ""}`}
                  onClick={() => chooseApprovalMode("yolo")}
                  disabled={approvalBarDisabled}
                  aria-pressed={toolApprovalMode === "yolo"}
                  title={t("composer.accessYoloTitle")}
                >
                  <ShieldAlert size={14} />
                  <span>{t("composer.modeYolo")}</span>
                </button>
              </div>
            </div>
            <span className="composer-meta__divider" aria-hidden="true" />
            <div className="composer-meta__control composer-meta__control--model">
              {/*
                Creation-only: showContextWindowRing is wired to sidebarCreation
                (desktopLayoutStyle === "creation") in App.tsx. The ring popover
                is portaled to <body> without an .app--creation prefix, so its
                styles look global but only ever apply in creation layout. If you
                ever surface this ring in another layout, its font sizes already
                scale via --font-scale (see .context-ring-popover in styles.css).
              */}
              {showContextWindowRing && (
                <ContextWindowRing
                  enabled={showContextWindowRing}
                  context={context}
                  tabId={tabId}
                  turnCost={turnCost}
                  cacheHitTokens={cacheHitTokens}
                  cacheMissTokens={cacheMissTokens}
                  balance={balance}
                />
              )}
              <ModelSwitcher label={modelLabel} tabId={tabId} onPick={onSwitchModel} />
            </div>
            {hasEffort && (
              <div className="composer-meta__control composer-meta__control--effort">
                <EffortSwitcher effort={effort} disabled={running} onPick={onSetEffort} />
              </div>
            )}
            {hasEffort && (
              <div className="composer-meta__control composer-meta__control--more">
                <Tooltip label={compactEffortTitle} disabled={moreMenuOpen || moreMenuClosing}>
                  <button
                    ref={moreMenuAnchorRef}
                    type="button"
                    className={`composer-more-trigger composer-more-trigger--effort${currentEffort !== "auto" ? " composer-more-trigger--explicit" : ""}${moreMenuOpen || moreMenuClosing ? " composer-more-trigger--open" : ""}`}
                    onClick={() => (moreMenuOpen || moreMenuClosing ? closeMoreMenu() : openMoreMenu())}
                    disabled={disabled || running}
                    aria-haspopup="menu"
                    aria-expanded={moreMenuOpen && !moreMenuClosing}
                    aria-label={compactEffortTitle}
                    title={moreMenuOpen || moreMenuClosing ? undefined : compactEffortTitle}
                  >
                    <Gauge size={14} />
                    <span>{currentEffort}</span>
                    <ChevronsUpDown size={11} />
                  </button>
                </Tooltip>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
