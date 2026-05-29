import {
  type ChangeEvent,
  type KeyboardEvent,
  type RefObject,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import type React from "react";
import { invoke } from "@tauri-apps/api/core";
import { open as openFileDialog } from "@tauri-apps/plugin-dialog";
import { t, type TKey } from "../i18n";
import { I } from "../icons";
import {
  DEFAULT_COMPOSER_ROWS,
  applyComposerTextareaAutosize,
} from "./composer-sizing";
import { fmtElapsed } from "./live";
import { Shortcut } from "./shortcut";

export type ReasoningEffort = "low" | "medium" | "high" | "max";
export type EditMode = "review" | "auto" | "yolo" | "plan";

type ModeEntry = { k: EditMode; label: TKey; icon: React.ReactNode; hint: TKey };

const EFFORTS: readonly ReasoningEffort[] = ["low", "medium", "high", "max"];

const MODE_INFO: ModeEntry[] = [
  { k: "plan", label: "editMode.plan", icon: <I.list size={11} />, hint: "editMode.planHint" },
  { k: "review", label: "editMode.review", icon: <I.shield size={11} />, hint: "editMode.reviewHint" },
  { k: "auto", label: "editMode.auto", icon: <I.zap size={11} />, hint: "editMode.autoHint" },
  { k: "yolo", label: "editMode.yolo", icon: <I.warn size={11} />, hint: "editMode.yoloHint" },
];

export function ModeSwitch({
  mode,
  onChange,
}: {
  mode: EditMode;
  onChange: (m: EditMode) => void;
}) {
  return (
    <div className="mode-switch" data-mode={mode}>
      {MODE_INFO.map((m) => (
        <button
          key={m.k}
          type="button"
          className="ms-seg"
          data-on={mode === m.k}
          data-k={m.k}
          onClick={() => onChange(m.k)}
          title={t(m.hint)}
        >
          {m.icon}
          <span>{t(m.label)}</span>
        </button>
      ))}
    </div>
  );
}

export type SlashCmd = {
  cmd: string;
  desc: string;
  run: () => void;
  kb?: string;
  insertOnly?: boolean;
};
export type MentionItem = {
  name: string;
  path?: string;
  kind: "file" | "dir" | "url" | "agent" | "clip";
  desc?: string;
};

export type Chip =
  | { kind: "at"; label: string }
  | { kind: "slash"; label: string };

/** For long paths show only the filename; truncate filename if it's still too long. */
function chipLabel(label: string, maxLen = 32): string {
  if (label.length <= maxLen) return label;
  const sep = label.includes("/") ? "/" : "\\";
  const segments = label.split(sep);
  const filename = segments[segments.length - 1]!;
  if (filename.length <= maxLen) return filename;
  return filename.slice(0, maxLen - 1) + "…";
}

type Popup =
  | { kind: "slash"; query: string }
  | { kind: "at"; query: string; nonce: number }
  | null;

type ActiveRange = { start: number; end: number; sigil: string; query: string };

/** Return the @word or /word range at cursorPos, or null. */
function findActiveRange(text: string, cursorPos: number): ActiveRange | null {
  if (cursorPos < 0 || cursorPos > text.length) return null;
  let wordStart = cursorPos;
  while (wordStart > 0 && !/\s/.test(text[wordStart - 1]!)) wordStart--;
  let wordEnd = cursorPos;
  while (wordEnd < text.length && !/\s/.test(text[wordEnd]!)) wordEnd++;
  if (wordStart === wordEnd) return null;
  const word = text.slice(wordStart, wordEnd);
  if (word.length > 0 && (word[0] === "@" || word[0] === "/")) {
    if (wordStart === 0 || /\s/.test(text[wordStart - 1]!)) {
      return { start: wordStart, end: wordEnd, sigil: word[0], query: word.slice(1) };
    }
  }
  return null;
}

/** Render draft text with @word and /word tokens wrapped in highlight spans. */
function highlightTokens(text: string): React.ReactNode {
  if (!text) return text;
  const parts: React.ReactNode[] = [];
  let lastIdx = 0;
  for (const m of text.matchAll(/(\s|^)([@\/]\S+)/g)) {
    const pre = text.slice(lastIdx, m.index);
    if (pre) parts.push(pre);
    parts.push(
      <span key={m.index}>
        {m[1]}
        <span className="hl-token">{m[2]}</span>
      </span>,
    );
    lastIdx = m.index + m[0].length;
  }
  if (lastIdx < text.length) {
    parts.push(text.slice(lastIdx));
  }
  return parts.length > 0 ? parts : text;
}

function slashIcon(cmd: string) {
  const m: Record<string, React.ReactNode> = {
    "/clear": <I.x size={12} />,
    "/new": <I.plus size={12} />,
    "/abort": <I.stop size={12} />,
    "/copy": <I.layers size={12} />,
    "/export": <I.download size={12} />,
    "/model": <I.cpu size={12} />,
    "/theme": <I.sun size={12} />,
    "/lang": <I.globe size={12} />,
  };
  return m[cmd] || <I.slash size={12} />;
}

/** Parent dir of the current @ query, with trailing slash. `null` = no parent to show (at workspace root). */
function parentOfAtQuery(query: string): string | null {
  const normalized = query.replace(/\\/g, "/");
  const trailingSlash = normalized.endsWith("/");
  const lastSlash = normalized.lastIndexOf("/");
  if (lastSlash < 0) return null;
  const dirContext = trailingSlash ? normalized.slice(0, -1) : normalized.slice(0, lastSlash);
  if (!dirContext) return null;
  const parentIdx = dirContext.lastIndexOf("/");
  return parentIdx >= 0 ? `${dirContext.slice(0, parentIdx)}/` : "";
}

function displayMentionPath(path: string): { name: string; desc?: string } {
  const normalized = path.replace(/\\/g, "/");
  const isDir = /[\\/]$/.test(path);
  const trimmed = normalized.replace(/\/+$/, "");
  if (!trimmed) return { name: path };
  const slash = trimmed.lastIndexOf("/");
  const base = slash >= 0 ? trimmed.slice(slash + 1) : trimmed;
  const parent = slash >= 0 ? `${trimmed.slice(0, slash)}/` : "";
  return {
    name: isDir ? `${base}/` : base,
    desc: parent || undefined,
  };
}

function atIcon(k: MentionItem["kind"]) {
  if (k === "file") return <I.file size={12} />;
  if (k === "dir") return <I.folder size={12} />;
  if (k === "url") return <I.globe size={12} />;
  if (k === "agent") return <I.bot size={12} />;
  if (k === "clip") return <I.layers size={12} />;
  return <I.at size={12} />;
}

function guessImageExtension(mime: string): string {
  const normalized = mime.toLowerCase();
  if (normalized === "image/jpeg") return "jpg";
  if (normalized === "image/png") return "png";
  if (normalized === "image/gif") return "gif";
  if (normalized === "image/webp") return "webp";
  if (normalized === "image/svg+xml") return "svg";
  const slash = normalized.indexOf("/");
  return slash >= 0 ? normalized.slice(slash + 1).replace(/[^a-z0-9]+/g, "") || "png" : "png";
}

export function Composer({
  draft,
  setDraft,
  onDraftUserEdit,
  promptHistoryBrowsing,
  onPromptHistoryNavigate,
  onSend,
  onAbort,
  disabled,
  busy,
  busyLabel,
  busyElapsedMs,
  modelLabel,
  reasoningEffort,
  onModelChange,
  onEffortChange,
  editMode,
  onEditModeChange,
  textareaRef,
  slashCommands,
  onMentionQuery,
  onMentionPreview,
  onMentionPicked,
  mentionResults,
  workspaceDir,
  queuedSends,
  onQueueWhileBusy,
  onDequeueSend,
}: {
  draft: string;
  setDraft: React.Dispatch<React.SetStateAction<string>>;
  onDraftUserEdit?: () => void;
  promptHistoryBrowsing?: boolean;
  onPromptHistoryNavigate?: (direction: "older" | "newer") => boolean | void;
  onSend: () => void;
  onAbort: () => void;
  disabled?: boolean;
  busy?: boolean;
  /** Replaces the hint-row left side while the agent is running — typically "Reasoning" or "Skill · <name>". */
  busyLabel?: string;
  busyElapsedMs?: number;
  modelLabel: string;
  reasoningEffort: ReasoningEffort;
  onModelChange: (model: string) => void;
  onEffortChange: (effort: ReasoningEffort) => void;
  editMode: EditMode;
  onEditModeChange: (mode: EditMode) => void;
  textareaRef: RefObject<HTMLTextAreaElement | null>;
  slashCommands: SlashCmd[];
  onMentionQuery?: (q: string, nonce: number) => void;
  onMentionPreview?: (path: string, nonce: number) => void;
  onMentionPicked?: (path: string) => void;
  mentionResults?: { nonce: number; query: string; results: string[] } | null;
  workspaceDir?: string;
  /** Messages typed while busy=true; rendered as removable chips above the textarea and auto-drained FIFO on turn-complete. */
  queuedSends?: string[];
  /** Called when the user presses Enter while busy with a non-empty draft. Owns clearing the draft. */
  onQueueWhileBusy?: (text: string) => void;
  onDequeueSend?: (index: number) => void;
}) {
  const [popup, setPopup] = useState<Popup>(null);
  const [pickedChips, setPickedChips] = useState<Map<string, Chip["kind"]>>(new Map());
  const chips = useMemo(() => {
    const result: Chip[] = [];
    for (const [label, kind] of pickedChips) {
      const sigil = kind === "at" ? "@" : "/";
      const escaped = sigil + label.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
      const re = new RegExp(`(?:^|\\s)${escaped}(?:\\s|$)`);
      if (re.test(draft)) {
        result.push({ kind, label });
      }
    }
    return result;
  }, [draft, pickedChips]);
  const [activeIdx, setActiveIdx] = useState(0);
  const [modelMenuOpen, setModelMenuOpen] = useState(false);
  const backdropContent = useMemo(() => highlightTokens(draft), [draft]);
  const nonceRef = useRef(0);
  const modelWrapRef = useRef<HTMLDivElement>(null);
  // macOS Chinese IME fires compositionend BEFORE the confirm keydown.
  const composingRef = useRef(false);
  const compositionEndedAtRef = useRef(0);
  const activeRangeRef = useRef<ActiveRange | null>(null);
  const backdropRef = useRef<HTMLDivElement>(null);

  const insertMention = (picked: string) => {
    const rel =
      workspaceDir && picked.startsWith(workspaceDir)
        ? picked.slice(workspaceDir.length).replace(/^[\\/]+/, "")
        : picked;
    const range = activeRangeRef.current;
    if (range && range.sigil === "@") {
      const savedRange = { ...range };
      setDraft((current) => {
        const before = current.slice(0, savedRange.start);
        const after = current.slice(savedRange.end);
        const insertion = `@${rel}`;
        const spacerBefore = before && !before.endsWith(" ") ? " " : "";
        const spacerAfter = after ? (after.startsWith(" ") ? "" : " ") : " ";
        return `${before}${spacerBefore}${insertion}${spacerAfter}${after}`;
      });
    } else {
      setDraft((current) =>
        current ? `${current.replace(/\s+$/, "")} @${rel} ` : `@${rel} `,
      );
    }
    activeRangeRef.current = null;
    setPickedChips((prev) => new Map(prev).set(rel, "at"));
    onMentionPicked?.(rel);
    setPopup(null);
    textareaRef.current?.focus();
  };

  useLayoutEffect(() => {
    const textarea = textareaRef.current;
    if (!textarea) return;
    applyComposerTextareaAutosize(textarea);
  });

  // Programmatic draft transitions to "/" (e.g. /help suggestion in EmptyState, #929) must open the slash popup, since handleChange only fires on actual user input.
  const prevDraftRef = useRef(draft);
  useEffect(() => {
    const prev = prevDraftRef.current;
    prevDraftRef.current = draft;
    if (draft === "/" && prev !== "/") {
      activeRangeRef.current = { start: 0, end: 1, sigil: "/", query: "" };
      setPopup({ kind: "slash", query: "" });
    }
  }, [draft]);

  useEffect(() => {
    if (!modelMenuOpen) return;
    const onDown = (e: MouseEvent) => {
      if (
        modelWrapRef.current &&
        !modelWrapRef.current.contains(e.target as Node)
      ) {
        setModelMenuOpen(false);
      }
    };
    window.addEventListener("mousedown", onDown);
    return () => window.removeEventListener("mousedown", onDown);
  }, [modelMenuOpen]);

  const attachFile = async (filter?: "image") => {
    try {
      const picked = await openFileDialog({
        multiple: false,
        directory: false,
        defaultPath: workspaceDir,
        filters:
          filter === "image"
            ? [
                {
                  name: t("composer.imageFilterName"),
                  extensions: ["png", "jpg", "jpeg", "gif", "webp", "svg"],
                },
              ]
            : undefined,
      });
      if (typeof picked !== "string" || !picked) return;
      insertMention(picked);
    } catch (err) {
      console.error("attach failed", err);
    }
  };

  const handlePaste = async (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    const items = Array.from(e.clipboardData?.items ?? []);
    const imageItem = items.find((item) => item.type.startsWith("image/"));
    if (!imageItem) return;
    const file = imageItem.getAsFile();
    if (!file) return;
    e.preventDefault();
    try {
      const buffer = await file.arrayBuffer();
      const savedPath = await invoke<string>("save_clipboard_image", {
        bytes: buffer,
        extension: guessImageExtension(file.type),
      });
      insertMention(savedPath);
    } catch (err) {
      console.error("clipboard image paste failed", err);
    }
  };

  const slashItems = useMemo(() => {
    if (!popup || popup.kind !== "slash") return [];
    const q = popup.query.toLowerCase();
    if (!q) return slashCommands;
    return slashCommands.filter((c) => c.cmd.toLowerCase().includes(q));
  }, [popup, slashCommands]);

  const atItems = useMemo<MentionItem[]>(() => {
    if (!popup || popup.kind !== "at") return [];
    if (!mentionResults || mentionResults.nonce !== popup.nonce) return [];
    const base: MentionItem[] = mentionResults.results.map((path) => {
      const display = displayMentionPath(path);
      return {
        name: display.name,
        path,
        kind: path.endsWith("/") || path.endsWith("\\") ? "dir" : "file",
        desc: display.desc,
      };
    });
    // "../" entry (#1019): one level up whenever the @ query is inside a subdir.
    const parent = parentOfAtQuery(popup.query);
    if (parent !== null) {
      base.unshift({
        name: "..",
        kind: "dir",
        desc: parent ? `↑ ${parent}` : `↑ ${t("composer.workspaceRoot")}`,
      });
    }
    return base;
  }, [popup, mentionResults]);

  const items =
    popup?.kind === "slash" ? slashItems : popup?.kind === "at" ? atItems : [];

  useEffect(() => {
    setActiveIdx(0);
  }, [popup?.kind]);

  useEffect(() => {
    setActiveIdx((i) => (items.length ? Math.min(i, items.length - 1) : 0));
  }, [items.length]);

  useEffect(() => {
    if (!popup || popup.kind !== "at" || !onMentionQuery) return;
    onMentionQuery(popup.query, popup.nonce);
  }, [popup, onMentionQuery]);

  const syncPopupFromCursor = (value: string, cursorPos: number) => {
    const range = findActiveRange(value, cursorPos);
    if (range && (range.sigil === "/" || range.sigil === "@")) {
      activeRangeRef.current = range;
      if (range.sigil === "/") {
        if (popup?.kind !== "slash" || popup.query !== range.query) {
          setPopup({ kind: "slash", query: range.query });
        }
      } else {
        if (popup?.kind !== "at" || popup.query !== range.query) {
          const nonce = ++nonceRef.current;
          setPopup({ kind: "at", query: range.query, nonce });
        }
      }
    } else if (popup) {
      activeRangeRef.current = null;
      setPopup(null);
    }
  };

  const handleChange = (e: ChangeEvent<HTMLTextAreaElement>) => {
    const v = e.target.value;
    onDraftUserEdit?.();
    setDraft(v);
    const cursorPos = e.target.selectionStart ?? v.length;
    syncPopupFromCursor(v, cursorPos);
  };

  const handleSelect = (e: React.SyntheticEvent<HTMLTextAreaElement>) => {
    const ta = e.currentTarget;
    const cursorPos = ta.selectionStart;
    if (cursorPos === null) return;
    syncPopupFromCursor(ta.value, cursorPos);
  };

  const handleTextareaScroll = (e: React.UIEvent<HTMLTextAreaElement>) => {
    if (backdropRef.current) {
      backdropRef.current.scrollTop = e.currentTarget.scrollTop;
    }
  };

  const dismiss = () => setPopup(null);

  const pickItem = (idx: number) => {
    const it = items[idx];
    if (!it || !popup) return;
    const range = activeRangeRef.current;
    const before = range ? draft.slice(0, range.start) : draft;
    const after = range ? draft.slice(range.end) : "";
    if (popup.kind === "slash") {
      const cmd = (it as SlashCmd).cmd;
      const insertOnly = (it as SlashCmd).insertOnly === true;
      if (insertOnly) {
        const spacer = before && !before.endsWith(" ") ? " " : "";
        setDraft(`${before}${spacer}${cmd} ${after}`);
        setPickedChips((prev) => new Map(prev).set(cmd.replace(/^\//, ""), "slash"));
      } else {
        setDraft(`${before}${after}`);
        (it as SlashCmd).run();
      }
    } else {
      const mention = it as MentionItem;
      if (mention.name === "..") {
        const parent = parentOfAtQuery(popup.query) ?? "";
        const newText = `@${parent}`;
        const next = `${before}${newText}${after}`;
        setDraft(next);
        activeRangeRef.current = {
          start: range?.start ?? 0,
          end: (range?.start ?? 0) + newText.length,
          sigil: "@",
          query: parent,
        };
        const nonce = ++nonceRef.current;
        setPopup({ kind: "at", query: parent, nonce });
        textareaRef.current?.focus();
        return;
      }
      const mentionPath = mention.path ?? mention.name;
      insertMention(mentionPath);
      return;
    }
    activeRangeRef.current = null;
    setPopup(null);
    textareaRef.current?.focus();
  };

  const shouldNavigatePromptHistory = (
    e: KeyboardEvent<HTMLTextAreaElement>,
    direction: "older" | "newer",
  ) => {
    if (!onPromptHistoryNavigate) return false;
    if (e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return false;
    const native = e.nativeEvent as globalThis.KeyboardEvent;
    if (native.isComposing) return false;
    const ta = textareaRef.current;
    if (!ta || ta.selectionStart !== ta.selectionEnd) return false;
    const atStart = ta.selectionStart === 0;
    const atEnd = ta.selectionStart === draft.length;
    if (promptHistoryBrowsing) return atStart || atEnd;
    return direction === "older" && atStart;
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (popup) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setActiveIdx((i) => (items.length ? (i + 1) % items.length : 0));
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setActiveIdx((i) =>
          items.length ? (i - 1 + items.length) % items.length : 0,
        );
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        dismiss();
        return;
      }
      if (e.key === "Tab" && popup.kind === "at" && items.length > 0) {
        e.preventDefault();
        // `..` is the synthetic parent-dir entry (#1019); same shape
        // but rewrites to the parent path.
        const it = items[activeIdx];
        if (it && (it as MentionItem).kind === "dir") {
          const mention = it as MentionItem;
          const range = activeRangeRef.current;
          const before = range ? draft.slice(0, range.start) : draft;
          const after = range ? draft.slice(range.end) : "";
          if (mention.name === "..") {
            const parent = parentOfAtQuery(popup.query) ?? "";
            const newText = `@${parent}`;
            const next = `${before}${newText}${after}`;
            setDraft(next);
            activeRangeRef.current = {
              start: range?.start ?? 0,
              end: (range?.start ?? 0) + newText.length,
              sigil: "@",
              query: parent,
            };
            const nonce = ++nonceRef.current;
            setPopup({ kind: "at", query: parent, nonce });
            return;
          }
          const dirPath = (mention.path ?? mention.name).replace(/[\\/]+$/, "");
          const newText = `@${dirPath}/`;
          const next = `${before}${newText}${after}`;
          setDraft(next);
          activeRangeRef.current = {
            start: range?.start ?? 0,
            end: (range?.start ?? 0) + newText.length,
            sigil: "@",
            query: `${dirPath}/`,
          };
          const nonce = ++nonceRef.current;
          setPopup({ kind: "at", query: `${dirPath}/`, nonce });
          return;
        }
      }
      if (e.key === "Enter") {
        if (items.length > 0) {
          e.preventDefault();
          pickItem(activeIdx);
          return;
        }
        dismiss();
      }
    }
    if (!popup) {
      if (e.key === "ArrowUp" || e.key === "ArrowDown") {
        const direction = e.key === "ArrowUp" ? "older" : "newer";
        if (shouldNavigatePromptHistory(e, direction)) {
          const handled = onPromptHistoryNavigate?.(direction);
          if (handled !== false) {
            e.preventDefault();
            return;
          }
        }
      }
    }
    if (composingRef.current || e.nativeEvent.isComposing || Date.now() - compositionEndedAtRef.current < 50) return;
    if (e.key === "Enter" && !e.shiftKey && !popup) {
      e.preventDefault();
      if (busy) {
        const text = draft.trim();
        if (text && onQueueWhileBusy) {
          onQueueWhileBusy(text);
          setPickedChips(new Map());
        }
      } else if (!disabled && draft.trim()) {
        onSend();
        setPickedChips(new Map());
      }
    }
  };

  return (
    <div className="composer-wrap">
      <div className="composer-inner">
        {queuedSends && queuedSends.length > 0 ? (
          <div className="composer-queued">
            <span className="composer-queued-label">
              {t("composer.queueCount", { n: queuedSends.length })}
            </span>
            {queuedSends.map((text, i) => (
              <span key={i} className="composer-queue-chip" title={text}>
                <span className="text">{text}</span>
                {onDequeueSend ? (
                  <span className="x" onClick={() => onDequeueSend(i)}>
                    <I.x size={10} />
                  </span>
                ) : null}
              </span>
            ))}
          </div>
        ) : null}

        <div className="hint-row">
          {busy && busyLabel ? (
            <>
              <span className="composer-busy-status">
                <span className="composer-busy-pip" />
                <span className="composer-busy-label">{busyLabel}</span>
                <span className="composer-busy-time">
                  {fmtElapsed(busyElapsedMs ?? 0)}
                </span>
              </span>
              <span className="grow" />
              <ModeSwitch mode={editMode} onChange={onEditModeChange} />
              <span className="hint-sep" />
              <span>
                <Shortcut keys={["enter"]} /> {t("composer.queue")} &nbsp;·&nbsp;{" "}
                <Shortcut keys={["esc"]} /> {t("composer.interrupt")}
              </span>
            </>
          ) : (
            <>
              <span>
                <Shortcut keys={["/"]} /> {t("composer.commands")} &nbsp;·&nbsp;{" "}
                <Shortcut keys={["@"]} /> {t("composer.mentionFiles")}
                &nbsp;·&nbsp; <Shortcut keys={["mod", "K"]} /> {t("composer.commandPalette")}
              </span>
              <span className="grow" />
              <ModeSwitch mode={editMode} onChange={onEditModeChange} />
              <span className="hint-sep" />
              <span>
                <Shortcut keys={["enter"]} /> {t("composer.send")} &nbsp;{" "}
                <Shortcut keys={["shift", "enter"]} /> {t("composer.newline")}
              </span>
            </>
          )}
        </div>

        <div className="composer">
          {chips.length > 0 ? (
            <div className="composer-tags">
              {chips.map((c, i) => (
                <span key={`${c.kind}-${c.label}-${i}`} className={`chip ${c.kind}`} title={c.label}>
                  {c.kind === "slash" ? (
                    <I.slash size={11} />
                  ) : (
                    <I.at size={11} />
                  )}
                  <span>{chipLabel(c.label)}</span>
                </span>
              ))}
            </div>
          ) : null}

          <div className="composer-textarea-wrap">
            <div className="composer-backdrop" ref={backdropRef} aria-hidden="true">
              {backdropContent}
            </div>
            <textarea
              ref={textareaRef}
              value={draft}
              placeholder={t("composer.placeholder")}
              onChange={handleChange}
              onSelect={handleSelect}
              onScroll={handleTextareaScroll}
              onPaste={(e) => void handlePaste(e)}
              onKeyDown={handleKeyDown}
              onCompositionStart={() => { composingRef.current = true; }}
              onCompositionEnd={() => {
                composingRef.current = false;
                compositionEndedAtRef.current = Date.now();
              }}
              rows={DEFAULT_COMPOSER_ROWS}
              disabled={disabled}
            />
          </div>

          <div className="composer-foot">
            <button
              type="button"
              className="cf-btn"
              title={t("composer.insertFile")}
              onClick={() => void attachFile()}
            >
              <span className="ico">
                <I.paperclip size={14} />
              </span>
            </button>
            <button
              type="button"
              className="cf-btn"
              title={t("composer.insertImage")}
              onClick={() => void attachFile("image")}
            >
              <span className="ico">
                <I.image size={14} />
              </span>
            </button>
            <button
              type="button"
              className="cf-btn"
              onClick={() => setPopup({ kind: "slash", query: "" })}
            >
              <span className="ico">
                <I.slash size={14} />
              </span>
              <span className="label">{t("composer.commandsLabel")}</span>
            </button>
            <button
              type="button"
              className="cf-btn"
              onClick={() => {
                const nonce = ++nonceRef.current;
                setPopup({ kind: "at", query: "", nonce });
              }}
            >
              <span className="ico">
                <I.at size={14} />
              </span>
              <span className="label">{t("composer.mentionLabel")}</span>
            </button>

            <span className="grow" />

            <div ref={modelWrapRef} style={{ position: "relative" }}>
              <button
                type="button"
                className="model-pill"
                onClick={() => setModelMenuOpen((v) => !v)}
                title={t("composer.switchModel")}
              >
                <I.brain size={12} />
                <span>{modelLabel}</span>
                <span className="badge">{reasoningEffort}</span>
                <I.chev size={10} />
              </button>
              {modelMenuOpen ? (
                <ModelEffortMenu
                  modelLabel={modelLabel}
                  currentEffort={reasoningEffort}
                  onPickModel={(m) => {
                    onModelChange(m);
                    setModelMenuOpen(false);
                  }}
                  onPickEffort={(e) => {
                    onEffortChange(e);
                    setModelMenuOpen(false);
                  }}
                />
              ) : null}
            </div>
            {busy ? (
              <button
                type="button"
                className="send-btn"
                style={{ background: "var(--danger)" }}
                onClick={onAbort}
                title={t("composer.interrupt")}
              >
                <I.stop size={14} />
              </button>
            ) : (
              <button
                type="button"
                className="send-btn"
                disabled={disabled || !draft.trim()}
                onClick={() => {
                  if (!disabled && draft.trim()) {
                    onSend();
                    setPickedChips(new Map());
                  }
                }}
              >
                <I.send size={14} />
              </button>
            )}
          </div>

          {popup ? (
            <Popup
              kind={popup.kind}
              items={items}
              activeIdx={activeIdx}
              onPick={(i) => pickItem(i)}
              onClose={dismiss}
              onHover={(i, item) => {
                setActiveIdx(i);
                if (popup.kind === "at" && onMentionPreview) {
                  const mention = item as MentionItem;
                  const path = mention.path ?? mention.name;
                  onMentionPreview(path, popup.nonce);
                }
              }}
            />
          ) : null}
        </div>
      </div>
    </div>
  );
}

function Popup({
  kind,
  items,
  activeIdx,
  onPick,
  onClose,
  onHover,
}: {
  kind: "slash" | "at";
  items: (SlashCmd | MentionItem)[];
  activeIdx: number;
  onPick: (i: number) => void;
  onClose: () => void;
  onHover: (i: number, item: SlashCmd | MentionItem) => void;
}) {
  const listRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    requestAnimationFrame(() => {
      const el = listRef.current?.querySelector<HTMLElement>(`[data-active="true"]`);
      el?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
    });
  }, [activeIdx]);

  return (
    <div
      className={kind === "at" ? "popup at-popup" : "popup"}
      onMouseDown={(e) => e.preventDefault()}
    >
      <div className="ph">
        <span className="tok">{kind === "slash" ? "/" : "@"}</span>
        <span>
          {kind === "slash"
            ? t("composer.slashHeader")
            : t("composer.atHeader")}
        </span>
        <span className="grow" />
        <span style={{ cursor: "pointer" }} onClick={onClose}>
          <I.x size={11} />
        </span>
      </div>
      <div className={kind === "at" ? "popup-list at-popup-list" : "popup-list"} ref={listRef}>
        {items.length === 0 ? (
          <div
            style={{
              padding: "12px 8px",
              fontSize: 11.5,
              color: "var(--muted-2)",
              fontFamily: "Geist Mono, monospace",
            }}
          >
            {t("composer.noMatches")}
          </div>
        ) : null}
        {items.map((it, i) => (
          <div
            key={i}
            className="popup-item"
            data-active={i === activeIdx}
            onClick={() => onPick(i)}
            onMouseEnter={() => onHover(i, it)}
          >
            <span className="ico">
              {kind === "slash"
                ? slashIcon((it as SlashCmd).cmd)
                : atIcon((it as MentionItem).kind)}
            </span>
            <div className="nm">
              {kind === "slash" ? (
                <>
                  <span className="cmd">{(it as SlashCmd).cmd}</span>
                  <span className="desc">{(it as SlashCmd).desc}</span>
                </>
              ) : (
                <>
                  <span className="mention-name">{(it as MentionItem).name}</span>
                  {(it as MentionItem).desc ? (
                    <div className="desc">{(it as MentionItem).desc}</div>
                  ) : null}
                </>
              )}
            </div>
            <span className="kb">
              {kind === "slash" ? ((it as SlashCmd).kb ?? "") : ""}
            </span>
          </div>
        ))}
      </div>
      <div className="popup-foot">
        <span>
          <Shortcut keys={["updown"]} /> {t("composer.select")}
        </span>
        <span>
          <Shortcut keys={["enter"]} /> {t("composer.confirm")}
        </span>
        <span>
          <Shortcut keys={["esc"]} /> {t("composer.close")}
        </span>
      </div>
    </div>
  );
}

const KNOWN_MODELS: readonly string[] = ["deepseek-v4-flash", "deepseek-v4-pro"];

function ModelEffortMenu({
  modelLabel,
  currentEffort,
  onPickModel,
  onPickEffort,
}: {
  modelLabel: string;
  currentEffort: ReasoningEffort;
  onPickModel: (model: string) => void;
  onPickEffort: (effort: ReasoningEffort) => void;
}) {
  const [draft, setDraft] = useState(modelLabel);
  return (
    <div
      className="popup"
      style={{
        bottom: "calc(100% + 6px)",
        left: "auto",
        right: 0,
        width: 280,
        position: "absolute",
      }}
    >
      <div className="ph">
        <span className="tok">M</span>
        <span>{t("composer.switchModel")}</span>
      </div>
      <div className="popup-list">
        {KNOWN_MODELS.map((m) => (
          <div
            key={m}
            className="popup-item"
            data-active={m === modelLabel}
            onClick={() => onPickModel(m)}
          >
            <span className="ico">
              <I.brain size={12} />
            </span>
            <div className="nm">
              <span className="cmd">{m}</span>
            </div>
          </div>
        ))}
        <div style={{ padding: "6px 8px", display: "flex", gap: 6 }}>
          <input
            className="field mono"
            style={{ flex: 1 }}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder="custom model id"
          />
          <button
            type="button"
            className="btn"
            disabled={!draft.trim() || draft.trim() === modelLabel}
            onClick={() => onPickModel(draft.trim())}
          >
            {t("composer.confirm")}
          </button>
        </div>
      </div>
      <div className="ph" style={{ marginTop: 4 }}>
        <span className="tok">E</span>
        <span>{t("composer.switchEffort")}</span>
      </div>
      <div className="popup-list">
        {EFFORTS.map((e) => (
          <div
            key={e}
            className="popup-item"
            data-active={e === currentEffort}
            onClick={() => onPickEffort(e)}
          >
            <span className="ico">
              <I.cpu size={12} />
            </span>
            <div className="nm">
              <span className="cmd">{e}</span>
              <div className="desc">{t(`effort.${e}Desc` as TKey)}</div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
