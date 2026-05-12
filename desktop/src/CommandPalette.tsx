import { CornerDownLeft, FilePlus, FocusIcon, Info, Settings, Sparkles, Trash2 } from "lucide-react";
import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";

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

export function buildCommands(handlers: {
  newChat: () => void;
  clearChat: () => void;
  focusComposer: () => void;
  openSettings: () => void;
  about: () => void;
}): Command[] {
  return [
    {
      id: "new-chat",
      label: "New chat",
      hint: "丢掉当前对话，开新一轮",
      icon: <FilePlus size={14} />,
      shortcut: ["⌘", "N"],
      run: handlers.newChat,
    },
    {
      id: "clear-chat",
      label: "Clear messages",
      hint: "只清当前 UI，subprocess 不重启",
      icon: <Trash2 size={14} />,
      run: handlers.clearChat,
    },
    {
      id: "focus-composer",
      label: "Focus composer",
      icon: <FocusIcon size={14} />,
      shortcut: ["⌘", "L"],
      run: handlers.focusComposer,
    },
    {
      id: "settings",
      label: "Settings",
      hint: "即将上线",
      icon: <Settings size={14} />,
      run: handlers.openSettings,
    },
    {
      id: "about",
      label: "About Reasonix",
      icon: <Info size={14} />,
      run: handlers.about,
    },
  ];
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
            placeholder="搜索命令…"
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
          {filtered.length === 0 && <div className="palette-empty">没匹配到</div>}
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
            移动
          </span>
          <span className="kbd-group">
            <span className="kbd">↵</span>
            执行
          </span>
          <span className="kbd-group">
            <span className="kbd">esc</span>
            关闭
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
