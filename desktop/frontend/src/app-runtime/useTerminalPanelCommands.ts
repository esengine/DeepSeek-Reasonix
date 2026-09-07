import { useCommittedCommand } from "../lib/useCommittedCommand";
import { useGlobalShortcut } from "../lib/keyboardShortcuts";
import { useLayoutStore, saveTerminalPanelOpen } from "../store/layout";
import { useTerminalStore } from "../store/terminal";

function showTerminal() {
  useLayoutStore.getState().setTerminalPanelOpen(true);
  saveTerminalPanelOpen(true);
}

/** Commands and shortcuts share the same committed capability boundary. */
export function useTerminalPanelCommands(input: { tabId?: string; enabled: boolean; shortcutsEnabled?: boolean }) {
  const toggleTerminalPanel = useCommittedCommand(() => {
    if (!input.enabled) return;
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
  useGlobalShortcut("terminal.toggle", toggleTerminalPanel, [toggleTerminalPanel], input.shortcutsEnabled !== false);
  useGlobalShortcut("terminal.newSession", newTerminalSession, [newTerminalSession], input.shortcutsEnabled !== false);
  return { toggleTerminalPanel, openTerminalForPath, closeTerminalPanel };
}
