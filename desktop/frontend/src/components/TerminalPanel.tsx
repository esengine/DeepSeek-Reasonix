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
import { clampTerminalSelectionPointToHost } from "../lib/terminalSelection";
import { useTerminalStore } from "../store/terminal";
import { TerminalSessionRail } from "./TerminalSessionRail";
import {
  TerminalView,
  type TerminalSelectionAction,
  type TerminalViewHandle,
} from "./TerminalView";

export function TerminalPanel({
  tabId,
  cwd,
  readOnly,
  fitEnabled = true,
  onClose,
  onAddOutput,
  onAddSelection,
}: {
  tabId: string;
  cwd?: string;
  readOnly: boolean;
  fitEnabled?: boolean;
  onClose: () => void;
  onAddOutput: (sessionId: string) => void;
  onAddSelection?: (text: string) => void;
}) {
  const t = useT();
  const [selectedShellId, setSelectedShellId] = useState("default");
  const [selectionAction, setSelectionAction] = useState<TerminalSelectionAction | null>(null);
  const [actionPoint, setActionPoint] = useState<{ left: number; top: number } | null>(null);
  const actionRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLElement>(null);
  const terminalViewRef = useRef<TerminalViewHandle>(null);
  const dismissedRef = useRef<string | null>(null);
  const workspace = useTerminalStore((state) => state.workspace);
  const loading = useTerminalStore((state) => state.loading);
  const error = useTerminalStore((state) => state.error);
  const activeSessionId = useTerminalStore((state) => state.activeSessionId);
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
    // shortcutRevision re-resolves the combo after the user rebinds it in settings.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [shortcutPlatform, shortcutRevision],
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

  const closeSelectionAction = useCallback(() => {
    setSelectionAction(null);
    setActionPoint(null);
  }, []);

  useEffect(() => {
    dismissedRef.current = null;
    closeSelectionAction();
  }, [activeSessionId, closeSelectionAction, tabId]);

  const handleSelectionActionChange = useCallback((action: TerminalSelectionAction | null) => {
    if (!action) {
      dismissedRef.current = null;
      closeSelectionAction();
      return;
    }
    if (dismissedRef.current === action.text) return;
    dismissedRef.current = null;
    setSelectionAction(action);
  }, [closeSelectionAction]);

  const addSelectionToChat = useCallback(() => {
    if (!selectionAction || !onAddSelection) return;
    const text = selectionAction.text;
    terminalViewRef.current?.clearSelection();
    closeSelectionAction();
    onAddSelection(text);
  }, [closeSelectionAction, onAddSelection, selectionAction]);

  useGlobalShortcut(
    "selection.addToChat",
    addSelectionToChat,
    [],
    Boolean(selectionAction) && Boolean(onAddSelection),
  );

  useLayoutEffect(() => {
    if (!selectionAction) {
      setActionPoint(null);
      return;
    }
    const panel = panelRef.current;
    const toolbar = actionRef.current?.getBoundingClientRect();
    if (!panel) {
      setActionPoint(selectionAction.point);
      return;
    }
    setActionPoint(clampTerminalSelectionPointToHost(
      selectionAction.point,
      panel,
      toolbar?.width ?? 160,
      toolbar?.height ?? 40,
    ));
  }, [selectionAction]);

  useEffect(() => {
    if (!selectionAction) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      dismissedRef.current = selectionAction.text;
      closeSelectionAction();
    };
    const close = () => closeSelectionAction();
    window.addEventListener("keydown", onKeyDown);
    window.addEventListener("resize", close);
    window.addEventListener("scroll", close, true);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("resize", close);
      window.removeEventListener("scroll", close, true);
    };
  }, [closeSelectionAction, selectionAction]);

  const newSession = useCallback(() => {
    void createSession(tabId, ".", selectedShellId).catch(() => {});
  }, [createSession, selectedShellId, tabId]);
  const sessions = workspace?.sessions ?? [];
  const shellOptions = workspace?.shells.length
    ? workspace.shells
    : [{ id: "default", label: t("terminal.defaultShell") }];
  const active = sessions.find((session) => session.id === activeSessionId) ?? sessions[0];
  const terminalReadOnly = readOnly || Boolean(workspace?.readOnly);

  return (
    <section ref={panelRef} className="terminal-panel" aria-label={t("terminal.title")}>
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
            {active ? (
              <TerminalView
                key={active.id}
                ref={terminalViewRef}
                tabId={tabId}
                session={active}
                fitEnabled={fitEnabled}
                onSelectionActionChange={onAddSelection ? handleSelectionActionChange : undefined}
              />
            ) : (
              <div className="terminal-empty terminal-empty--action"><TerminalSquare size={22} /><p>{t("terminal.empty")}</p><button type="button" className="btn btn--secondary btn--small" onClick={newSession}><Plus size={14} />{t("terminal.newSession")}</button></div>
            )}
          </div>
        </div>
      )}
      {selectionAction && onAddSelection && typeof document !== "undefined" && createPortal(
        <div
          ref={actionRef}
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
