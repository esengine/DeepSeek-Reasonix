import {
  ClipboardCopy,
  Download,
  FilePlus,
  FocusIcon,
  FolderOpen,
  Info,
  Plus,
  Search,
  Settings,
  SquareX,
  StopCircle,
  Trash2,
} from "lucide-react";
import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { t, useLang } from "./i18n";

export type CommandGroup = "nav" | "action" | "workspace" | "settings";

export type Command = {
  id: string;
  label: string;
  hint?: string;
  icon: ReactNode;
  shortcut?: string[];
  group: CommandGroup;
  run: () => void;
};

export function useCommandPalette(active: boolean = true) {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    // Skip in background tabs — each TabRuntime calls this hook, so without the gate Cmd+K toggles every tab's palette at once.
    if (!active) return;
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
  }, [active]);
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
      group: "nav",
      label: t("palette.newChat"),
      hint: t("palette.newChatHint"),
      icon: <FilePlus size={13} />,
      shortcut: ["⌘", "N"],
      run: handlers.newChat,
    },
    {
      id: "new-tab",
      group: "nav",
      label: t("palette.newTab"),
      hint: t("palette.newTabHint"),
      icon: <Plus size={13} />,
      shortcut: ["⌘", "T"],
      run: handlers.newTab,
    },
  ];
  if (handlers.canCloseTab) {
    list.push({
      id: "close-tab",
      group: "nav",
      label: t("palette.closeTab"),
      hint: t("palette.closeTabHint"),
      icon: <SquareX size={13} />,
      shortcut: ["⌘", "W"],
      run: handlers.closeTab,
    });
  }
  list.push({
    id: "focus-composer",
    group: "nav",
    label: t("palette.focusComposer"),
    icon: <FocusIcon size={13} />,
    shortcut: ["⌘", "L"],
    run: handlers.focusComposer,
  });
  if (handlers.busy) {
    list.push({
      id: "abort",
      group: "action",
      label: t("palette.abort"),
      hint: t("palette.abortHint"),
      icon: <StopCircle size={13} />,
      shortcut: ["esc"],
      run: handlers.abort,
    });
  }
  if (handlers.hasMessages) {
    list.push({
      id: "copy-last",
      group: "action",
      label: t("palette.copyLast"),
      hint: t("palette.copyLastHint"),
      icon: <ClipboardCopy size={13} />,
      run: handlers.copyLast,
    });
    list.push({
      id: "export-md",
      group: "action",
      label: t("palette.exportMd"),
      hint: t("palette.exportMdHint"),
      icon: <Download size={13} />,
      run: handlers.exportMarkdown,
    });
    list.push({
      id: "clear-chat",
      group: "action",
      label: t("palette.clearChat"),
      hint: t("palette.clearChatHint"),
      icon: <Trash2 size={13} />,
      run: handlers.clearChat,
    });
  }
  list.push({
    id: "pick-workspace",
    group: "workspace",
    label: t("palette.pickWorkspace"),
    hint: t("palette.pickWorkspaceHint"),
    icon: <FolderOpen size={13} />,
    run: handlers.pickWorkspace,
  });
  list.push({
    id: "settings",
    group: "settings",
    label: t("palette.settings"),
    hint: t("palette.settingsHint"),
    icon: <Settings size={13} />,
    run: handlers.openSettings,
  });
  list.push({
    id: "about",
    group: "settings",
    label: t("palette.about"),
    icon: <Info size={13} />,
    run: handlers.about,
  });
  return list;
}

const GROUP_ORDER: CommandGroup[] = ["nav", "action", "workspace", "settings"];

function groupLabel(g: CommandGroup): string {
  switch (g) {
    case "nav": return t("palette.groupNav");
    case "action": return t("palette.groupAction");
    case "workspace": return t("palette.groupWorkspace");
    case "settings": return t("palette.groupSettings");
  }
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

  const grouped = useMemo(() => {
    const byGroup = new Map<CommandGroup, Command[]>();
    for (const c of filtered) {
      const arr = byGroup.get(c.group) ?? [];
      arr.push(c);
      byGroup.set(c.group, arr);
    }
    return GROUP_ORDER
      .map((g) => ({ group: g, items: byGroup.get(g) ?? [] }))
      .filter((s) => s.items.length > 0);
  }, [filtered]);

  if (!open) return null;

  const run = (cmd: Command) => {
    cmd.run();
    onClose();
  };

  return (
    <div className="cmdk-mask" onMouseDown={onClose}>
      <div className="cmdk" onMouseDown={(e) => e.stopPropagation()}>
        <div className="cmdk-head">
          <Search size={14} />
          <input
            ref={inputRef}
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
          <span className="hint">
            <kbd>esc</kbd>
          </span>
        </div>
        <div className="cmdk-body" ref={listRef}>
          {filtered.length === 0 ? (
            <div className="cmdk-empty">{t("palette.empty")}</div>
          ) : null}
          {grouped.map((section) => (
            <div className="cmdk-group" key={section.group}>
              <div className="cmdk-gh">{groupLabel(section.group)}</div>
              {section.items.map((c) => {
                const i = filtered.indexOf(c);
                return (
                  <div
                    key={c.id}
                    data-idx={i}
                    className="cmdk-row"
                    data-active={i === active}
                    onMouseEnter={() => setActive(i)}
                    onClick={() => run(c)}
                  >
                    <span className="ic">{c.icon}</span>
                    <span className="l">{c.label}</span>
                    <span className="g">{groupLabel(c.group)}</span>
                    {c.shortcut ? (
                      <span className="kb">{c.shortcut.join("")}</span>
                    ) : (
                      <span className="kb-empty" />
                    )}
                  </div>
                );
              })}
            </div>
          ))}
        </div>
        <div className="cmdk-foot">
          <span>
            <kbd>↑↓</kbd>
            {t("palette.footMove")}
          </span>
          <span>
            <kbd>⏎</kbd>
            {t("palette.footRun")}
          </span>
          <span>
            <kbd>esc</kbd>
            {t("palette.footClose")}
          </span>
          <span style={{ marginLeft: "auto", color: "var(--muted)" }}>
            {filtered.length} {t("palette.countSuffix")}
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
