import {
  ClipboardCopy,
  CornerDownLeft,
  Download,
  FilePlus,
  FocusIcon,
  FolderOpen,
  Info,
  Plus,
  Settings,
  Sparkles,
  SquareX,
  StopCircle,
  Trash2,
} from "lucide-react";
import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { t, useLang } from "./i18n";

export type Command = {
  id: string;
  label: string;
  hint?: string;
  icon: ReactNode;
  shortcut?: string[];
  run: () => void;
};

export function useCommandPalette() {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.ctrlKey || e.metaKey;
      if (mod && (e.key === "k" || e.key === "K")) {
        e.preventDefault();
        setOpen((v) => !v);
      } else if (e.key === "Escape") {
        setOpen(false);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  return { open, setOpen };
}

export type CommandHandlers = {
  newChat: () => void;
  clearChat: () => void;
  focusComposer: () => void;
  openSettings: () => void;
  about: () => void;
  abort: () => void;
  copyLast: () => void;
  exportMarkdown: () => void;
  pickWorkspace: () => void;
  newTab: () => void;
  closeTab: () => void;
  busy: boolean;
  canCloseTab: boolean;
  hasMessages: boolean;
};

export function buildCommands(handlers: CommandHandlers): Command[] {
  const list: Command[] = [
    {
      id: "new-chat",
      label: t("palette.newChat"),
      hint: t("palette.newChatHint"),
      icon: <FilePlus size={14} />,
      shortcut: ["⌘", "N"],
      run: handlers.newChat,
    },
    {
      id: "new-tab",
      label: t("palette.newTab"),
      hint: t("palette.newTabHint"),
      icon: <Plus size={14} />,
      shortcut: ["⌘", "T"],
      run: handlers.newTab,
    },
  ];
  if (handlers.canCloseTab) {
    list.push({
      id: "close-tab",
      label: t("palette.closeTab"),
      hint: t("palette.closeTabHint"),
      icon: <SquareX size={14} />,
      shortcut: ["⌘", "W"],
      run: handlers.closeTab,
    });
  }
  if (handlers.busy) {
    list.push({
      id: "abort",
      label: t("palette.abort"),
      hint: t("palette.abortHint"),
      icon: <StopCircle size={14} />,
      shortcut: ["esc"],
      run: handlers.abort,
    });
  }
  if (handlers.hasMessages) {
    list.push({
      id: "copy-last",
      label: t("palette.copyLast"),
      hint: t("palette.copyLastHint"),
      icon: <ClipboardCopy size={14} />,
      run: handlers.copyLast,
    });
    list.push({
      id: "export-md",
      label: t("palette.exportMd"),
      hint: t("palette.exportMdHint"),
      icon: <Download size={14} />,
      run: handlers.exportMarkdown,
    });
    list.push({
      id: "clear-chat",
      label: t("palette.clearChat"),
      hint: t("palette.clearChatHint"),
      icon: <Trash2 size={14} />,
      run: handlers.clearChat,
    });
  }
  list.push({
    id: "focus-composer",
    label: t("palette.focusComposer"),
    icon: <FocusIcon size={14} />,
    shortcut: ["⌘", "L"],
    run: handlers.focusComposer,
  });
  list.push({
    id: "pick-workspace",
    label: t("palette.pickWorkspace"),
    hint: t("palette.pickWorkspaceHint"),
    icon: <FolderOpen size={14} />,
    run: handlers.pickWorkspace,
  });
  list.push({
    id: "settings",
    label: t("palette.settings"),
    hint: t("palette.settingsHint"),
    icon: <Settings size={14} />,
    run: handlers.openSettings,
  });
  list.push({
    id: "about",
    label: t("palette.about"),
    icon: <Info size={14} />,
    run: handlers.about,
  });
  return list;
}

export function CommandPalette({
  open,
  onClose,
  commands,
}: {
  open: boolean;
  onClose: () => void;
  commands: Command[];
}) {
  useLang();
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return commands;
    return commands.filter((c) =>
      [c.label, c.hint].filter(Boolean).join(" ").toLowerCase().includes(q),
    );
  }, [query, commands]);

  useEffect(() => {
    if (open) {
      setQuery("");
      setActive(0);
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [open]);

  useEffect(() => {
    setActive((a) => Math.min(a, Math.max(0, filtered.length - 1)));
  }, [filtered.length]);

  useEffect(() => {
    const el = listRef.current?.querySelector<HTMLElement>(`[data-idx="${active}"]`);
    el?.scrollIntoView({ block: "nearest" });
  }, [active]);

  if (!open) return null;

  const run = (cmd: Command) => {
    cmd.run();
    onClose();
  };

  return (
    <div className="palette-overlay" onMouseDown={onClose}>
      <div className="palette" onMouseDown={(e) => e.stopPropagation()}>
        <div className="palette-head">
          <Sparkles size={14} className="palette-glyph" />
          <input
            ref={inputRef}
            className="palette-input"
            placeholder={t("palette.searchPlaceholder")}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "ArrowDown") {
                e.preventDefault();
                setActive((a) => Math.min(a + 1, filtered.length - 1));
              } else if (e.key === "ArrowUp") {
                e.preventDefault();
                setActive((a) => Math.max(a - 1, 0));
              } else if (e.key === "Enter") {
                e.preventDefault();
                const cmd = filtered[active];
                if (cmd) run(cmd);
              }
            }}
          />
          <span className="kbd">esc</span>
        </div>
        <div className="palette-list" ref={listRef}>
          {filtered.length === 0 && <div className="palette-empty">{t("palette.empty")}</div>}
          {filtered.map((c, i) => (
            <button
              type="button"
              key={c.id}
              data-idx={i}
              className={`palette-item ${i === active ? "active" : ""}`}
              onMouseEnter={() => setActive(i)}
              onClick={() => run(c)}
            >
              <span className="palette-item-icon">{c.icon}</span>
              <span className="palette-item-label">
                <span>{c.label}</span>
                {c.hint && <span className="palette-item-hint">{c.hint}</span>}
              </span>
              {c.shortcut && (
                <span className="palette-item-kbd">
                  {c.shortcut.map((k) => (
                    <span key={k} className="kbd">
                      {k}
                    </span>
                  ))}
                </span>
              )}
              {i === active && !c.shortcut && (
                <CornerDownLeft size={12} className="palette-item-enter" />
              )}
            </button>
          ))}
        </div>
        <div className="palette-foot">
          <span className="kbd-group">
            <span className="kbd">↑</span>
            <span className="kbd">↓</span>
            {t("palette.footMove")}
          </span>
          <span className="kbd-group">
            <span className="kbd">↵</span>
            {t("palette.footRun")}
          </span>
          <span className="kbd-group">
            <span className="kbd">esc</span>
            {t("palette.footClose")}
          </span>
        </div>
      </div>
    </div>
  );
}

export function Toast({ message }: { message: string | null }) {
  if (!message) return null;
  return <div className="toast">{message}</div>;
}
