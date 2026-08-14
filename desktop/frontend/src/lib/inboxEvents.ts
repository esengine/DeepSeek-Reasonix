export function onInboxChanged(cb: (tabId: string) => void): () => void {
  if (typeof window !== "undefined" && window.runtime && window.go?.main?.App) {
    return window.runtime.EventsOn("InboxChanged", (payload?: unknown) => {
      const tabId = payload && typeof payload === "object" && "tabId" in payload
        ? String((payload as { tabId?: unknown }).tabId ?? "")
        : "";
      cb(tabId);
    });
  }
  return () => {};
}
