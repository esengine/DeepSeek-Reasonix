export type InboxChangedEvent = {
  tabId: string;
  sessionPath?: string;
  revision?: number;
};

export function onInboxChanged(cb: (event: InboxChangedEvent) => void): () => void {
  if (typeof window !== "undefined" && window.runtime && window.go?.main?.App) {
    return window.runtime.EventsOn("InboxChanged", (payload?: unknown) => {
      const tabId = payload && typeof payload === "object" && "tabId" in payload
        ? String((payload as { tabId?: unknown }).tabId ?? "")
        : "";
      const sessionPath = payload && typeof payload === "object" && "sessionPath" in payload
        ? String((payload as { sessionPath?: unknown }).sessionPath ?? "")
        : undefined;
      const rawRevision = payload && typeof payload === "object" && "revision" in payload
        ? Number((payload as { revision?: unknown }).revision)
        : NaN;
      cb({ tabId, sessionPath, revision: Number.isFinite(rawRevision) ? rawRevision : undefined });
    });
  }
  return () => {};
}
