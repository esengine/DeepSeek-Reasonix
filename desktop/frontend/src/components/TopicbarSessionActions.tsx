import { lazy, Suspense, useEffect, useRef, useState } from "react";
import { Activity, Download, TerminalSquare } from "lucide-react";
import { CopyButton } from "./CopyButton";
import { Tooltip } from "./Tooltip";
import { t } from "../lib/i18n";

const loadExportMenu = () => import("./TopicbarExportMenu");
const TopicbarExportMenu = lazy(async () => ({ default: (await loadExportMenu()).TopicbarExportMenu }));

export interface TopicbarSessionActionsProps {
  sessionHasContent: boolean;
  getSessionMarkdown: () => string | Promise<string>;
  exportSession: (format: "markdown" | "json" | "pdf" | "image") => void;
  toggleTerminal: () => void;
  terminalEnabled?: boolean;
  terminalOpen: boolean;
  prefetchTerminal?: () => void;
  openSessionSummary: () => void;
  tasksOpen: boolean;
}

const actionClass = "topicbar__action-btn topicbar__action-btn--icon topicbar__action-btn--utility";

/** Keep session actions directly accessible; only export formats need a menu. */
export function TopicbarSessionActions({
  sessionHasContent, getSessionMarkdown, exportSession,
  toggleTerminal, terminalEnabled = true, terminalOpen, prefetchTerminal,
  openSessionSummary, tasksOpen,
}: TopicbarSessionActionsProps) {
  const exportRootRef = useRef<HTMLDivElement>(null);
  const exportTriggerRef = useRef<HTMLButtonElement>(null);
  const [exportFocus, setExportFocus] = useState<"first" | "last" | null>(null);
  const exportVisible = exportFocus !== null && sessionHasContent;

  const closeExport = (restoreFocus: boolean) => {
    setExportFocus(null);
    if (restoreFocus) exportTriggerRef.current?.focus();
  };

  useEffect(() => {
    if (!exportVisible) return;
    const dismissOutside = (event: Event) => {
      if (!exportRootRef.current?.contains(event.target as Node)) setExportFocus(null);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      setExportFocus(null);
      exportTriggerRef.current?.focus();
    };
    document.addEventListener("pointerdown", dismissOutside);
    document.addEventListener("focusin", dismissOutside);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("pointerdown", dismissOutside);
      document.removeEventListener("focusin", dismissOutside);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [exportVisible]);

  return (
    <>
      <Tooltip label={t("topicBar.copyAll")}>
        <CopyButton getText={getSessionMarkdown} label={t("topicBar.copyAll")} className={actionClass} showInlineLabel={false} />
      </Tooltip>
      <div ref={exportRootRef} className={`topicbar__export${exportVisible ? " topicbar__export--open" : ""}`}>
        <Tooltip label={t("topicBar.export")}>
          <button
            ref={exportTriggerRef} className={actionClass} type="button"
            aria-label={t("topicBar.export")} aria-haspopup="menu" aria-expanded={exportVisible}
            disabled={!sessionHasContent}
            onPointerEnter={() => { void loadExportMenu(); }}
            onFocus={() => { void loadExportMenu(); }}
            onKeyDown={(event) => {
              if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
              event.preventDefault();
              setExportFocus(event.key === "ArrowUp" ? "last" : "first");
            }}
            onClick={() => setExportFocus((focus) => focus ? null : "first")}
          >
            <Download size={14} />
          </button>
        </Tooltip>
        {exportVisible && (
          <Suspense fallback={null}>
            <TopicbarExportMenu initialFocus={exportFocus!} exportSession={exportSession} onClose={closeExport} />
          </Suspense>
        )}
      </div>
      <Tooltip label={t("rightDock.terminal")}>
        <button
          className={actionClass} type="button" aria-label={t("rightDock.terminal")}
          aria-pressed={terminalOpen} disabled={!terminalEnabled}
          onPointerEnter={terminalEnabled ? prefetchTerminal : undefined}
          onFocus={terminalEnabled ? prefetchTerminal : undefined}
          onClick={toggleTerminal}
        >
          <TerminalSquare size={14} />
        </button>
      </Tooltip>
      <Tooltip label={t("summary.session")}>
        <button className={actionClass} type="button" aria-label={t("summary.session")} aria-expanded={tasksOpen} onClick={openSessionSummary}>
          <Activity size={14} />
        </button>
      </Tooltip>
    </>
  );
}
