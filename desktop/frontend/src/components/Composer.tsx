import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, ClipboardEvent, DragEvent, KeyboardEvent, PointerEvent as ReactPointerEvent } from "react";
import { ArrowUp, Check, Eye, FileText, Folder, Gauge, List, MessageSquare, MoreHorizontal, Search, Shield, ShieldAlert, ShieldCheck, SlidersHorizontal, Square, Target, Trash2, X } from "lucide-react";
import { asArray } from "../lib/array";
import { filterAtMatches } from "../lib/atMatches";
import { DedupIndex, sha256 } from "../lib/attachDedup";
import { app, onFilesDropped } from "../lib/bridge";
import { canUsePromptHistory, isFnKeyEvent, promptHistoryDirectionFromEvent } from "../lib/composerKeyboard";
import { cacheGeneration, loadOlder } from "../lib/composerHistory";
import { SPINNER_WORDS, useI18n } from "../lib/i18n";
import { detectShortcutPlatform, matchesShortcut } from "../lib/keyboardShortcuts";
import { clearLayoutSize, loadOptionalLayoutSize, saveLayoutSize } from "../lib/layoutPreferences";
import { useToast } from "../lib/toast";
import { type CollaborationMode, type CommandInfo, type ComposerInsertRequest, type DirEntry, type EffortInfo, type HistoryMessage, type Mode, type PromptHistoryEntry, type SessionMeta, type SessionReference, type SlashArgItem, type SlashArgsResult, type TokenMode, type ToolApprovalMode } from "../lib/types";
import {
  formatWorkspaceReference,
  parseWorkspaceReference,
  readWorkspaceReferenceDrag,
  WORKSPACE_REF_DRAG_TYPE,
} from "../lib/workspaceDrag";
import { SlashMenu } from "./SlashMenu";
import { ArgMenu } from "./ArgMenu";
import { VirtualMenu } from "./VirtualMenu";
import { ANCHORED_POPOVER_CLOSE_MS, AnchoredPopover } from "./AnchoredPopover";
import { EffortSwitcher } from "./EffortSwitcher";
import { ModelSwitcher } from "./ModelSwitcher";
import { Tooltip } from "./Tooltip";
import { ComposerContextCard } from "./ComposerContextCard";

interface Attachment {
  path: string;
  previewUrl?: string;
  displayName?: string;
}

interface AttachmentDedupKey {
  hash: string;
  source: string;
}

interface WorkspaceReference {
  path: string;
  isDir?: boolean;
}

const LONG_PASTE_MIN_CHARS = 2000;
const LONG_PASTE_MIN_LINES = 20;
const COMPOSER_MIN_HEIGHT = 104;
const COMPOSER_MAX_HEIGHT = 360;
const COMPOSER_MAX_VIEWPORT_RATIO = 0.4;
const COMPOSER_AUTO_RESERVED_HEIGHT = 58;
const PROMPT_HISTORY_PREFETCH_REMAINING = 3;
// Grace after compositionend to swallow a confirm-Enter that lands just after
// it; the real gap is a few ms, so keep it short or a deliberate quick second
// Enter (submit) gets eaten too.
const IME_CONFIRM_GRACE_MS = 100;

type PastedBlock = {
  label: string;
  text: string;
};

type WebkitFileEntry = {
  isDirectory?: boolean;
};

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

function isPasteShortcut(e: KeyboardEvent<HTMLTextAreaElement>): boolean {
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

function clampComposerHeight(height: number): number {
  return Math.min(Math.max(Math.round(height), COMPOSER_MIN_HEIGHT), composerMaxHeight());
}

function composerAutoInputMaxHeight(): number {
  return Math.max(32, composerMaxHeight() - COMPOSER_AUTO_RESERVED_HEIGHT);
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
  e: KeyboardEvent<HTMLTextAreaElement>,
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
  tabId,
  effort,
  onSend,
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
  disabled,
  submitDisabled = false,
  readOnly = false,
  decisionPending = false,
  ready,
  turnStartAt,
  turnTokens,
  retry,
  transientDismissSignal,
}: {
  running: boolean;
  collaborationMode: CollaborationMode;
  toolApprovalMode: ToolApprovalMode;
  tokenMode: TokenMode;
  goal?: string;
  cwd?: string;
  modelLabel: string;
  tabId?: string;
  effort?: EffortInfo;
  onSend: (displayText: string, submitText?: string) => void;
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
  disabled?: boolean;
  submitDisabled?: boolean;
  readOnly?: boolean;
  decisionPending?: boolean;
  // ready/cwd/running re-trigger the command fetch: Commands() returns only
  // built-ins until boot.Build finishes (the controller, hence skills/custom/MCP,
  // is nil before then), the available set changes when the workspace switches,
  // and a completed turn may have installed skills or MCP prompts.
  ready?: boolean;
  turnStartAt?: number;
  turnTokens?: number;
  retry?: { attempt: number; max: number };
  transientDismissSignal?: number;
}) {
  const { t, locale } = useI18n();
  const { showToast } = useToast();
  const shortcutPlatform = useMemo(() => detectShortcutPlatform(), []);
  const now = useTick(running);
  const [text, setText] = useState("");
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [workspaceRefs, setWorkspaceRefs] = useState<WorkspaceReference[]>([]);
  const [pastedBlocks, setPastedBlocks] = useState<PastedBlock[]>([]);
  const [openPastedLabels, setOpenPastedLabels] = useState<string[]>([]);
  const [pendingPaste, setPendingPaste] = useState(0);
  const pastedBlocksRef = useRef<PastedBlock[]>([]);
  const nextPasteId = useRef(1);
  const [active, setActive] = useState(0);
  const [dismissed, setDismissed] = useState(false);
  const [dragOver, setDragOver] = useState(false);
  const [composerHeight, setComposerHeight] = useState<number | null>(loadComposerHeight);
  const [composerResizing, setComposerResizing] = useState(false);
  const [textareaAutoHeight, setTextareaAutoHeight] = useState<number | null>(null);
  const [textareaAutoOverflow, setTextareaAutoOverflow] = useState(false);
  const [intentMenuOpen, setIntentMenuOpen] = useState(false);
  const [intentMenuClosing, setIntentMenuClosing] = useState(false);
  const [moreMenuOpen, setMoreMenuOpen] = useState(false);
  const [moreMenuClosing, setMoreMenuClosing] = useState(false);
  const [showPastChats, setShowPastChats] = useState(false);
  const [pastChats, setPastChats] = useState<SessionMeta[]>([]);
  const [pastChatQuery, setPastChatQuery] = useState("");
  const [sessionRefs, setSessionRefs] = useState<SessionReference[]>([]);
  const [loadingPastChats, setLoadingPastChats] = useState(false);
  const [submitting, setSubmitting] = useState(false);
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
  const composerCardRef = useRef<HTMLDivElement>(null);
  const intentMenuAnchorRef = useRef<HTMLButtonElement>(null);
  const moreMenuAnchorRef = useRef<HTMLButtonElement>(null);
  const intentCloseTimerRef = useRef<number | null>(null);
  const moreCloseTimerRef = useRef<number | null>(null);
  const wasRunning = useRef(running);
  const composingRef = useRef(false);
  const lastCompositionEndAt = useRef(0);
  const lastSelectionRef = useRef({ start: 0, end: 0 });
  const consumedInsertIdRef = useRef(0);
  const lastTransientDismissSignal = useRef(transientDismissSignal);
  const submittingRef = useRef(false);
  const nativeClipboardPasteTimerRef = useRef<number | null>(null);
  // Snapshot of the current cwd so async callbacks (openPastChats) can detect
  // workspace switches and discard stale responses (issue #3601).
  const cwdRef = useRef(cwd);
  cwdRef.current = cwd;
  const attachmentDedupRef = useRef(new DedupIndex());
  const attachmentDedupKeysRef = useRef<Record<string, AttachmentDedupKey>>({});

  const clearNativeClipboardPasteTimer = () => {
    if (nativeClipboardPasteTimerRef.current === null) return;
    window.clearTimeout(nativeClipboardPasteTimerRef.current);
    nativeClipboardPasteTimerRef.current = null;
  };

  useEffect(() => () => clearNativeClipboardPasteTimer(), []);

  useEffect(() => {
    if (wasRunning.current && !running && text.trim() === "") {
      pastedBlocksRef.current = [];
      setPastedBlocks([]);
      setOpenPastedLabels([]);
    }
    wasRunning.current = running;
  }, [running, text]);

  // --- slash commands (whole-input "/token") ---
  const [commands, setCommands] = useState<CommandInfo[]>([]);
  useEffect(() => {
    app.Commands().then((next) => setCommands(asArray(next))).catch(() => {});
  }, [ready, cwd, running]);

  const slashQuery = useMemo(() => {
    if (!text.startsWith("/") || /\s/.test(text)) return null;
    return text.slice(1).toLowerCase();
  }, [text]);
  const slashMatches = useMemo(
    () => (slashQuery === null ? [] : commands.filter((c) => c.name.toLowerCase().includes(slashQuery))),
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
    if (!text.startsWith("/") || !/\s/.test(text)) {
      setArgRes(null);
      return;
    }
    let live = true;
    clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      app
        .SlashArgs(text)
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
          const useful = items.filter((it) => text.slice(0, from) + it.insert !== text);
          setArgRes(useful.length > 0 ? { items: useful, from } : null);
          setActive(0);
        })
        .catch(() => {});
    }, 120);
    return () => {
      live = false;
      clearTimeout(debounceRef.current);
    };
  }, [text]);

  // --- @ file references (token at the end of the text) ---
  // atRaw is everything after a trailing "@token"; atDir is its path up to the
  // last "/", atFrag the part after. The menu lists one directory level (atDir)
  // and filters by atFrag — descending one level per pick.
  const atRaw = useMemo(() => {
    const m = /(?:^|\s)@([^\s]*)$/.exec(text);
    return m ? m[1] : null;
  }, [text]);
  const atDir = useMemo(() => {
    if (atRaw === null) return "";
    const slash = atRaw.lastIndexOf("/");
    return slash >= 0 ? atRaw.slice(0, slash + 1) : "";
  }, [atRaw]);
  const atFrag = useMemo(() => {
    if (atRaw === null) return "";
    const slash = atRaw.lastIndexOf("/");
    return (slash >= 0 ? atRaw.slice(slash + 1) : atRaw).toLowerCase();
  }, [atRaw]);

  const [entries, setEntries] = useState<DirEntry[]>([]);
  const [searchEntries, setSearchEntries] = useState<DirEntry[]>([]);
  const dirCache = useRef<Record<string, DirEntry[]>>({});
  const searchCache = useRef<Record<string, DirEntry[]>>({});

  // When the workspace/project changes (cwd prop), invalidate all @ mention
  // state so the picker reloads candidates for the new project. Without this,
  // dirCache/searchCache retain entries from the old project and the picker
  // shows stale results (issue #3601).
  const prevCwdRef = useRef(cwd);
  useEffect(() => {
    if (prevCwdRef.current === cwd) return; // skip mount — state already initial
    prevCwdRef.current = cwd;
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
  }, [cwd]);

  useEffect(() => {
    if (atRaw === null) return;
    const cached = dirCache.current[atDir];
    if (cached) {
      setEntries(cached);
      return;
    }
    let live = true;
    app
      .ListDir(atDir)
      .then((es) => {
        const list = asArray(es);
        dirCache.current[atDir] = list;
        if (live) setEntries(list);
      })
      .catch(() => {});
    return () => {
      live = false;
    };
    // re-fetch when the menu opens or the directory level changes
  }, [atRaw === null, atDir, cwd]);
  useEffect(() => {
    if (atRaw === null || atDir !== "" || atFrag === "") {
      setSearchEntries([]);
      return;
    }
    const cached = searchCache.current[atFrag];
    if (cached) {
      setSearchEntries(cached);
      return;
    }
    setSearchEntries([]);
    let live = true;
    app
      .SearchFileRefs(atFrag)
      .then((es) => {
        const list = es ?? [];
        searchCache.current[atFrag] = list;
        if (live) setSearchEntries(list);
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [atRaw === null, atDir, atFrag, cwd]);
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
  const menuMode: "slash" | "slasharg" | "at" | null =
    slashMatches.length > 0 && !dismissed
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
          : 0;

  // Reset highlight + un-dismiss whenever the active query changes.
  useEffect(() => {
    setActive(0);
    setDismissed(false);
  }, [slashQuery, atRaw]);

  useEffect(() => {
    if (transientDismissSignal === undefined || transientDismissSignal === lastTransientDismissSignal.current) return;
    lastTransientDismissSignal.current = transientDismissSignal;
    setDismissed(true);
  }, [transientDismissSignal]);

  // When the @ trigger disappears (user deleted the @), close the past:chats
  // sub-menu and reset related state. Without this, showPastChats can outlive
  // the @ token and leave the session list visible with no way to dismiss it.
  useEffect(() => {
    if (menuMode !== "at" && showPastChats) {
      setShowPastChats(false);
      setPastChatQuery("");
      setActive(0);
    }
  }, [menuMode]);

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

  const setTextCaretEnd = (next: string) => {
    setText(next);
    requestAnimationFrame(() => {
      const ta = taRef.current;
      if (ta) {
        ta.focus();
        ta.selectionStart = ta.selectionEnd = next.length;
      }
    });
  };

  const rememberCaret = () => {
    const ta = taRef.current;
    if (!ta) return;
    lastSelectionRef.current = { start: ta.selectionStart ?? text.length, end: ta.selectionEnd ?? text.length };
  };

  const insertTextAtCaret = (snippet: string) => {
    const ta = taRef.current;
    const start = ta ? (ta.selectionStart ?? text.length) : Math.min(lastSelectionRef.current.start, text.length);
    const end = ta ? (ta.selectionEnd ?? start) : Math.min(lastSelectionRef.current.end, text.length);
    const before = text.slice(0, start);
    const after = text.slice(end);
    const leading = before.length === 0 || before.endsWith("\n\n") ? "" : before.endsWith("\n") ? "\n" : "\n\n";
    const body = snippet.trimEnd();
    const trailing = after.length === 0 ? "\n" : after.startsWith("\n") ? "" : "\n\n";
    const inserted = leading + body + trailing;
    const next = before + inserted + after;
    const pos = before.length + inserted.length;
    setText(next);
    requestAnimationFrame(() => {
      const node = taRef.current;
      if (!node) return;
      node.focus();
      node.selectionStart = node.selectionEnd = pos;
      lastSelectionRef.current = { start: pos, end: pos };
    });
  };

  const replaceComposerText = (next: string) => {
    clearAttachments();
    setWorkspaceRefs([]);
    setSessionRefs([]);
    pastedBlocksRef.current = [];
    setPastedBlocks([]);
    setOpenPastedLabels([]);
    setTextCaretEnd(next);
  };

  const addWorkspaceReference = (ref: WorkspaceReference) => {
    setWorkspaceRefs((prev) => {
      const key = workspaceReferenceKey(ref);
      if (prev.some((item) => workspaceReferenceKey(item) === key)) return prev;
      return [...prev, ref];
    });
    requestAnimationFrame(() => taRef.current?.focus());
  };

  useEffect(() => {
    if (!insertRequest || insertRequest.id === consumedInsertIdRef.current) return;
    consumedInsertIdRef.current = insertRequest.id;
    if (insertRequest.mode === "replace") {
      replaceComposerText(insertRequest.text);
      return;
    }
    const ref = parseWorkspaceReference(insertRequest.text);
    if (ref) {
      addWorkspaceReference(ref);
      return;
    }
    insertTextAtCaret(insertRequest.text);
  }, [insertRequest]);

  const expandPastedBlocks = (displayText: string): string => {
    let expanded = displayText;
    for (const block of pastedBlocksRef.current) {
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
    setAttachments([]);
    attachmentDedupRef.current.clear();
    attachmentDedupKeysRef.current = {};
  };

  const removeAttachment = (path: string) => {
    forgetAttachment(path);
    setAttachments((prev) => prev.filter((x) => x.path !== path));
    requestAnimationFrame(() => taRef.current?.focus());
  };

  const clearIntentCloseTimer = useCallback(() => {
    if (intentCloseTimerRef.current === null) return;
    window.clearTimeout(intentCloseTimerRef.current);
    intentCloseTimerRef.current = null;
  }, []);

  const openIntentMenu = useCallback(() => {
    clearIntentCloseTimer();
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

  const clearMoreCloseTimer = useCallback(() => {
    if (moreCloseTimerRef.current === null) return;
    window.clearTimeout(moreCloseTimerRef.current);
    moreCloseTimerRef.current = null;
  }, []);

  const openMoreMenu = useCallback(() => {
    clearMoreCloseTimer();
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
  const tokenModeOn = tokenMode === "economy";

  const submit = async () => {
    if (disabled || submitDisabled || readOnly || submittingRef.current) return;
    const trimmedText = text.trim();
    if (pendingPaste > 0) return;
    if (!trimmedText && attachments.length === 0 && workspaceRefs.length === 0) {
      if (goalModeOn && !activeGoal) {
        setComposerPrompt(t("composer.goalInputRequired"));
        requestAnimationFrame(() => taRef.current?.focus());
      }
      return;
    }
    setComposerPrompt(null);
    submittingRef.current = true;
    setSubmitting(true);
    try {
    const orderedAttachments = sortComposerAttachments(attachments);
    const refs = [
      ...workspaceRefs.map((ref) => formatWorkspaceReference(ref.path, ref.isDir)),
      ...orderedAttachments.map((a) => `@${a.path}`),
    ].join(" ");
    const displayRefs = [
      ...workspaceRefs.map((ref) => formatWorkspaceReference(ref.path, ref.isDir)),
      ...orderedAttachments.map(formatAttachmentDisplayReference),
    ].join(" ");
    const displayText = [trimmedText, displayRefs].filter(Boolean).join(trimmedText && displayRefs ? " " : "");
    // PR-B: when past:chats refs are attached, prepend their formatted transcript
    // to submitText only (displayText stays unchanged so the user still sees their
    // original prompt in the input preview). With no refs we keep the original
    // submitText verbatim — no header, no rewording, byte-identical to pre-PR-B.
    const sessionContext = sessionRefs.length === 0 ? "" : await buildSessionContext(sessionRefs);
    const baseSubmitText = [expandPastedBlocks(trimmedText), refs].filter(Boolean).join(trimmedText && refs ? " " : "");
    const submitText = sessionContext ? `${sessionContext}${baseSubmitText}` : baseSubmitText;
    onSend(displayText, submitText);
    setText("");
    historyIndexRef.current = -1;
    setHistoryIndex(-1);
    clearAttachments();
    setWorkspaceRefs([]);
    setSessionRefs([]);
    } finally {
      submittingRef.current = false;
      setSubmitting(false);
    }
  };

  const readFileAsDataURL = (file: File) =>
    new Promise<string>((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result));
      reader.onerror = () => reject(reader.error);
      reader.readAsDataURL(file);
    });

  const attachImageFiles = async (files: File[]) => {
    const images = files.filter((f) => f.type.startsWith("image/"));
    if (images.length === 0) return;
    for (const file of images) {
      setPendingPaste((n) => n + 1);
      try {
        const key = await fileDedupKey(file);
        if (attachmentDedupRef.current.seen(key.hash, key.source)) continue;
        const dataUrl = await readFileAsDataURL(file);
        const path = await app.SavePastedImage(dataUrl);
        const previewUrl = await app.AttachmentDataURL(path);
        rememberAttachment(path, key);
        setAttachments((prev) => [...prev, { path, previewUrl, displayName: file.name }]);
      } catch (error) {
        console.warn("[composer] failed to attach pasted image", error);
        showToast(t("composer.attachImageFailed"), "warn");
        // non-fatal: a failed image attach must not block normal text input
      } finally {
        setPendingPaste((n) => Math.max(0, n - 1));
      }
    }
  };

  // Non-image pastes (PDFs, docs): the clipboard hands us bytes, not a path, so
  // the kernel stores them and we reference the saved path — attached, not ignored.
  const attachOtherFiles = async (files: File[]) => {
    const others = files.filter((f) => !f.type.startsWith("image/"));
    if (others.length === 0) return;
    for (const file of others) {
      setPendingPaste((n) => n + 1);
      try {
        const key = await fileDedupKey(file);
        if (attachmentDedupRef.current.seen(key.hash, key.source)) continue;
        const dataUrl = await readFileAsDataURL(file);
        const path = await app.SavePastedFile(file.name, dataUrl);
        rememberAttachment(path, key);
        setAttachments((prev) => [...prev, { path, displayName: file.name }]);
      } catch {
        // non-fatal: a failed attach must not block normal text input
      } finally {
        setPendingPaste((n) => Math.max(0, n - 1));
      }
    }
  };

  const attachFiles = (files: File[]) => {
    void attachImageFiles(files);
    void attachOtherFiles(files);
  };

  const attachNativeClipboardImage = async (notifyOnError: boolean) => {
    setPendingPaste((n) => n + 1);
    try {
      const path = await app.SaveClipboardImage();
      const previewUrl = await app.AttachmentDataURL(path);
      const key = { hash: await dataURLHash(previewUrl), source: `native-clipboard:${path}` };
      if (attachmentDedupRef.current.seen(key.hash, key.source)) return;
      rememberAttachment(path, key);
      setAttachments((prev) => [...prev, { path, previewUrl }]);
    } catch (error) {
      console.warn("[composer] failed to read native clipboard image", error);
      if (notifyOnError) showToast(t("composer.pasteImageFailed"), "warn");
    } finally {
      setPendingPaste((n) => Math.max(0, n - 1));
    }
  };

  // OS file drops arrive as absolute paths through the native bridge (the webview
  // withholds them from the HTML drop event); the kernel resolves each into a
  // workspace @reference or a stored attachment.
  const attachDroppedPaths = async (paths: string[]) => {
    setDragOver(false);
    for (const path of paths) {
      setPendingPaste((n) => n + 1);
      try {
        const key = { hash: "", source: `path:${path}` };
        if (attachmentDedupRef.current.seen(key.hash, key.source)) continue;
        const item = await app.AttachDropped(path);
        if (item.kind === "workspace") {
          addWorkspaceReference({ path: item.path, isDir: item.isDir });
        } else {
          rememberAttachment(item.path, key);
          setAttachments((prev) => [...prev, { path: item.path, previewUrl: item.previewUrl, displayName: baseName(path) }]);
        }
      } catch {
        // non-fatal: a failed drop attach must not block normal text input
      } finally {
        setPendingPaste((n) => Math.max(0, n - 1));
      }
    }
  };

  useEffect(() => {
    return onFilesDropped((paths) => void attachDroppedPaths(paths));
  }, []);

  const onPaste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
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
      void attachNativeClipboardImage(hasImageHint);
      return;
    }
    if (!shouldFoldPaste(pasted)) return;

    e.preventDefault();
    const ta = e.currentTarget;
    const start = ta.selectionStart ?? text.length;
    const end = ta.selectionEnd ?? text.length;
    const id = nextPasteId.current++;
    const lines = lineCount(pasted);
    const label = t("composer.pastedLabel", { id, lines });
    const block: PastedBlock = { label, text: pasted };
    const next = text.slice(0, start) + label + text.slice(end);

    pastedBlocksRef.current = [...pastedBlocksRef.current, block];
    setPastedBlocks((prev) => [...prev, block]);
    setText(next);
    requestAnimationFrame(() => {
      const node = taRef.current;
      if (!node) return;
      const pos = start + label.length;
      node.focus();
      node.selectionStart = node.selectionEnd = pos;
    });
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
    if (hasWorkspaceReferenceDrag(e.dataTransfer) || !hasFileDrag(e.dataTransfer) || !hasPathlessFileDrop(e.dataTransfer)) return;
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
    // (onFilesDropped → AttachDropped) handles them. Just clear the hover state.
    if (hasFileDrag(e.dataTransfer)) setDragOver(false);
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
    if (typeof restored === "string") setTextCaretEnd(restored);
  };

  const pickCommand = (c: CommandInfo) => setTextCaretEnd("/" + c.name + " ");

  const activePastedBlocks = pastedBlocks.filter((block) => text.includes(block.label));
  const shellModeActive = text.trimStart().startsWith("!");

  const removeWorkspaceReference = (target: WorkspaceReference) => {
    const key = workspaceReferenceKey(target);
    setWorkspaceRefs((prev) => prev.filter((ref) => workspaceReferenceKey(ref) !== key));
    requestAnimationFrame(() => taRef.current?.focus());
  };

  const togglePastedPreview = (label: string) => {
    setOpenPastedLabels((prev) => (prev.includes(label) ? prev.filter((x) => x !== label) : [...prev, label]));
  };

  const removePastedBlock = (block: PastedBlock) => {
    const next = text.split(block.label).join("");
    pastedBlocksRef.current = pastedBlocksRef.current.filter((x) => x.label !== block.label);
    setPastedBlocks((prev) => prev.filter((x) => x.label !== block.label));
    setOpenPastedLabels((prev) => prev.filter((x) => x !== block.label));
    setTextCaretEnd(next);
  };

  const expandPastedBlock = (block: PastedBlock) => {
    const next = text.split(block.label).join(block.text);
    pastedBlocksRef.current = pastedBlocksRef.current.filter((x) => x.label !== block.label);
    setPastedBlocks((prev) => prev.filter((x) => x.label !== block.label));
    setOpenPastedLabels((prev) => prev.filter((x) => x !== block.label));
    setTextCaretEnd(next);
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
    const node = taRef.current;
    if (!node) return;
    const previousHeight = node.style.height;
    node.style.height = "auto";
    const maxHeight = composerAutoInputMaxHeight();
    const nextHeight = Math.min(node.scrollHeight, maxHeight);
    const nextOverflow = node.scrollHeight > maxHeight + 1;
    node.style.height = previousHeight;
    setTextareaAutoHeight((current) => (current === nextHeight ? current : nextHeight));
    setTextareaAutoOverflow((current) => (current === nextOverflow ? current : nextOverflow));
  }, [composerHeight]);

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
    const startHeight = composerHeight ?? card.getBoundingClientRect().height;
    let nextHeight = clampComposerHeight(startHeight);
    let moved = false;
    setComposerResizing(true);
    document.body.classList.add("composer-resizing");

    const onMove = (event: PointerEvent) => {
      moved = true;
      nextHeight = clampComposerHeight(startHeight + startY - event.clientY);
      setComposerHeight(nextHeight);
    };
    const onUp = () => {
      setComposerResizing(false);
      document.body.classList.remove("composer-resizing");
      if (moved) saveComposerHeight(nextHeight);
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
    const current = composerHeight ?? card?.getBoundingClientRect().height ?? COMPOSER_MIN_HEIGHT;
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
    const atPos = text.length - (atRaw?.length ?? 0) - 1; // index of '@'
    const prefix = text.slice(0, atPos);
    // A directory keeps the menu open (trailing "/"); a file completes it (space).
    setTextCaretEnd(prefix + "@" + atDir + e.name + (e.isDir ? "/" : " "));
  };

  // --- past:chats session reference ---
  const openPastChats = async () => {
    const snapshotCwd = cwdRef.current;
    setShowPastChats(true);
    setActive(0);
    setPastChatQuery("");
    setLoadingPastChats(true);
    try {
      const sessions = await app.ListSessions();
      // Discard stale response if workspace changed while the request was in-flight.
      if (cwdRef.current !== snapshotCwd) return;
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
      if (cwdRef.current !== snapshotCwd) return;
      setPastChats([]);
    } finally {
      if (cwdRef.current === snapshotCwd) setLoadingPastChats(false);
    }
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
  const count = menuMode === "at" && showPastChats
    ? filteredPastChats.length
    : countBase;

  // Clamp active index when the menu item count changes (e.g. switching
  // between file list and past:chats list, or filtering sessions).
  useEffect(() => {
    const maxIdx = Math.max(0, count - 1);
    setActive((prev) => (prev > maxIdx ? 0 : prev));
  }, [count]);


  const removeAtToken = (value: string) => {
    return value.replace(/(?:^|\s)@[^\s]*$/, "").trimEnd();
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
    setText((prev) => removeAtToken(prev));
    setPastChatQuery("");
    setShowPastChats(false);
    setActive(0);
    // Return focus to textarea so the user can keep typing immediately.
    requestAnimationFrame(() => {
      taRef.current?.focus();
      taRef.current?.setSelectionRange(
        taRef.current.value.length,
        taRef.current.value.length,
      );
    });
  };

  const removeSessionRef = (path: string) => {
    setSessionRefs((prev) => prev.filter((ref) => ref.path !== path));
  };

  // pickArg replaces just the current token with the suggestion. A "descend" item
  // (e.g. "/skill show ") ends with a space, so the effect re-fetches the next
  // level; a terminal item leaves the menu (next fetch returns nothing).
  const pickArg = (it: SlashArgItem) => {
    if (!argRes) return;
    setTextCaretEnd(text.slice(0, argRes.from) + it.insert);
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
    if (menuMode === "at") {
      if (showPastChats) {
        const session = filteredPastChats[active];
        if (session) pickSession(session);
        return;
      }
      const item = atMenuItems[active];
      if (!item) return;
      if (item.kind === "pastChats") {
        void openPastChats();
        return;
      }
      pickEntry(item.entry);
    }
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
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
      nativeClipboardPasteTimerRef.current = window.setTimeout(() => {
        nativeClipboardPasteTimerRef.current = null;
        void attachNativeClipboardImage(false);
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

    const canUseCurrentPromptHistory = () => canUsePromptHistory({
      direction: historyDirection,
      menuOpen: Boolean(menuMode),
      composing,
      altKey: e.altKey,
      ctrlKey: e.ctrlKey,
      metaKey: e.metaKey,
      shiftKey: e.shiftKey,
      fnKey,
      value: e.currentTarget.value,
      selectionStart: e.currentTarget.selectionStart,
      selectionEnd: e.currentTarget.selectionEnd,
      historyIndex: historyIndexRef.current,
    });

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
      void (async () => {
        // Use refs for all mutable reads inside the async closure so rapid
        // successive key presses always see the latest values.
        if (historyIndexRef.current === -1) {
          savedTextRef.current = text; // save current draft
        }
        const target =
          historyDirection === "up"
            ? historyIndexRef.current + 1
            : historyDirection === "down"
              ? historyIndexRef.current - 1
              : historyIndexRef.current;
        if (target >= historyEntriesRef.current.length && !(await ensurePromptHistoryIndex(target))) {
          return;
        }
        const next =
          historyDirection === "up"
            ? Math.min(target, historyEntriesRef.current.length - 1)
            : historyDirection === "down"
              ? Math.max(target, -1)
              : historyIndexRef.current;
        historyIndexRef.current = next;
        setHistoryIndex(next);
        if (next === -1) {
          setTextCaretEnd(savedTextRef.current);
        } else {
          setTextCaretEnd(historyEntriesRef.current[next].text);
          if (historyDirection === "up" && historyEntriesRef.current.length - 1 - next <= PROMPT_HISTORY_PREFETCH_REMAINING) {
            prefetchPromptHistoryTail();
          }
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
        if (showPastChats) {
          setPastChatQuery("");
          setShowPastChats(false);
          setActive(0);
        } else {
          setDismissed(true);
        }
        return;
      }
    }

    // Enter sends; Shift+Enter newline. `composing` guards IME confirms.
    if (e.key === "Enter" && !e.shiftKey && !composing) {
      e.preventDefault();
      submit();
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
        setPastChatQuery("");
        setShowPastChats(false);
        setActive(0);
      }
    }
  };

  const composerCardStyle = composerHeight === null ? undefined : ({ "--composer-height": `${composerHeight}px` } as CSSProperties);
  const textareaStyle = composerHeight === null && textareaAutoHeight !== null
    ? ({ height: `${textareaAutoHeight}px`, overflowY: textareaAutoOverflow ? "auto" : "hidden" } as CSSProperties)
    : undefined;
  const composerAutoExpanded = composerHeight === null && textareaAutoHeight !== null && textareaAutoHeight > 40;
  void onSetMode;
  const chooseApprovalMode = (nextMode: ToolApprovalMode) => {
    onSetToolApprovalMode(nextMode);
    requestAnimationFrame(() => taRef.current?.focus());
  };
  const choosePlanMode = () => {
    closeIntentMenu(() => {
      onSetCollaborationMode(planModeOn ? "normal" : "plan");
      requestAnimationFrame(() => taRef.current?.focus());
    });
  };
  const chooseGoalMode = () => {
    if (goalModeOn) {
      closeIntentMenu(() => {
        onClearGoal();
        requestAnimationFrame(() => taRef.current?.focus());
      });
      return;
    }
    closeIntentMenu(() => {
      onSetCollaborationMode("goal");
      requestAnimationFrame(() => taRef.current?.focus());
    });
  };
  const chooseTokenMode = () => {
    closeIntentMenu(() => {
      onSetTokenMode(tokenModeOn ? "full" : "economy");
      requestAnimationFrame(() => taRef.current?.focus());
    });
  };
  const effortLevels = asArray(effort?.levels);
  const currentEffort = effort?.current || "auto";
  const hasEffort = Boolean(effort?.supported && effortLevels.length > 0);
  const chooseEffortLevel = (level: string) => {
    closeMoreMenu(() => {
      if (level !== currentEffort) onSetEffort(level);
      requestAnimationFrame(() => taRef.current?.focus());
    });
  };
  const runActivity = retry
    ? t("status.retrying", { attempt: retry.attempt, max: retry.max })
    : running && turnStartAt
      ? (() => {
          const elapsedMs = Math.max(0, now - turnStartAt);
          const words = SPINNER_WORDS[locale];
          const word = words[Math.floor(elapsedMs / 3000) % words.length];
          const tok = turnTokens && turnTokens > 0 ? ` · ↓ ${fmtTokens(turnTokens)} ${t("status.tokens")}` : "";
          return `${word}… ${fmtElapsed(elapsedMs)}${tok}`;
        })()
      : null;
  const composerMetaClass = [
    "composer-meta",
    hasEffort ? "composer-meta--has-effort" : "composer-meta--no-effort",
    planModeOn || goalModeOn || tokenModeOn ? "composer-meta--has-intent-chip" : "composer-meta--no-intent-chip",
  ].join(" ");

  return (
    <div
      className={`composer-wrap${decisionPending ? " composer-wrap--decision-pending" : ""}`}
      style={{ "--wails-drop-target": "drop" } as CSSProperties}
      onDropCapture={onFileDropCapture}
    >
      <AnchoredPopover
        open={intentMenuOpen}
        closing={intentMenuClosing}
        anchorRef={intentMenuAnchorRef}
        onClose={() => closeIntentMenu()}
        className="composer-access-menu composer-intent-menu"
        align="start"
      >
        <div className="composer-access-menu__section">
          <div className="composer-access-menu__label">{t("composer.intentMenuTitle")}</div>
          <button
            type="button"
            className={`composer-access-menu__item composer-intent-menu__item${planModeOn ? " composer-access-menu__item--active" : ""}`}
            onClick={choosePlanMode}
            disabled={disabled || running}
            title={planModeOn ? t("composer.exitPlanTitle") : t("composer.enterPlanTitle")}
          >
            <List size={16} />
            <span className="composer-access-menu__copy">
              <span className="composer-access-menu__title">{t("composer.modePlan")}</span>
              <span className="composer-access-menu__desc">{t("composer.planModeDesc")}</span>
            </span>
            <span className={`composer-intent-switch${planModeOn ? " composer-intent-switch--on" : ""}`} aria-hidden="true">
              <span />
            </span>
          </button>
          <button
            type="button"
            className={`composer-access-menu__item composer-intent-menu__item${goalModeOn ? " composer-access-menu__item--active" : ""}`}
            onClick={chooseGoalMode}
            disabled={disabled || running}
            title={goalModeOn ? activeGoal || t("composer.goalModeActiveDesc") : t("composer.goalModeDesc")}
          >
            <Target size={16} />
            <span className="composer-access-menu__copy">
              <span className="composer-access-menu__title">{t("composer.modeGoal")}</span>
              <span className="composer-access-menu__desc">{goalModeOn ? activeGoal || t("composer.goalModeActiveDesc") : t("composer.goalModeDesc")}</span>
            </span>
            <span className={`composer-intent-switch${goalModeOn ? " composer-intent-switch--on" : ""}`} aria-hidden="true">
              <span />
            </span>
          </button>
          <button
            type="button"
            className={`composer-access-menu__item composer-intent-menu__item${tokenModeOn ? " composer-access-menu__item--active" : ""}`}
            onClick={chooseTokenMode}
            disabled={disabled || running}
            title={tokenModeOn ? t("composer.tokenEconomyOnDesc") : t("composer.tokenEconomyDesc")}
          >
            <Gauge size={16} />
            <span className="composer-access-menu__copy">
              <span className="composer-access-menu__title">{t("composer.tokenEconomy")}</span>
              <span className="composer-access-menu__desc">{tokenModeOn ? t("composer.tokenEconomyOnDesc") : t("composer.tokenEconomyDesc")}</span>
            </span>
            <span className={`composer-intent-switch${tokenModeOn ? " composer-intent-switch--on" : ""}`} aria-hidden="true">
              <span />
            </span>
          </button>
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
      {menuMode === "at" && (
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
                    autoFocus
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
                setPastChatQuery("");
                setShowPastChats(false);
                setActive(0);
              }}
            >
              <span className="slashmenu__name">← 返回文件列表</span>
            </button>
          </div>
        ) : (
          <VirtualMenu
            items={atMenuItems}
            activeIndex={active}
            itemKey={(it) => (it.kind === "pastChats" ? "past:chats" : (it.entry.isDir ? "d:" : "f:") + it.entry.name)}
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
                    {it.entry.name}
                    {it.entry.isDir ? "/" : ""}
                  </span>
                </button>
              )
            }
          />
        )
      )}
      {runActivity && (
        <div className="composer-toolbar composer-toolbar--status-only">
          <div className="composer-runstatus" role="status" aria-live="polite">
            <span className="composer-runstatus__dot" />
            <span className="composer-runstatus__text">{runActivity}</span>
            <Tooltip label={t("composer.stop")}>
              <button className="composer-runstatus__stop" type="button" onClick={handleCancel}>
                <Square size={10} fill="currentColor" />
                <span>{t("composer.stopShort")}</span>
              </button>
            </Tooltip>
          </div>
        </div>
      )}
      {(attachments.length > 0 || workspaceRefs.length > 0 || sessionRefs.length > 0) && (
        <div className="composer-context" aria-label={t("composer.contextItems")}>
          {sortComposerAttachments(attachments).map((a) => {
            const imageOnly = Boolean(a.previewUrl) && attachments.every((item) => item.previewUrl) && workspaceRefs.length === 0 && sessionRefs.length === 0;
            return (
              <ComposerContextCard
                key={a.path}
                variant="attachment"
                tooltipLabel={a.path}
                removeLabel={t("composer.removeImage")}
                onRemove={() => removeAttachment(a.path)}
                previewUrl={a.previewUrl}
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
              tooltipLabel={formatWorkspaceReference(ref.path, ref.isDir)}
              removeLabel={t("composer.removeReference")}
              onRemove={() => removeWorkspaceReference(ref)}
              folder={Boolean(ref.isDir)}
              label={ref.isDir ? `${baseName(ref.path)}/` : baseName(ref.path)}
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
        </div>
      )}
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
        className={`composer-card${composerHeight !== null ? " composer-card--resized" : ""}${composerAutoExpanded ? " composer-card--autosized" : ""}${composerResizing ? " composer-card--resizing" : ""}${running ? " composer-card--running" : ""}`}
        ref={composerCardRef}
        style={composerCardStyle}
      >
        <button
          className="composer-resize-handle"
          type="button"
          aria-label={t("composer.resize")}
          title={t("composer.resize")}
          onPointerDown={onComposerResizeStart}
          onKeyDown={onComposerResizeKeyDown}
          onDoubleClick={resetComposerHeight}
        />
        <div
          className={`composer${dragOver ? " composer--dragover" : ""}${disabled || readOnly ? " composer--disabled" : ""}${shellModeActive ? " composer--shell" : ""}`}
          onDrop={onDrop}
          onDragOver={onDragOver}
          onDragLeave={onDragLeave}
        >
          <span className="composer__caret">{shellModeActive ? "$" : "›"}</span>
          <textarea
            id="composer-input"
            ref={taRef}
            className="composer__input"
            aria-label={t("composer.placeholder")}
            value={text}
            onChange={(e) => {
              resetPromptHistoryNavigation();
              setText(e.target.value);
              if (composerPrompt) setComposerPrompt(null);
            }}
            onSelect={rememberCaret}
            onClick={rememberCaret}
            onKeyUp={rememberCaret}
            onFocus={rememberCaret}
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
            placeholder={readOnly ? t("composer.readOnlyChannel") : disabled ? t("common.loading") : goalModeOn && !activeGoal ? t("composer.goalInputPlaceholder") : t("composer.placeholder")}
            rows={1}
            disabled={disabled || readOnly}
          />
          {composerPrompt && (
            <span className="composer__prompt" role="status">
              {composerPrompt}
            </span>
          )}
          {!running && (
            <Tooltip label={t("composer.send")}>
              <button
                className="composer__btn composer__btn--send"
                onClick={submit}
                disabled={submitting || pendingPaste > 0 || ((!text.trim() && attachments.length === 0 && workspaceRefs.length === 0) && !(goalModeOn && !activeGoal)) || disabled || submitDisabled || readOnly}
              >
                <ArrowUp size={16} />
              </button>
            </Tooltip>
          )}
        </div>
        <div className={composerMetaClass}>
          <div className="composer-meta__params">
            <div className="composer-meta__control composer-meta__control--intent">
              <Tooltip label={t("composer.intentMenuTitle")} disabled={intentMenuOpen || intentMenuClosing}>
                <button
                  ref={intentMenuAnchorRef}
                  type="button"
                  className={`composer-action-trigger${intentMenuOpen || intentMenuClosing ? " composer-action-trigger--open" : ""}`}
                  onClick={() => (intentMenuOpen || intentMenuClosing ? closeIntentMenu() : openIntentMenu())}
                  disabled={disabled || running}
                  aria-haspopup="menu"
                  aria-expanded={intentMenuOpen && !intentMenuClosing}
                  aria-label={t("composer.intentMenuTitle")}
                  title={intentMenuOpen || intentMenuClosing ? undefined : t("composer.intentMenuTitle")}
                >
                  <SlidersHorizontal size={17} />
                </button>
              </Tooltip>
              {planModeOn && (
                <Tooltip label={t("composer.exitPlanTitle")}>
                  <button
                    type="button"
                    className="composer-mode-chip composer-mode-chip--plan"
                    onClick={choosePlanMode}
                    disabled={disabled}
                    title={t("composer.exitPlanTitle")}
                    aria-label={t("composer.exitPlanTitle")}
                  >
                    <span className="composer-mode-chip__icon composer-mode-chip__icon--mode" aria-hidden="true">
                      <List size={14} />
                    </span>
                    <span className="composer-mode-chip__icon composer-mode-chip__icon--dismiss" aria-hidden="true">
                      <X size={11} />
                    </span>
                    <span className="composer-mode-chip__label">{t("composer.modePlan")}</span>
                  </button>
                </Tooltip>
              )}
              {goalModeOn && (
                <Tooltip label={t("composer.exitGoalTitle")}>
                  <button
                    type="button"
                    className="composer-mode-chip composer-mode-chip--goal"
                    onClick={chooseGoalMode}
                    disabled={disabled}
                    title={activeGoal || t("composer.exitGoalTitle")}
                    aria-label={t("composer.exitGoalTitle")}
                  >
                    <span className="composer-mode-chip__icon composer-mode-chip__icon--mode" aria-hidden="true">
                      <Target size={14} />
                    </span>
                    <span className="composer-mode-chip__icon composer-mode-chip__icon--dismiss" aria-hidden="true">
                      <X size={11} />
                    </span>
                    <span className="composer-mode-chip__label">{t("composer.modeGoal")}</span>
                  </button>
                </Tooltip>
              )}
              {tokenModeOn && (
                <Tooltip label={t("composer.tokenEconomyOnDesc")}>
                  <button
                    type="button"
                    className="composer-mode-chip composer-mode-chip--token"
                    onClick={chooseTokenMode}
                    disabled={disabled || running}
                    title={t("composer.tokenEconomyExitTitle")}
                    aria-label={t("composer.tokenEconomyExitTitle")}
                  >
                    <span className="composer-mode-chip__icon composer-mode-chip__icon--mode" aria-hidden="true">
                      <Gauge size={14} />
                    </span>
                    <span className="composer-mode-chip__icon composer-mode-chip__icon--dismiss" aria-hidden="true">
                      <X size={11} />
                    </span>
                    <span className="composer-mode-chip__label">{t("composer.tokenEconomyShort")}</span>
                  </button>
                </Tooltip>
              )}
            </div>
            <div className="composer-meta__control composer-meta__control--approval">
              <div className="composer-modebar composer-modebar--approval" data-mode={toolApprovalMode} title={t("composer.accessMenuTitle")}>
                <span className="composer-modebar__thumb" aria-hidden="true" />
                <button
                  type="button"
                  className={`composer-modebar__item composer-modebar__item--ask${toolApprovalMode === "ask" ? " composer-modebar__item--active" : ""}`}
                  onClick={() => chooseApprovalMode("ask")}
                  disabled={disabled}
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
                  disabled={disabled}
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
                  disabled={disabled}
                  aria-pressed={toolApprovalMode === "yolo"}
                  title={t("composer.accessYoloTitle")}
                >
                  <ShieldAlert size={14} />
                  <span>{t("composer.modeYolo")}</span>
                </button>
              </div>
            </div>
            <div className="composer-meta__control composer-meta__control--model">
              <ModelSwitcher label={modelLabel} tabId={tabId} onPick={onSwitchModel} />
            </div>
            {hasEffort && (
              <div className="composer-meta__control composer-meta__control--effort">
                <EffortSwitcher effort={effort} disabled={running} onPick={onSetEffort} />
              </div>
            )}
            {hasEffort && (
              <div className="composer-meta__control composer-meta__control--more">
                <Tooltip label={t("composer.moreControls")} disabled={moreMenuOpen || moreMenuClosing}>
                  <button
                    ref={moreMenuAnchorRef}
                    type="button"
                    className={`composer-more-trigger${moreMenuOpen || moreMenuClosing ? " composer-more-trigger--open" : ""}`}
                    onClick={() => (moreMenuOpen || moreMenuClosing ? closeMoreMenu() : openMoreMenu())}
                    disabled={disabled || running}
                    aria-haspopup="menu"
                    aria-expanded={moreMenuOpen && !moreMenuClosing}
                    aria-label={t("composer.moreControls")}
                    title={moreMenuOpen || moreMenuClosing ? undefined : t("composer.moreControls")}
                  >
                    <MoreHorizontal size={16} />
                    <span>{t("topicBar.more")}</span>
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
