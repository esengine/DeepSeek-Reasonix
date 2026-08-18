import { app } from "./bridge";

export function replayPendingPromptsForActiveTab(
  activeTabId: string | undefined,
  replay: (tabId: string) => Promise<void> = (tabId) => {
    // Older test/dev bindings fall back to the reconnect-compatible global call.
    const scopedReplay = app.ReplayPendingPromptsForTab;
    return typeof scopedReplay === "function" ? scopedReplay(tabId) : app.ReplayPendingPrompts();
  },
): void {
  if (!activeTabId) return;
  void replay(activeTabId).catch(() => {});
}
