import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, ClipboardEvent, DragEvent, KeyboardEvent, PointerEvent as ReactPointerEvent, ReactNode } from "react";
import { AlertTriangle, ArrowUp, Check, ChevronDown, Eye, FileText, Folder, FolderGit2, FolderPlus, List, Search, Square, Trash2, X, Zap } from "lucide-react";
import { asArray } from "../lib/array";
import { app, onFilesDropped } from "../lib/bridge";
import { SPINNER_WORDS, useI18n } from "../lib/i18n";
import { clearLayoutSize, loadOptionalLayoutSize, saveLayoutSize } from "../lib/layoutPreferences";
import type { CommandInfo, ComposerInsertRequest, DirEntry, EffortInfo, Mode, SlashArgItem, SlashArgsResult, WorkspaceView } from "../lib/types";
import {
  formatWorkspaceReference,
  parseWorkspaceReference,
  readWorkspaceReferenceDrag,
  WORKSPACE_REF_DRAG_TYPE,
} from "../lib/workspaceDrag";
import { SlashMenu } from "./SlashMenu";
import { ArgMenu } from "./ArgMenu";
import { FileMenu } from "./FileMenu";
import { EffortSwitcher } from "./EffortSwitcher";
import { ModelSwitcher } from "./ModelSwitcher";
import { Tooltip } from "./Tooltip";
import { AnchoredPopover } from "./AnchoredPopover";

interface Attachment {
  path: string;
  previewUrl?: string;
}

interface WorkspaceReference {
  path: string;
  isDir?: boolean;
}

const LONG_PASTE_MIN_CHARS = 2000;
const LONG_PASTE_MIN_LINES = 20;
const COMPOSER_MIN_HEIGHT = 86;
const COMPOSER_MAX_HEIGHT = 360;
const COMPOSER_MAX_VIEWPORT_RATIO = 0.4;
const COMPOSER_AUTO_RESERVED_HEIGHT = 58;
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
  const clean = path.replace(/\/$/, "");
  return clean.split("/").filter(Boolean).pop() ?? path;
}

function workspaceReferenceKey(ref: WorkspaceReference): string {
  return `${ref.isDir ? "dir" : "file"}:${ref.path}`;
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

export function Composer({
  running,
  mode,
  cwd,
  modelLabel,
  tabId,
  effort,
  onSend,
  onCancel,
  onCycleMode,
  onSetMode,
  onSwitchModel,
  onSetEffort,
  onPickFolder,
  onRemoveWorkspace,
  insertRequest,
  disabled,
  decisionPending = false,
  ready,
  turnStartAt,
  turnTokens,
  retry,
  workspaceRefreshSignal,
}: {
  running: boolean;
  mode: Mode;
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
  onSwitchModel: (name: string) => void;
  onSetEffort: (level: string) => void;
  onPickFolder: (path?: string) => Promise<string>;
  onRemoveWorkspace: (path: string) => Promise<void>;
  insertRequest?: ComposerInsertRequest | null;
  disabled?: boolean;
  decisionPending?: boolean;
  // ready/cwd re-trigger the command fetch: Commands() returns only built-ins
  // until boot.Build finishes (the controller, hence skills/custom/MCP, is nil
  // before then), and the available set changes when the workspace switches.
  ready?: boolean;
  turnStartAt?: number;
  turnTokens?: number;
  retry?: { attempt: number; max: number };
  workspaceRefreshSignal?: number;
}) {
  const { t, locale } = useI18n();
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
  const [workspaceMenuOpen, setWorkspaceMenuOpen] = useState(false);
  const [workspaceQuery, setWorkspaceQuery] = useState("");
  const [workspaces, setWorkspaces] = useState<WorkspaceView[]>([]);
  // Two-click delete: the first click on the trash icon moves the row into a
  // "Confirm?" state and shows a real label ("Delete?") on the icon; the
  // second click (within ~3s) actually fires the removal. A click anywhere
  // else, Escape, or a workspace switch resets the row. We keep the existing
  // server-side RemoveWorkspace as the actual delete so the projects file
  // stays the single source of truth — this is purely a confirmation gate.
  const [confirmRemovePath, setConfirmRemovePath] = useState<string | null>(null);
  const [composerHeight, setComposerHeight] = useState<number | null>(loadComposerHeight);
  const [composerResizing, setComposerResizing] = useState(false);
  const [textareaAutoHeight, setTextareaAutoHeight] = useState<number | null>(null);
  const [textareaAutoOverflow, setTextareaAutoOverflow] = useState(false);
  const taRef = useRef<HTMLTextAreaElement>(null);
  const composerCardRef = useRef<HTMLDivElement>(null);
  const workspaceAnchorRef = useRef<HTMLDivElement>(null);
  const wasRunning = useRef(running);
  const composingRef = useRef(false);
  const lastCompositionEndAt = useRef(0);
  const lastSelectionRef = useRef({ start: 0, end: 0 });
  const consumedInsertIdRef = useRef(0);

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
  }, [ready, cwd]);

  const slashQuery = useMemo(() => {
    if (!text.startsWith("/") || /\s/.test(text)) return null;
    return text.slice(1).toLowerCase();
  }, [text]);
  const slashMatches = useMemo(
    () => (slashQuery === null ? [] : commands.filter((c) => c.name.toLowerCase().includes(slashQuery)).slice(0, 8)),
    [slashQuery, commands],
  );

  // --- slash argument completion ("/cmd <args>") --- mirrors the CLI: once past
  // the command word, the backend suggests sub-commands (/skill → list/show/…,
  // /mcp → add/remove, /model → refs). Fetched from app.SlashArgs. Debounced
  // by 120ms so rapid typing doesn't flood the backend with IPC calls — the
  // menu only updates after the user pauses.
  const [argRes, setArgRes] = useState<SlashArgsResult | null>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout>>();
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
    // re-fetch only when the menu opens or the directory level changes
  }, [atRaw === null, atDir]);
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
  }, [atRaw === null, atDir, atFrag]);
  const atMatches = useMemo(
    () => {
      if (atRaw === null) return [];
      const local = entries.filter((e) => e.name.toLowerCase().includes(atFrag));
      const seen = new Set(local.map((e) => e.name));
      const searched = searchEntries.filter((e) => {
        const basename = e.name.split("/").pop()?.toLowerCase() ?? "";
        return basename.includes(atFrag) && !seen.has(e.name);
      });
      return [...local, ...searched].slice(0, 10);
    },
    [atRaw, atFrag, entries, searchEntries],
  );

  // --- which menu (if any) is open --- (slash command names win; then slash
  // arguments; then @-refs — they're rarely valid at once)
  const menuMode: "slash" | "slasharg" | "at" | null =
    slashMatches.length > 0 && !dismissed
      ? "slash"
      : argRes && argRes.items.length > 0 && !dismissed
        ? "slasharg"
        : atMatches.length > 0 && !dismissed
          ? "at"
          : null;
  const count =
    menuMode === "slash"
      ? slashMatches.length
      : menuMode === "slasharg"
        ? argRes!.items.length
        : menuMode === "at"
          ? atMatches.length
          : 0;

  // Reset highlight + un-dismiss whenever the active query changes.
  useEffect(() => {
    setActive(0);
    setDismissed(false);
  }, [slashQuery, atRaw]);

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

  const submit = () => {
    if (disabled) return;
    const t = text.trim();
    if ((!t && attachments.length === 0 && workspaceRefs.length === 0) || pendingPaste > 0) return;
    const refs = [
      ...workspaceRefs.map((ref) => formatWorkspaceReference(ref.path, ref.isDir)),
      ...attachments.map((a) => `@${a.path}`),
    ].join(" ");
    const displayText = [t, refs].filter(Boolean).join(t && refs ? " " : "");
    const submitText = [expandPastedBlocks(t), refs].filter(Boolean).join(t && refs ? " " : "");
    onSend(displayText, submitText);
    setText("");
    setAttachments([]);
    setWorkspaceRefs([]);
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
        const dataUrl = await readFileAsDataURL(file);
        const path = await app.SavePastedImage(dataUrl);
        const previewUrl = await app.AttachmentDataURL(path);
        setAttachments((prev) => [...prev, { path, previewUrl }]);
      } catch {
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
        const dataUrl = await readFileAsDataURL(file);
        const path = await app.SavePastedFile(file.name, dataUrl);
        setAttachments((prev) => [...prev, { path }]);
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

  // OS file drops arrive as absolute paths through the native bridge (the webview
  // withholds them from the HTML drop event); the kernel resolves each into a
  // workspace @reference or a stored attachment.
  const attachDroppedPaths = async (paths: string[]) => {
    setDragOver(false);
    for (const path of paths) {
      setPendingPaste((n) => n + 1);
      try {
        const item = await app.AttachDropped(path);
        if (item.kind === "workspace") {
          addWorkspaceReference({ path: item.path, isDir: item.isDir });
        } else {
          setAttachments((prev) => [...prev, { path: item.path, previewUrl: item.previewUrl }]);
        }
      } catch {
        // non-fatal: a failed drop attach must not block normal text input
      } finally {
        setPendingPaste((n) => Math.max(0, n - 1));
      }
    }
  };

  useEffect(() => onFilesDropped((paths) => void attachDroppedPaths(paths)), []);

  const onPaste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
    const files = Array.from(e.clipboardData.files);
    if (files.length > 0) {
      e.preventDefault();
      attachFiles(files);
      return;
    }

    const pasted = e.clipboardData.getData("text");
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

  const workspaceName = useMemo(() => {
    if (!cwd) return "";
    const parts = cwd.split(/[/\\]/).filter(Boolean);
    return parts.length > 0 ? parts[parts.length - 1] : cwd;
  }, [cwd]);

  const loadWorkspaces = () => {
    app.ListWorkspaces().then((next) => setWorkspaces(asArray(next))).catch(() => setWorkspaces([]));
  };

  useEffect(() => {
    if (workspaceMenuOpen) loadWorkspaces();
  }, [workspaceMenuOpen, cwd, workspaceRefreshSignal]);

  const filteredWorkspaces = useMemo(() => {
    const q = workspaceQuery.trim().toLowerCase();
    if (!q) return workspaces;
    return workspaces.filter((w) => `${w.name} ${w.path}`.toLowerCase().includes(q));
  }, [workspaceQuery, workspaces]);

  const chooseWorkspace = async (path?: string) => {
    const next = await onPickFolder(path);
    if (next) {
      setWorkspaceMenuOpen(false);
      setWorkspaceQuery("");
    }
  };

  const removeWorkspace = async (path: string) => {
    await onRemoveWorkspace(path);
    setWorkspaces((prev) => prev.filter((w) => w.path !== path));
    setConfirmRemovePath(null);
  };

  // First click on the trash icon arms the confirmation; second click fires.
  // We reset the armed state after a short idle window so the user doesn't
  // accidentally delete a workspace they walked past 30s ago.
  useEffect(() => {
    if (!confirmRemovePath) return;
    const id = window.setTimeout(() => setConfirmRemovePath(null), 3000);
    return () => window.clearTimeout(id);
  }, [confirmRemovePath]);

  // Escape / menu close / workspace switch all clear the armed delete.
  useEffect(() => {
    if (!workspaceMenuOpen) setConfirmRemovePath(null);
  }, [workspaceMenuOpen]);

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
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-text-size"] });
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

  // pickArg replaces just the current token with the suggestion. A "descend" item
  // (e.g. "/skill show ") ends with a space, so the effect re-fetches the next
  // level; a terminal item leaves the menu (next fetch returns nothing).
  const pickArg = (it: SlashArgItem) => {
    if (!argRes) return;
    setTextCaretEnd(text.slice(0, argRes.from) + it.insert);
  };

  const pickActive = () => {
    if (menuMode === "slash") pickCommand(slashMatches[active]);
    else if (menuMode === "slasharg" && argRes) pickArg(argRes.items[active]);
    else if (menuMode === "at") pickEntry(atMatches[active]);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    const composing = isImeKeyEvent(e, composingRef.current, lastCompositionEndAt.current);
    if (e.key === "Enter" && composing) return;

    // Shift+Tab cycles the input mode (normal → plan → YOLO → normal). Handled
    // before the menus so it works even while one is open.
    if (e.key === "Tab" && e.shiftKey && !composing) {
      e.preventDefault();
      onCycleMode();
      return;
    }

    if (menuMode && !composing) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setActive((i) => (i + 1) % count);
        return;
      }
      if (e.key === "ArrowUp") {
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
        setDismissed(true);
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
    if (e.key === "Escape" && running && !decisionPending) {
      e.preventDefault();
      handleCancel();
    }
  };

  const composerCardStyle = composerHeight === null ? undefined : ({ "--composer-height": `${composerHeight}px` } as CSSProperties);
  const textareaStyle = composerHeight === null && textareaAutoHeight !== null
    ? ({ height: `${textareaAutoHeight}px`, overflowY: textareaAutoOverflow ? "auto" : "hidden" } as CSSProperties)
    : undefined;
  const composerAutoExpanded = composerHeight === null && textareaAutoHeight !== null && textareaAutoHeight > 40;
  const modeOptions: Array<{ id: Mode; label: string; icon: ReactNode }> = [
    { id: "normal", label: "auto", icon: <Zap size={13} /> },
    { id: "plan", label: "plan", icon: <List size={13} /> },
    { id: "yolo", label: "yolo", icon: <AlertTriangle size={13} /> },
  ];
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
  const hasWorkspace = Boolean(cwd);
  const hasEffort = Boolean(effort?.supported);
  const composerMetaClass = [
    "composer-meta",
    hasWorkspace ? "composer-meta--has-workspace" : "composer-meta--no-workspace",
    hasEffort ? "composer-meta--has-effort" : "composer-meta--no-effort",
  ].join(" ");

  return (
    <div
      className={`composer-wrap${decisionPending ? " composer-wrap--decision-pending" : ""}`}
      style={{ "--wails-drop-target": "drop" } as CSSProperties}
      onDropCapture={onFileDropCapture}
    >
      <AnchoredPopover
        open={workspaceMenuOpen && !!cwd}
        anchorRef={workspaceAnchorRef}
        onClose={() => setWorkspaceMenuOpen(false)}
        className="workspace-switcher workspace-switcher--portal"
      >
          <label className="workspace-switcher__search">
            <Search size={14} />
            <input
              autoFocus
              value={workspaceQuery}
              onChange={(e) => setWorkspaceQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Escape") setWorkspaceMenuOpen(false);
              }}
              placeholder={t("composer.searchProjects")}
            />
          </label>
          <div className="workspace-switcher__list">
            {filteredWorkspaces.map((w) => (
              <div className="workspace-switcher__row" key={w.path}>
                <button
                  className={`workspace-switcher__item${w.current ? " workspace-switcher__item--current" : ""}`}
                  title={w.path}
                  onClick={() => {
                    if (w.current) {
                      setWorkspaceMenuOpen(false);
                      return;
                    }
                    void chooseWorkspace(w.path);
                  }}
                >
                  <FolderGit2 size={15} />
                  <span>{w.name}</span>
                  {w.current && <Check size={15} />}
                </button>
                <button
                  className={`workspace-switcher__remove${confirmRemovePath === w.path ? " workspace-switcher__remove--armed" : ""}${w.current ? " workspace-switcher__remove--current" : ""}`}
                  type="button"
                  aria-label={confirmRemovePath === w.path ? t("composer.confirmRemoveProject") : t("composer.removeProject")}
                  title={
                    w.current
                      ? t("composer.cannotRemoveCurrent")
                      : confirmRemovePath === w.path
                        ? t("composer.confirmRemoveProject")
                        : t("composer.removeProject")
                  }
                  disabled={running || w.current}
                  onClick={(event) => {
                    event.stopPropagation();
                    if (w.current) return;
                    if (confirmRemovePath === w.path) {
                      void removeWorkspace(w.path);
                    } else {
                      setConfirmRemovePath(w.path);
                    }
                  }}
                >
                  {confirmRemovePath === w.path ? <Check size={14} /> : <Trash2 size={14} />}
                </button>
              </div>
            ))}
            {filteredWorkspaces.length === 0 && <div className="workspace-switcher__empty">{t("composer.noProjectMatches")}</div>}
          </div>
          <div className="workspace-switcher__actions">
            <button type="button" onClick={() => void chooseWorkspace()}>
              <FolderPlus size={15} />
              <span>{t("composer.addProject")}</span>
            </button>
          </div>
      </AnchoredPopover>
      {menuMode === "slash" && (
        <SlashMenu items={slashMatches} activeIndex={active} onPick={pickCommand} onHover={setActive} />
      )}
      {menuMode === "slasharg" && argRes && (
        <ArgMenu items={argRes.items} activeIndex={active} onPick={pickArg} onHover={setActive} />
      )}
      {menuMode === "at" && <FileMenu items={atMatches} activeIndex={active} onPick={pickEntry} onHover={setActive} />}
      <div className="composer-toolbar">
        <div className="composer-modebar" role="toolbar" aria-label={t("composer.modeTitle")}>
          {modeOptions.map((option) => (
            <button
              key={option.id}
              type="button"
              className={`composer-modebar__item composer-modebar__item--${option.id}${mode === option.id ? " composer-modebar__item--active" : ""}`}
              onClick={() => onSetMode(option.id)}
              aria-pressed={mode === option.id}
              disabled={disabled || running}
            >
              {option.icon}
              <span>{option.label}</span>
            </button>
          ))}
        </div>
        {runActivity && (
          <div className="composer-runstatus" role="status" aria-live="polite">
            <span className="composer-runstatus__dot" />
            <span className="composer-runstatus__text">{runActivity}</span>
            <Tooltip label={t("composer.stop")}>
              <button className="composer-runstatus__stop" type="button" onClick={handleCancel} disabled={decisionPending}>
                <Square size={10} fill="currentColor" />
                <span>{t("composer.stopShort")}</span>
              </button>
            </Tooltip>
          </div>
        )}
      </div>
      {(attachments.length > 0 || workspaceRefs.length > 0) && (
        <div className="composer-context" aria-label={t("composer.contextItems")}>
          {attachments.map((a) => (
            <div
              className={`composer-context__item${a.previewUrl ? " composer-context__item--image" : " composer-context__item--attachment"}`}
              key={a.path}
            >
              <Tooltip label={a.path}>
                <span className="composer-context__label">
                  {a.previewUrl ? <img src={a.previewUrl} alt="" /> : <FileText size={15} />}
                  <span>{a.path.split("/").pop()}</span>
                </span>
              </Tooltip>
              <Tooltip label={t("composer.removeImage")}>
                <button
                  type="button"
                  onClick={() => setAttachments((prev) => prev.filter((x) => x.path !== a.path))}
                >
                  <X size={14} />
                </button>
              </Tooltip>
            </div>
          ))}
          {workspaceRefs.map((ref) => (
            <div
              className={`composer-context__item composer-context__item--workspace${ref.isDir ? " composer-context__item--folder" : " composer-context__item--file"}`}
              key={workspaceReferenceKey(ref)}
            >
              <Tooltip label={formatWorkspaceReference(ref.path, ref.isDir)}>
                <span className="composer-context__label">
                  {ref.isDir ? <Folder size={15} /> : <FileText size={15} />}
                  <span>{ref.isDir ? `${baseName(ref.path)}/` : baseName(ref.path)}</span>
                </span>
              </Tooltip>
              <Tooltip label={t("composer.removeReference")}>
                <button
                  type="button"
                  onClick={() => removeWorkspaceReference(ref)}
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
                  <span>{block.label}</span>
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
                {open && <pre className="composer__pasted-preview">{block.text}</pre>}
              </div>
            );
          })}
        </div>
      )}
      <div
        className={`composer-card${composerHeight !== null ? " composer-card--resized" : ""}${composerAutoExpanded ? " composer-card--autosized" : ""}${composerResizing ? " composer-card--resizing" : ""}`}
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
          className={`composer${dragOver ? " composer--dragover" : ""}${disabled ? " composer--disabled" : ""}${text.trimStart().startsWith("!") ? " composer--shell" : ""}`}
          onDrop={onDrop}
          onDragOver={onDragOver}
          onDragLeave={onDragLeave}
        >
          <span className="composer__caret">{text.trimStart().startsWith("!") ? "$" : "›"}</span>
          <textarea
            ref={taRef}
            className="composer__input"
            value={text}
            onChange={(e) => setText(e.target.value)}
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
            placeholder={disabled ? t("common.loading") : t("composer.placeholder")}
            rows={1}
            disabled={disabled}
          />
          {!running && (
            <Tooltip label={t("composer.send")}>
              <button
                className="composer__btn composer__btn--send"
                onClick={submit}
                disabled={pendingPaste > 0 || (!text.trim() && attachments.length === 0 && workspaceRefs.length === 0) || disabled}
              >
                <ArrowUp size={16} />
              </button>
            </Tooltip>
          )}
        </div>
        <div className={composerMetaClass}>
          {cwd && (
            <div className="composer-meta__control composer-meta__control--workspace composer-workspace-wrap" ref={workspaceAnchorRef}>
              <button
                className={`composer__workspace${workspaceMenuOpen ? " composer__workspace--open" : ""}`}
                onClick={() => {
                  if (!running) setWorkspaceMenuOpen((open) => !open);
                }}
                disabled={running}
              >
                <FolderGit2 size={13} />
                <span>{workspaceName}</span>
                <ChevronDown size={12} />
              </button>
            </div>
          )}
          <div className="composer-meta__params">
            <div className="composer-meta__control composer-meta__control--model">
              <ModelSwitcher label={modelLabel} tabId={tabId} onPick={onSwitchModel} />
            </div>
            {effort?.supported && (
              <div className="composer-meta__control composer-meta__control--effort">
                <EffortSwitcher effort={effort} disabled={running} onPick={onSetEffort} />
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
