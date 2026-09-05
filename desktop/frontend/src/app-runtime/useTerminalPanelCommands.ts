import { useCommittedCommand } from "../lib/useCommittedCommand";
import { useGlobalShortcut } from "../lib/keyboardShortcuts";
import { useLayoutStore, saveTerminalPanelOpen } from "../store/layout";
import { useOverlayStore } from "../store/overlays";
import { useTerminalStore } from "../store/terminal";

function showTerminal() {
  useOverlayStore.getState().setMainView("chat");
  useLayoutStore.getState().setTerminalPanelOpen(true);
  saveTerminalPanelOpen(true);
}

/** Commands and shortcuts share the same committed capability boundary. */
export function useTerminalPanelCommands(input: { tabId?: string; enabled: boolean }) {
  const toggleTerminalPanel = useCommittedCommand(() => {
    if (!input.enabled) return;
    if (useOverlayStore.getState().mainView === "automation") { showTerminal(); return; }
    const next = !useLayoutStore.getState().terminalPanelOpen;
    useLayoutStore.getState().setTerminalPanelOpen(next);
    saveTerminalPanelOpen(next);
  });
  const openTerminalForPath = useCommittedCommand((path = ".") => {
    if (!input.enabled) return;
    showTerminal();
    if (input.tabId) void useTerminalStore.getState().createSession(input.tabId, path || ".", "default").catch(() => {});
  });
  const newTerminalSession = useCommittedCommand(() => {
    if (input.tabId) openTerminalForPath();
  });
  const closeTerminalPanel = useCommittedCommand(() => {
    useLayoutStore.getState().setTerminalPanelOpen(false);
    saveTerminalPanelOpen(false);
  });
  useGlobalShortcut("terminal.toggle", toggleTerminalPanel);
  useGlobalShortcut("terminal.newSession", newTerminalSession);
  return { toggleTerminalPanel, openTerminalForPath, closeTerminalPanel };
}
