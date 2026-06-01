import { useEffect, useMemo, useRef, useState } from "react";
import type { ClipboardEvent, DragEvent, KeyboardEvent } from "react";
import { ArrowUp, Square, X } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { CommandInfo, DirEntry, Mode, SlashArgItem, SlashArgsResult } from "../lib/types";
import { SlashMenu } from "./SlashMenu";
import { ArgMenu } from "./ArgMenu";
import { FileMenu } from "./FileMenu";

interface Attachment {
  path: string;
  previewUrl: string;
}

const LONG_PASTE_MIN_CHARS = 1000;
const LONG_PASTE_MIN_LINES = 5;

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

export function Composer({
  running,
  mode,
  onSend,
  onCancel,
  onCycleMode,
  disabled,
}: {
  running: boolean;
  mode: Mode;
  onSend: (displayText: string, submitText?: string) => void;
  // Returns the un-sent text when cancelling before the server replied (so it can
  // be restored to the input); undefined for a normal cancel.
  onCancel: () => string | undefined;
  onCycleMode: () => void;
  disabled?: boolean;
}) {
  const t = useT();
  const [text, setText] = useState("");
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [pendingPaste, setPendingPaste] = useState(0);
  const pastedBlocksRef = useRef<PastedBlock[]>([]);
  const nextPasteId = useRef(1);
  const [active, setActive] = useState(0);
  const [dismissed, setDismissed] = useState(false);
  const [dragOver, setDragOver] = useState(false);
  const taRef = useRef<HTMLTextAreaElement>(null);
  const wasRunning = useRef(running);

  useEffect(() => {
    if (wasRunning.current && !running && text.trim() === "") {
      pastedBlocksRef.current = [];
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
    const t = text.trim();
    if ((!t && attachments.length === 0) || pendingPaste > 0) return;
    const refs = attachments.map((a) => `@${a.path}`).join(" ");
    const displayText = [t, refs].filter(Boolean).join(t && refs ? " " : "");
    const submitText = [expandPastedBlocks(t), refs].filter(Boolean).join(t && refs ? " " : "");
    onSend(displayText, submitText);
    setText("");
    setAttachments([]);
  };

  const attachImageFiles = async (files: File[]) => {
    const images = files.filter((f) => f.type.startsWith("image/"));
    if (images.length === 0) return;
    for (const file of images) {
      setPendingPaste((n) => n + 1);
      try {
        const dataUrl = await new Promise<string>((resolve, reject) => {
          const reader = new FileReader();
          reader.onload = () => resolve(String(reader.result));
          reader.onerror = () => reject(reader.error);
          reader.readAsDataURL(file);
        });
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

  const onPaste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
    const files = Array.from(e.clipboardData.files).filter((f) => f.type.startsWith("image/"));
    if (files.length > 0) {
      e.preventDefault();
      void attachImageFiles(files);
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
    const label = `[Pasted text #${id} · ${lines} lines]`;
    const block: PastedBlock = { label, text: pasted };
    const next = text.slice(0, start) + label + text.slice(end);

    pastedBlocksRef.current = [...pastedBlocksRef.current, block];
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
    if (!files.some((f) => f.type.startsWith("image/"))) return;
    e.preventDefault();
    setDragOver(false);
    void attachImageFiles(files);
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
    const composing = e.nativeEvent.isComposing;

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

    // Enter sends; Shift+Enter newline. isComposing guards IME (pinyin) confirms.
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

  return (
    <div className="composer-wrap">
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
              <img src={a.previewUrl} alt="" />
              <span>{a.path.split("/").pop()}</span>
              <button
                type="button"
                title="Remove image"
                onClick={() => setAttachments((prev) => prev.filter((x) => x.path !== a.path))}
              >
                <X size={14} />
              </button>
            </div>
          ))}
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
    </div>
  );
}
