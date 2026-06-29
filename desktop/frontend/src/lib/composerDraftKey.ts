type ComposerDraftKeyTab = {
  id?: string;
  scope?: string;
  workspaceRoot?: string;
  topicId?: string;
  sessionPath?: string;
};

export function composerDraftKeyForTab(activeTab: ComposerDraftKeyTab | undefined, activeTabId?: string | null): string {
  if (!activeTab) return activeTabId ?? "";
  const scope = activeTab.scope === "project" ? "project" : "global";
  const workspaceRoot = scope === "project" ? activeTab.workspaceRoot || "" : "";
  const topicId = activeTab.topicId || "";
  const sessionPath = activeTab.sessionPath || "";
  if (topicId) {
    return ["session-topic", scope, workspaceRoot, topicId].join("\u0000");
  }
  if (sessionPath) {
    return ["session-path", scope, workspaceRoot, sessionPath].join("\u0000");
  }
  return ["tab", activeTab.id || activeTabId || ""].join("\u0000");
}
