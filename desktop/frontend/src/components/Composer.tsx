import { useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, ClipboardEvent, DragEvent, KeyboardEvent, PointerEvent as ReactPointerEvent } from "react";
import { ArrowUp, Check, ChevronDown, Eye, FileText, FolderGit2, FolderPlus, Search, Square, Trash2, X } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { clearLayoutSize, loadOptionalLayoutSize, saveLayoutSize } from "../lib/layoutPreferences";
import type { CommandInfo, DirEntry, Mode, SlashArgItem, SlashArgsResult, WorkspaceView } from "../lib/types";
import { SlashMenu } from "./SlashMenu";
import { ArgMenu } from "./ArgMenu";
import { FileMenu } from "./FileMenu";

interface Attachment {
  path: string;
  previewUrl?: string;
}

const LONG_PASTE_MIN_CHARS = 2000;
const LONG_PASTE_MIN_LINES = 20;
const COMPOSER_MIN_HEIGHT = 86;
const COMPOSER_MAX_HEIGHT = 360;
const COMPOSER_MAX_VIEWPORT_RATIO = 0.4;
// Grace after compositionend to swallow a confirm-Enter that lands just after
// it; the real gap is a few ms, so keep it short or a deliberate quick second
// Enter (submit) gets eaten too.
const IME_CONFIRM_GRACE_MS = 100;

type PastedBlock = {
  label: string;
  text: string;
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

function composerMaxHeight(): number {
  if (typeof window === "undefined") return COMPOSER_MAX_HEIGHT;
  return Math.max(COMPOSER_MIN_HEIGHT, Math.min(COMPOSER_MAX_HEIGHT, Math.floor(window.innerHeight * COMPOSER_MAX_VIEWPORT_RATIO)));
}

function clampComposerHeight(height: number): number {
  return Math.min(Math.max(Math.round(height), COMPOSER_MIN_HEIGHT), composerMaxHeight());
}

function loadComposerHeight(): number | null {
  return loadOptionalLayoutSize("composerHeight", clampComposerHeight);
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
  onSend,
  onCancel,
  onCycleMode,
  onPickFolder,
  disabled,
}: {
  running: boolean;
  mode: Mode;
  cwd?: string;
  onSend: (displayText: string, submitText?: string) => void;
  // Returns the un-sent text when cancelling before the server replied (so it can
  // be restored to the input); undefined for a normal cancel.
  onCancel: () => string | undefined;
  onCycleMode: () => void;
  onPickFolder: (path?: string) => Promise<string>;
  disabled?: boolean;
}) {
  const t = useT();
  const [text, setText] = useState("");
  const [attachments, setAttachments] = useState<Attachment[]>([]);
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
  const [composerHeight, setComposerHeight] = useState<number | null>(loadComposerHeight);
  const [composerResizing, setComposerResizing] = useState(false);
  const taRef = useRef<HTMLTextAreaElement>(null);
  const composerCardRef = useRef<HTMLDivElement>(null);
  const workspaceAnchorRef = useRef<HTMLDivElement>(null);
  const workspaceMenuRef = useRef<HTMLDivElement>(null);
  const wasRunning = useRef(running);
  const composingRef = useRef(false);
  const lastCompositionEndAt = useRef(0);

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
    app.Commands().then(setCommands).catch(() => {});
  }, []);

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
  // /mcp → add/remove, /model → refs). Fetched from app.SlashArgs.
  const [argRes, setArgRes] = useState<SlashArgsResult | null>(null);
  useEffect(() => {
    if (!text.startsWith("/") || !/\s/.test(text)) {
      setArgRes(null);
      return;
    }
    let live = true;
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
        const useful = (r.items ?? []).filter((it) => text.slice(0, r.from) + it.insert !== text);
        setArgRes(useful.length > 0 ? { items: useful, from: r.from } : null);
        setActive(0);
      })
      .catch(() => {});
    return () => {
      live = false;
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
  const dirCache = useRef<Record<string, DirEntry[]>>({});
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
        const list = es ?? [];
        dirCache.current[atDir] = list;
        if (live) setEntries(list);
      })
      .catch(() => {});
    return () => {
      live = false;
    };
    // re-fetch only when the menu opens or the directory level changes
  }, [atRaw === null, atDir]);
  const atMatches = useMemo(
    () => (atRaw === null ? [] : entries.filter((e) => e.name.toLowerCase().includes(atFrag)).slice(0, 10)),
    [atRaw, atFrag, entries],
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
    if ((!t && attachments.length === 0) || pendingPaste > 0) return;
    const refs = attachments.map((a) => `@${a.path}`).join(" ");
    const displayText = [t, refs].filter(Boolean).join(t && refs ? " " : "");
    const submitText = [expandPastedBlocks(t), refs].filter(Boolean).join(t && refs ? " " : "");
    onSend(displayText, submitText);
    setText("");
    setAttachments([]);
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

  // Non-image drops (PDFs, docs): the browser hands us bytes, not a path, so the
  // kernel stores them and we reference the saved path — attached, not ignored.
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

  const onDrop = (e: DragEvent<HTMLDivElement>) => {
    const files = Array.from(e.dataTransfer.files);
    if (files.length === 0) return;
    e.preventDefault();
    setDragOver(false);
    attachFiles(files);
  };

  const onDragOver = (e: DragEvent<HTMLDivElement>) => {
    if (!Array.from(e.dataTransfer.items).some((it) => it.kind === "file")) return;
    e.preventDefault(); // required for the drop event to fire
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
    app.ListWorkspaces().then(setWorkspaces).catch(() => setWorkspaces([]));
  };

  useEffect(() => {
    if (workspaceMenuOpen) loadWorkspaces();
  }, [workspaceMenuOpen, cwd]);

  useEffect(() => {
    if (!workspaceMenuOpen) return;
    const close = (e: MouseEvent) => {
      const target = e.target as Node;
      if (workspaceAnchorRef.current?.contains(target) || workspaceMenuRef.current?.contains(target)) return;
      setWorkspaceMenuOpen(false);
    };
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, [workspaceMenuOpen]);

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

  useEffect(() => {
    const onResize = () => setComposerHeight((height) => (height === null ? null : clampComposerHeight(height)));
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  const saveComposerHeight = (height: number) => {
    saveLayoutSize("composerHeight", height, clampComposerHeight);
  };

  const resetComposerHeight = () => {
    setComposerHeight(null);
    clearLayoutSize("composerHeight");
  };

  const onComposerResizeStart = (e: ReactPointerEvent<HTMLDivElement>) => {
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
    if (e.key === "Escape" && running) {
      e.preventDefault();
      handleCancel();
    }
  };

  const composerCardStyle = composerHeight === null ? undefined : ({ "--composer-height": `${composerHeight}px` } as CSSProperties);

  return (
    <div className="composer-wrap">
      {workspaceMenuOpen && cwd && (
        <div className="workspace-switcher" ref={workspaceMenuRef}>
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
              <button
                key={w.path}
                className="workspace-switcher__item"
                onClick={() => {
                  if (w.current) {
                    setWorkspaceMenuOpen(false);
                    return;
                  }
                  void chooseWorkspace(w.path);
                }}
                title={w.path}
              >
                <FolderGit2 size={15} />
                <span>{w.name}</span>
                {w.current && <Check size={15} />}
              </button>
            ))}
            {filteredWorkspaces.length === 0 && <div className="workspace-switcher__empty">{t("composer.noProjectMatches")}</div>}
          </div>
          <div className="workspace-switcher__actions">
            <button onClick={() => void chooseWorkspace()}>
              <FolderPlus size={15} />
              <span>{t("composer.addProject")}</span>
            </button>
          </div>
        </div>
      )}
      {menuMode === "slash" && (
        <SlashMenu items={slashMatches} activeIndex={active} onPick={pickCommand} onHover={setActive} />
      )}
      {menuMode === "slasharg" && argRes && (
        <ArgMenu items={argRes.items} activeIndex={active} onPick={pickArg} onHover={setActive} />
      )}
      {menuMode === "at" && <FileMenu items={atMatches} activeIndex={active} onPick={pickEntry} onHover={setActive} />}
      {attachments.length > 0 && (
        <div className="composer__attachments">
          {attachments.map((a) => (
            <div className="composer__attachment" key={a.path}>
              {a.previewUrl ? <img src={a.previewUrl} alt="" /> : <FileText size={16} />}
              <span>{a.path.split("/").pop()}</span>
              <button
                type="button"
                title={t("composer.removeImage")}
                onClick={() => setAttachments((prev) => prev.filter((x) => x.path !== a.path))}
              >
                <X size={14} />
              </button>
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
                  <button type="button" title={t(open ? "composer.pastedHidePreview" : "composer.pastedShowPreview")} onClick={() => togglePastedPreview(block.label)}>
                    <Eye size={14} />
                  </button>
                  <button type="button" title={t("composer.pastedExpand")} onClick={() => expandPastedBlock(block)}>
                    {t("composer.pastedExpand")}
                  </button>
                  <button type="button" title={t("composer.pastedRemove")} onClick={() => removePastedBlock(block)}>
                    <Trash2 size={14} />
                  </button>
                </div>
                {open && <pre className="composer__pasted-preview">{block.text}</pre>}
              </div>
            );
          })}
        </div>
      )}
      <div
        className={`composer-card${composerHeight !== null ? " composer-card--resized" : ""}${composerResizing ? " composer-card--resizing" : ""}`}
        ref={composerCardRef}
        style={composerCardStyle}
      >
        <div
          className="composer-resize-handle"
          onPointerDown={onComposerResizeStart}
          onDoubleClick={resetComposerHeight}
        />
        <div
          className={`composer${dragOver ? " composer--dragover" : ""}${disabled ? " composer--disabled" : ""}`}
          onDrop={onDrop}
          onDragOver={onDragOver}
          onDragLeave={onDragLeave}
        >
          <span className="composer__caret">›</span>
          <textarea
            ref={taRef}
            className="composer__input"
            value={text}
            onChange={(e) => setText(e.target.value)}
            onPaste={onPaste}
            onKeyDown={onKeyDown}
            onCompositionStart={() => {
              composingRef.current = true;
            }}
            onCompositionEnd={() => {
              composingRef.current = false;
              lastCompositionEndAt.current = Date.now();
            }}
            placeholder={disabled ? t("common.loading") : t("composer.placeholder")}
            rows={1}
            disabled={disabled}
          />
          {running ? (
            <button className="composer__btn composer__btn--stop" onClick={handleCancel} title={t("composer.stop")}>
              <Square size={14} fill="currentColor" />
            </button>
          ) : (
            <button
              className="composer__btn composer__btn--send"
              onClick={submit}
              disabled={pendingPaste > 0 || (!text.trim() && attachments.length === 0) || disabled}
              title={t("composer.send")}
            >
              <ArrowUp size={16} />
            </button>
          )}
        </div>
        <div className="composer-meta">
          {cwd && (
            <div className="composer-workspace-wrap" ref={workspaceAnchorRef}>
              <button
                className={`composer__workspace${workspaceMenuOpen ? " composer__workspace--open" : ""}`}
                onClick={() => {
                  if (!running) setWorkspaceMenuOpen((open) => !open);
                }}
                disabled={running}
                title={running ? t("common.busyHint") : t("status.switchFolder", { cwd })}
              >
                <FolderGit2 size={13} />
                <span>{workspaceName}</span>
                <ChevronDown size={12} />
              </button>
            </div>
          )}
          <button
            className={`composer__mode composer__mode--${mode}`}
            onClick={onCycleMode}
            title={t("composer.modeTitle")}
          >
            <span className="composer__mode-dot" />
            {mode === "yolo" ? t("composer.modeYolo") : mode === "plan" ? t("composer.modePlan") : t("composer.modeNormal")}
            <span className="composer__mode-hint">{t("composer.modeHint")}</span>
          </button>
        </div>
      </div>
    </div>
  );
}
