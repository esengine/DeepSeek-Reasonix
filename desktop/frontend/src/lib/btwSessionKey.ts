export type BtwSessionKeyTab = {
  id?: string | null;
  scope?: string | null;
  workspaceRoot?: string | null;
  topicId?: string | null;
  sessionPath?: string | null;
};

export function btwSessionKeyForTab(tab: BtwSessionKeyTab | undefined | null, tabId?: string | null, fallbackSessionPath?: string | null): string {
  const id = String(tab?.id ?? tabId ?? "").trim();
  const scope = tab?.scope === "project" ? "project" : "global";
  const workspaceRoot = scope === "project" ? String(tab?.workspaceRoot ?? "").trim() : "";
  const sessionPath = String(tab?.sessionPath || fallbackSessionPath || "").trim();
  if (sessionPath) return ["btw-session", scope, workspaceRoot, sessionPath].join("\u0000");
  const topicId = String(tab?.topicId ?? "").trim();
  if (topicId) return ["btw-topic", scope, workspaceRoot, topicId].join("\u0000");
  return id ? ["btw-tab", id].join("\u0000") : "";
}

export function shouldMigrateBtwSessionKey(previousKey: string | undefined, nextKey: string | undefined): boolean {
  if (!previousKey || !nextKey || previousKey === nextKey) return false;
  const previousIsFallback = previousKey.startsWith("btw-topic\u0000") || previousKey.startsWith("btw-tab\u0000");
  return previousIsFallback && nextKey.startsWith("btw-session\u0000");
}
