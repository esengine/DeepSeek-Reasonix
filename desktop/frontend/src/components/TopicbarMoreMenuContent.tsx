import { useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from "react";
import {
  Activity,
  Check,
  Copy,
  Download,
  FileDown,
  FileImage,
  FileJson,
  FileText,
  GitBranch,
  TerminalSquare,
} from "lucide-react";

import "./TopicbarMoreMenuContent.css";

import { writeClipboardText } from "../lib/clipboard";
import { t } from "../lib/i18n";

export interface TopicbarMoreMenuContentProps {
  sessionHasContent: boolean;
  getSessionMarkdown: () => string | Promise<string>;
  exportSession: (format: "markdown" | "json" | "pdf" | "image") => void;
  openChangedDock: () => void;
  toggleTerminal: () => void;
  terminalEnabled?: boolean;
  prefetchTerminal?: () => void;
  openSessionSummary: () => void;
  tasksOpen: boolean;
  copied: boolean;
  initialFocus: "first" | "last";
  onClose: (restoreFocus: boolean) => void;
  onCopied: () => void;
}

function enabledItems(menu: HTMLElement): HTMLButtonElement[] {
  return Array.from(menu.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'))
    .filter((item) => !item.disabled && item.closest('[role="menu"]') === menu);
}

function moveMenuFocus(event: ReactKeyboardEvent<HTMLDivElement>) {
  if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
  event.preventDefault();
  event.stopPropagation();
  const items = enabledItems(event.currentTarget);
  if (items.length === 0) return;
  const current = items.indexOf(document.activeElement as HTMLButtonElement);
  const next = event.key === "Home" ? 0
    : event.key === "End" ? items.length - 1
    : event.key === "ArrowDown" ? (current + 1 + items.length) % items.length
    : (current - 1 + items.length) % items.length;
  items[next]?.focus();
}

export function TopicbarMoreMenuContent({
  sessionHasContent,
  getSessionMarkdown,
  exportSession,
  openChangedDock,
  toggleTerminal,
  terminalEnabled = true,
  prefetchTerminal,
  openSessionSummary,
  tasksOpen,
  copied,
  initialFocus,
  onClose,
  onCopied,
}: TopicbarMoreMenuContentProps) {
  const menuRef = useRef<HTMLDivElement>(null);
  const exportTriggerRef = useRef<HTMLButtonElement>(null);
  const exportMenuRef = useRef<HTMLDivElement>(null);
  const [exportOpen, setExportOpen] = useState(false);

  useEffect(() => {
    const items = menuRef.current ? enabledItems(menuRef.current) : [];
    (initialFocus === "last" ? items[items.length - 1] : items[0])?.focus();
  }, [initialFocus]);

  useEffect(() => {
    if (!exportOpen || !exportMenuRef.current) return;
    enabledItems(exportMenuRef.current)[0]?.focus();
  }, [exportOpen]);

  const closeAndRun = (action: () => void) => {
    onClose(true);
    action();
  };

  const copySession = async () => {
    try {
      await writeClipboardText(await getSessionMarkdown());
    } catch {
      /* clipboard unavailable */
    }
    onCopied();
  };

  const onMainMenuKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      onClose(true);
      return;
    }
    moveMenuFocus(event);
  };

  const onExportMenuKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.key === "ArrowLeft" || event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      setExportOpen(false);
      exportTriggerRef.current?.focus();
      return;
    }
    moveMenuFocus(event);
  };

  return (
    <div ref={menuRef} className="topicbar__more-menu" role="menu" aria-label={t("topicBar.more")} onKeyDown={onMainMenuKeyDown}>
      <button tabIndex={-1} className="topicbar__menu-item" type="button" role="menuitem" onClick={() => closeAndRun(() => { void copySession(); })}>
        {copied ? <Check size={14} /> : <Copy size={14} />}
        <span>{copied ? t("msg.copied") : t("topicBar.copyAll")}</span>
      </button>
      <div className={`topicbar__more-export${exportOpen ? " topicbar__more-export--open" : ""}`}>
        <button
          ref={exportTriggerRef}
          tabIndex={-1}
          className="topicbar__menu-item"
          type="button"
          role="menuitem"
          aria-haspopup="menu"
          aria-expanded={exportOpen}
          disabled={!sessionHasContent}
          onKeyDown={(event) => {
            if (event.key !== "ArrowRight") return;
            event.preventDefault();
            event.stopPropagation();
            setExportOpen(true);
          }}
          onClick={() => setExportOpen((open) => !open)}
        >
          <Download size={14} />
          <span>{t("topicBar.export")}</span>
        </button>
        {exportOpen && (
          <div ref={exportMenuRef} className="topicbar__more-export-menu" role="menu" aria-label={t("topicBar.export")} onKeyDown={onExportMenuKeyDown}>
            <button tabIndex={-1} type="button" role="menuitem" onClick={() => closeAndRun(() => exportSession("markdown"))}>
              <FileText size={13} />
              <span>{t("topicBar.exportMarkdown")}</span>
            </button>
            <button tabIndex={-1} type="button" role="menuitem" onClick={() => closeAndRun(() => exportSession("json"))}>
              <FileJson size={13} />
              <span>{t("topicBar.exportJson")}</span>
            </button>
            <button tabIndex={-1} type="button" role="menuitem" onClick={() => closeAndRun(() => exportSession("pdf"))}>
              <FileDown size={13} />
              <span>{t("topicBar.exportPdf")}</span>
            </button>
            <button tabIndex={-1} type="button" role="menuitem" onClick={() => closeAndRun(() => exportSession("image"))}>
              <FileImage size={13} />
              <span>{t("topicBar.exportImage")}</span>
            </button>
          </div>
        )}
      </div>
      <button tabIndex={-1} className="topicbar__menu-item" type="button" role="menuitem" onClick={() => closeAndRun(openChangedDock)}>
        <GitBranch size={14} />
        <span>{t("workspace.changedTab")}</span>
      </button>
      <button
        tabIndex={-1}
        className="topicbar__menu-item"
        type="button"
        role="menuitem"
        disabled={!terminalEnabled}
        onPointerEnter={terminalEnabled ? prefetchTerminal : undefined}
        onFocus={terminalEnabled ? prefetchTerminal : undefined}
        onClick={() => closeAndRun(toggleTerminal)}
      >
        <TerminalSquare size={14} />
        <span>{t("rightDock.terminal")}</span>
      </button>
      <button
        tabIndex={-1}
        className={`topicbar__menu-item${tasksOpen ? " topicbar__menu-item--active" : ""}`}
        type="button"
        role="menuitem"
        aria-pressed={tasksOpen}
        onClick={() => closeAndRun(openSessionSummary)}
      >
        <Activity size={14} />
        <span>{t("summary.session")}</span>
      </button>
    </div>
  );
}
