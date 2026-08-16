import { AlertTriangle, MessageSquare, MessageSquarePlus, PanelBottomClose, Plus, RefreshCw, TerminalSquare, X } from "lucide-react";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";

import {
  detectShortcutPlatform,
  formatShortcutCombo,
  onShortcutsChanged,
  resolvedShortcutCombo,
  useGlobalShortcut,
} from "../lib/keyboardShortcuts";
import { useT } from "../lib/i18n";
import { startTerminalEventBridge } from "../lib/terminalEvents";
import { useTerminalStore } from "../store/terminal";
import { TerminalSessionRail } from "./TerminalSessionRail";
import { TerminalView, type TerminalSelectionAction, type TerminalViewHandle } from "./TerminalView";

const SELECTION_ACTION_EDGE_GAP = 8;

export function TerminalPanel({
  tabId,
  cwd,
  readOnly,
  open,
  fitEnabled = true,
  onClose,
  onAddOutput,
  onAddToChat,
}: {
  tabId: string;
  cwd?: string;
  readOnly: boolean;
  open: boolean;
  fitEnabled?: boolean;
  onClose: () => void;
  onAddOutput: (sessionId: string) => void;
  onAddToChat: (text: string) => void;
}) {
  const t = useT();
  const [selectedShellId, setSelectedShellId] = useState("default");
  const [selectionAction, setSelectionAction] = useState<TerminalSelectionAction | null>(null);
  const [actionPoint, setActionPoint] = useState<{ left: number; top: number } | null>(null);
  const selectionActionRef = useRef<HTMLDivElement>(null);
  const terminalViewRef = useRef<TerminalViewHandle>(null);
  // App can render the new tab before this panel's sync effect advances the
  // process-wide terminal store. Never expose the previous tab's terminal in
  // that gap: even one painted frame could route input or a stale overlay to
  // the new tab with the old session id.
  const workspace = useTerminalStore((state) => state.tabId === tabId ? state.workspace : null);
  const loading = useTerminalStore((state) => state.tabId === tabId ? state.loading : true);
  const error = useTerminalStore((state) => state.tabId === tabId ? state.error : null);
  const activeSessionId = useTerminalStore((state) => state.tabId === tabId ? state.activeSessionId : null);
  const syncWorkspace = useTerminalStore((state) => state.syncWorkspace);
  const ensureReady = useTerminalStore((state) => state.ensureReady);
  const createSession = useTerminalStore((state) => state.createSession);
  const closeSession = useTerminalStore((state) => state.closeSession);
  const clearError = useTerminalStore((state) => state.clearError);
  const setActiveSession = useTerminalStore((state) => state.setActiveSession);
  const capabilityRef = useRef({ tabId, readOnly });
  const shortcutPlatform = useMemo(() => detectShortcutPlatform(), []);
  const [shortcutRevision, setShortcutRevision] = useState(0);
  useEffect(() => onShortcutsChanged(() => setShortcutRevision((value) => value + 1)), []);
  const addShortcut = useMemo(
    () => formatShortcutCombo(resolvedShortcutCombo("selection.addToChat", shortcutPlatform), shortcutPlatform),
    // shortcutRevision re-resolves the combo after the user changes it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [shortcutPlatform, shortcutRevision],
  );

  useLayoutEffect(() => {
    if (!selectionAction) {
      setActionPoint(null);
      return;
    }
    const rect = selectionActionRef.current?.getBoundingClientRect();
    if (!rect) {
      setActionPoint(selectionAction.point);
      return;
    }
    setActionPoint({
      left: Math.min(
        Math.max(SELECTION_ACTION_EDGE_GAP, selectionAction.point.left),
        Math.max(SELECTION_ACTION_EDGE_GAP, window.innerWidth - rect.width - SELECTION_ACTION_EDGE_GAP),
      ),
      top: Math.min(
        Math.max(SELECTION_ACTION_EDGE_GAP, selectionAction.point.top),
        Math.max(SELECTION_ACTION_EDGE_GAP, window.innerHeight - rect.height - SELECTION_ACTION_EDGE_GAP),
      ),
    });
  }, [selectionAction]);

  // The floating action only outlives the gesture when the terminal keeps its
  // focus: Escape, an outside click, a resize, or a scroll dismisses it.
  useEffect(() => {
    if (!selectionAction) return;
    const close = () => setSelectionAction(null);
    const onPointerDown = (event: PointerEvent) => {
      if (event.target instanceof Node && selectionActionRef.current?.contains(event.target)) return;
      setSelectionAction(null);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setSelectionAction(null);
    };
    document.addEventListener("pointerdown", onPointerDown, true);
    document.addEventListener("keydown", onKeyDown);
    window.addEventListener("resize", close);
    window.addEventListener("scroll", close, true);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown, true);
      document.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("resize", close);
      window.removeEventListener("scroll", close, true);
    };
  }, [selectionAction]);

  useEffect(() => {
    if (!open) setSelectionAction(null);
  }, [open]);

  const addSelectionToChat = useCallback(() => {
    if (!selectionAction) return;
    onAddToChat(selectionAction.text);
    terminalViewRef.current?.clearSelection();
    setSelectionAction(null);
  }, [onAddToChat, selectionAction]);

  useGlobalShortcut(
    "selection.addToChat",
    addSelectionToChat,
    [],
    open && Boolean(selectionAction),
  );

  useEffect(() => {
    startTerminalEventBridge();
    const previous = capabilityRef.current;
    const capabilityChanged = previous.tabId === tabId && previous.readOnly !== readOnly;
    capabilityRef.current = { tabId, readOnly };
    void syncWorkspace(tabId, capabilityChanged).catch(() => {});
  }, [readOnly, syncWorkspace, tabId]);

  useEffect(() => {
    if (workspace && !workspace.shells.some((shell) => shell.id === selectedShellId)) {
      setSelectedShellId("default");
    }
  }, [selectedShellId, workspace]);

  const newSession = useCallback(() => {
    void createSession(tabId, ".", selectedShellId).catch(() => {});
  }, [createSession, selectedShellId, tabId]);
  const sessions = workspace?.sessions ?? [];
  const shellOptions = workspace?.shells.length
    ? workspace.shells
    : [{ id: "default", label: t("terminal.defaultShell") }];
  const active = sessions.find((session) => session.id === activeSessionId) ?? sessions[0];
  const terminalReadOnly = readOnly || Boolean(workspace?.readOnly);

  useLayoutEffect(() => {
    setSelectionAction(null);
  }, [active?.id, tabId]);

  return (
    <section className="terminal-panel" aria-label={t("terminal.title")}>
      <header className="terminal-panel__header">
        <div className="terminal-panel__identity"><TerminalSquare size={15} /><strong>{t("terminal.title")}</strong>{cwd && <span title={cwd}>{cwd}</span>}</div>
        <div className="terminal-panel__actions">
          <select
            className="terminal-shell-select"
            value={selectedShellId}
            onChange={(event) => setSelectedShellId(event.target.value)}
            disabled={!workspace?.available || terminalReadOnly}
            aria-label={t("terminal.shell")}
            title={t("terminal.shell")}
          >
            {shellOptions.map((shell) => (
              <option key={shell.id} value={shell.id}>
                {shell.id === "default" ? t("terminal.defaultShell") : shell.label}
              </option>
            ))}
          </select>
          <button type="button" className="terminal-icon-button" onClick={newSession} disabled={!workspace?.available || terminalReadOnly} aria-label={t("terminal.newSession")} title={t("terminal.newSession")}><Plus size={15} /></button>
          <button type="button" className="terminal-icon-button" onClick={() => active && onAddOutput(active.id)} disabled={!active} aria-label={t("terminal.addOutput")} title={t("terminal.addOutput")}><MessageSquarePlus size={15} /></button>
          <button type="button" className="terminal-icon-button" onClick={onClose} aria-label={t("rightDock.collapse")} title={t("rightDock.collapse")}><PanelBottomClose size={15} /></button>
        </div>
      </header>
      {!workspace && loading ? (
        <div className="terminal-empty"><span className="terminal-empty__spinner" />{t("terminal.loading")}</div>
      ) : !workspace && error ? (
        <div className="terminal-empty terminal-empty--error" role="alert">
          <AlertTriangle size={18} />
          <strong>{error}</strong>
          <button type="button" className="btn btn--secondary btn--small" onClick={() => { clearError(); void ensureReady(tabId).catch(() => {}); }}>
            <RefreshCw size={14} />{t("terminal.retry")}
          </button>
        </div>
      ) : !workspace ? (
        <div className="terminal-empty"><AlertTriangle size={18} /><strong>{t("terminal.loading")}</strong></div>
      ) : !workspace.available || terminalReadOnly ? (
        <div className="terminal-empty"><AlertTriangle size={18} /><strong>{workspace.reason || t("terminal.readOnly")}</strong></div>
      ) : (
        <div className="terminal-panel__body">
          {error && (
            <div className="terminal-error" role="alert">
              <AlertTriangle size={14} />
              <span>{error}</span>
              <button type="button" className="terminal-icon-button" onClick={clearError} aria-label={t("terminal.dismissError")} title={t("terminal.dismissError")}><X size={13} /></button>
            </div>
          )}
          {sessions.length > 0 && (
            <TerminalSessionRail
              sessions={sessions}
              activeSessionId={active?.id ?? null}
              onSelect={setActiveSession}
              onClose={(id) => void closeSession(tabId, id).catch(() => {})}
            />
          )}
          <div className="terminal-panel__content">
            {active ? <TerminalView key={active.id} ref={terminalViewRef} tabId={tabId} session={active} open={open} fitEnabled={fitEnabled} onSelectionActionChange={setSelectionAction} onAddToChat={onAddToChat} /> : (
              <div className="terminal-empty terminal-empty--action"><TerminalSquare size={22} /><p>{t("terminal.empty")}</p><button type="button" className="btn btn--secondary btn--small" onClick={newSession}><Plus size={14} />{t("terminal.newSession")}</button></div>
            )}
          </div>
        </div>
      )}
      {open && selectionAction && typeof document !== "undefined" && createPortal(
        <div
          ref={selectionActionRef}
          className="transcript-selection-action"
          role="toolbar"
          aria-label={t("selection.actions")}
          style={{
            left: actionPoint?.left ?? selectionAction.point.left,
            top: actionPoint?.top ?? selectionAction.point.top,
            visibility: actionPoint ? "visible" : "hidden",
          }}
          onMouseDown={(event) => event.preventDefault()}
        >
          <button type="button" onClick={addSelectionToChat}>
            <MessageSquare size={14} aria-hidden="true" />
            <span>{t("selection.addToChat")}</span>
            <kbd>{addShortcut}</kbd>
          </button>
        </div>,
        document.body,
      )}
    </section>
  );
}
