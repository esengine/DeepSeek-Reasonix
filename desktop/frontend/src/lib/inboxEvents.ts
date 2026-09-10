import { desktopHost } from "./desktopHost";

export type InboxChangedEvent = {
  tabId: string;
  sessionPath?: string;
  revision?: number;
};

export function onInboxChanged(cb: (event: InboxChangedEvent) => void): () => void {
  const host = desktopHost();
  if (host.kind !== "none") {
    return host.events.on("InboxChanged", (payload?: unknown) => {
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
