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
import { open as openFileDialog } from "@tauri-apps/plugin-dialog";
import { t, type TKey } from "../i18n";
import { I } from "../icons";
import {
  DEFAULT_COMPOSER_ROWS,
  applyComposerTextareaAutosize,
} from "./composer-sizing";
import { fmtElapsed } from "./live";
import { Shortcut } from "./shortcut";

export type PresetName = "flash" | "pro";
export type EditMode = "review" | "auto" | "yolo";

type PresetEntry = { label: string; badge: string; desc: TKey };
type ModeEntry = { k: EditMode; label: TKey; icon: React.ReactNode; hint: TKey };

const PRESET_INFO: Record<PresetName, PresetEntry> = {
  flash: { label: "v4-flash", badge: "FLASH", desc: "preset.flashDesc" },
  pro: { label: "v4-pro", badge: "PRO", desc: "preset.proDesc" },
};

const MODE_INFO: ModeEntry[] = [
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
  kind: "file" | "dir" | "url" | "agent" | "clip";
  desc?: string;
};

export type Chip =
  | { kind: "at"; label: string }
  | { kind: "slash"; label: string };

type Popup =
  | { kind: "slash"; query: string }
  | { kind: "at"; query: string; nonce: number }
  | null;

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

function atIcon(k: MentionItem["kind"]) {
  if (k === "file") return <I.file size={12} />;
  if (k === "dir") return <I.folder size={12} />;
  if (k === "url") return <I.globe size={12} />;
  if (k === "agent") return <I.bot size={12} />;
  if (k === "clip") return <I.layers size={12} />;
  return <I.at size={12} />;
}

export function Composer({
  draft,
  setDraft,
  onSend,
  onAbort,
  disabled,
  busy,
  busyLabel,
  busyElapsedMs,
  preset,
  modelLabel,
  onPresetChange,
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
  setDraft: (s: string) => void;
  onSend: () => void;
  onAbort: () => void;
  disabled?: boolean;
  busy?: boolean;
  /** Replaces the hint-row left side while the agent is running — typically "Reasoning" or "Skill · <name>". */
  busyLabel?: string;
  busyElapsedMs?: number;
  preset: PresetName;
  modelLabel: string;
  onPresetChange: (preset: PresetName) => void;
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
  const [chips, setChips] = useState<Chip[]>([]);
  const [popup, setPopup] = useState<Popup>(null);
  const [activeIdx, setActiveIdx] = useState(0);
  const [modelMenuOpen, setModelMenuOpen] = useState(false);
  const nonceRef = useRef(0);
  const modelWrapRef = useRef<HTMLDivElement>(null);
  // macOS Chinese IME fires compositionend BEFORE the confirm keydown.
  const composingRef = useRef(false);
  const compositionEndedAtRef = useRef(0);
  const historyRef = useRef<string[]>([]);
  const [browseIdx, setBrowseIdx] = useState(-1);
  const savedDraftRef = useRef("");

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
      const rel =
        workspaceDir && picked.startsWith(workspaceDir)
          ? picked.slice(workspaceDir.length).replace(/^[\\/]+/, "")
          : picked;
      setDraft(draft ? `${draft.replace(/\s+$/, "")} @${rel} ` : `@${rel} `);
      setChips((c) => [...c, { kind: "at", label: rel }]);
      onMentionPicked?.(rel);
      textareaRef.current?.focus();
    } catch (err) {
      console.error("attach failed", err);
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
    const base: MentionItem[] = mentionResults.results.map((path) => ({
      name: path,
      kind: path.endsWith("/") || path.endsWith("\\") ? "dir" : "file",
      desc: path,
    }));
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
  }, [items.length, popup?.kind]);

  useEffect(() => {
    if (!popup || popup.kind !== "at" || !onMentionQuery) return;
    onMentionQuery(popup.query, popup.nonce);
  }, [popup, onMentionQuery]);

  const handleChange = (e: ChangeEvent<HTMLTextAreaElement>) => {
    const v = e.target.value;
    setDraft(v);
    const trail = v.match(/(^|\s)([/@])([^\s]*)$/);
    if (trail) {
      const sigil = trail[2];
      const query = trail[3] ?? "";
      if (sigil === "/") {
        setPopup({ kind: "slash", query });
      } else {
        const nonce = ++nonceRef.current;
        setPopup({ kind: "at", query, nonce });
      }
    } else if (popup) {
      setPopup(null);
    }
  };

  const dismiss = () => setPopup(null);

  const pickItem = (idx: number) => {
    const it = items[idx];
    if (!it || !popup) return;
    if (popup.kind === "slash") {
      const cmd = (it as SlashCmd).cmd;
      const insertOnly = (it as SlashCmd).insertOnly === true;
      if (insertOnly) {
        const next = draft.replace(/[/@][^\s]*$/, "").trimEnd();
        setDraft(next ? `${next} ${cmd} ` : `${cmd} `);
        setChips((c) => [...c, { kind: "slash", label: cmd.replace(/^\//, "") }]);
      } else {
        const next = draft.replace(/[/@][^\s]*$/, "").trimEnd();
        setDraft(next);
        setChips((c) => [...c, { kind: "slash", label: cmd.replace(/^\//, "") }]);
        (it as SlashCmd).run();
      }
    } else {
      const mention = it as MentionItem;
      if (mention.name === "..") {
        const parent = parentOfAtQuery(popup.query) ?? "";
        const next = draft.replace(/[@][^\s]*$/, `@${parent}`);
        setDraft(next);
        const nonce = ++nonceRef.current;
        setPopup({ kind: "at", query: parent, nonce });
        textareaRef.current?.focus();
        return;
      }
      const next = draft.replace(/[/@][^\s]*$/, "").trimEnd();
      setDraft(next ? `${next} @${mention.name} ` : `@${mention.name} `);
      setChips((c) => [...c, { kind: "at", label: mention.name }]);
      onMentionPicked?.(mention.name);
    }
    setPopup(null);
    textareaRef.current?.focus();
  };

  const navigateHistory = (dir: -1 | 1) => {
    const hist = historyRef.current;
    if (hist.length === 0) return;
    if (dir === -1) {
      const nextIdx = browseIdx + 1;
      if (nextIdx < hist.length) {
        if (browseIdx === -1) savedDraftRef.current = draft;
        setBrowseIdx(nextIdx);
        setDraft(hist[hist.length - 1 - nextIdx]);
      }
    } else {
      if (browseIdx > 0) {
        const nextIdx = browseIdx - 1;
        setBrowseIdx(nextIdx);
        setDraft(hist[hist.length - 1 - nextIdx]);
      } else if (browseIdx === 0) {
        setBrowseIdx(-1);
        setDraft(savedDraftRef.current);
      }
    }
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
        // Tab on a directory enters it — replaces `@src` with `@src/`
        // and re-queries so the popup shows that directory's children.
        // `..` is the synthetic parent-dir entry (#1019); same shape
        // but rewrites to the parent path.
        const it = items[activeIdx];
        if (it && (it as MentionItem).kind === "dir") {
          e.preventDefault();
          const mention = it as MentionItem;
          if (mention.name === "..") {
            const parent = parentOfAtQuery(popup.query) ?? "";
            const next = draft.replace(/[@][^\s]*$/, `@${parent}`);
            setDraft(next);
            const nonce = ++nonceRef.current;
            setPopup({ kind: "at", query: parent, nonce });
            return;
          }
          const dirPath = mention.name.replace(/\/+$/, "");
          const next = draft.replace(/[@][^\s]*$/, `@${dirPath}/`);
          setDraft(next);
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
      const ta = textareaRef.current;
      if (e.key === "ArrowUp" && ta && ta.selectionStart === 0) {
        e.preventDefault();
        navigateHistory(-1);
        return;
      }
      if (e.key === "ArrowDown" && ta && ta.selectionStart === draft.length) {
        e.preventDefault();
        navigateHistory(1);
        return;
      }
    }
    // macOS Chinese IME fires compositionend BEFORE the confirm Enter keydown.
    if (composingRef.current || Date.now() - compositionEndedAtRef.current < 50) return;
    if (e.key === "Enter" && !e.shiftKey && !popup) {
      e.preventDefault();
      if (busy) {
        const text = draft.trim();
        if (text && onQueueWhileBusy) {
          onQueueWhileBusy(text);
          setChips([]);
        }
      } else if (!disabled && draft.trim()) {
        historyRef.current.push(draft.trim());
        if (historyRef.current.length > 100) historyRef.current.shift();
        setBrowseIdx(-1);
        onSend();
        setChips([]);
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
                <span key={i} className={`chip ${c.kind}`}>
                  {c.kind === "slash" ? (
                    <I.slash size={11} />
                  ) : (
                    <I.at size={11} />
                  )}
                  <span>{c.label}</span>
                  <span
                    className="x"
                    onClick={() =>
                      setChips((cs) => cs.filter((_, j) => j !== i))
                    }
                  >
                    <I.x size={10} />
                  </span>
                </span>
              ))}
            </div>
          ) : null}

          <textarea
            ref={textareaRef}
            value={draft}
            placeholder={t("composer.placeholder")}
            onChange={handleChange}
            onKeyDown={handleKeyDown}
            onCompositionStart={() => { composingRef.current = true; }}
            onCompositionEnd={() => {
              composingRef.current = false;
              compositionEndedAtRef.current = Date.now();
            }}
            rows={DEFAULT_COMPOSER_ROWS}
            disabled={disabled}
          />

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
                title={t("composer.switchPreset")}
              >
                <I.brain size={12} />
                <span>{modelLabel}</span>
                <span className="badge">{PRESET_INFO[preset].badge}</span>
                <I.chev size={10} />
              </button>
              {modelMenuOpen ? (
                <ModelMenu
                  current={preset}
                  onPick={(p) => {
                    onPresetChange(p);
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
                    historyRef.current.push(draft.trim());
                    if (historyRef.current.length > 100) historyRef.current.shift();
                    setBrowseIdx(-1);
                    onSend();
                    setChips([]);
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
                  const path = (item as MentionItem).name;
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
      el?.scrollIntoView({ block: "nearest" });
    });
  }, [activeIdx]);

  return (
    <div className="popup" onMouseDown={(e) => e.preventDefault()}>
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
      <div className="popup-list" ref={listRef}>
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
                  <span>{(it as MentionItem).name}</span>
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

function ModelMenu({
  current,
  onPick,
}: {
  current: PresetName;
  onPick: (p: PresetName) => void;
}) {
  const order: PresetName[] = ["flash", "pro"];
  return (
    <div
      className="popup"
      style={{
        bottom: "calc(100% + 6px)",
        left: "auto",
        right: 0,
        width: 260,
        position: "absolute",
      }}
    >
      <div className="ph">
        <span className="tok">M</span>
        <span>{t("composer.switchPreset")}</span>
      </div>
      <div className="popup-list">
        {order.map((p) => (
          <div
            key={p}
            className="popup-item"
            data-active={p === current}
            onClick={() => onPick(p)}
          >
            <span className="ico">
              <I.brain size={12} />
            </span>
            <div className="nm">
              <span className="cmd">{PRESET_INFO[p].label}</span>
              <div className="desc">{t(PRESET_INFO[p].desc)}</div>
            </div>
            <span className="kb">{PRESET_INFO[p].badge}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
