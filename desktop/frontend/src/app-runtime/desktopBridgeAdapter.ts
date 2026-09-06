import { app } from "../lib/bridge";

/** Runtime-only bridge ports used by App owners; presentation never imports Wails directly. */
export const desktopBridge = {
  setRemoteTabComposerProfile: (tabId: string, mode: string, approvalMode: string, goal: string) =>
    app.SetRemoteTabComposerProfile(tabId, mode, approvalMode, goal),
  getTopicSummary: (request: Parameters<typeof app.GetTopicSummary>[0]) => app.GetTopicSummary(request),
  cancelJobForTab: (tabId: string, jobId: string) => app.CancelJobForTab(tabId, jobId),
  dismissTodoBatchForTab: (tabId: string, batchKey: string) => app.DismissTodoBatchForTab(tabId, batchKey),
  clearRemoteTabSession: (tabId: string) => app.ClearRemoteTabSession(tabId),
  terminalOutputForTab: (tabId: string, sessionId: string) => app.TerminalOutputForTab(tabId, sessionId),
  acceptDeliveryToTab: (tabId: string) => app.AcceptDeliveryToTab(tabId),
  disconnectRemoteHost: (hostId: string) => app.DisconnectRemoteHost(hostId),
  openRemoteProjectTab: app.OpenRemoteProjectTab,
  listTabs: app.ListTabs,
  openTaskSessionForTab: app.OpenTaskSessionForTab,
  listSessionsForTab: app.ListSessionsForTab,
  closeMergedWorktreeTab: app.CloseMergedWorktreeTab,
  finalizeWorktreeMerge: app.FinalizeWorktreeMerge,
};
