import { useEffect, useSyncExternalStore } from "react";
import { app } from "./bridge";
import { runtimeStateStore, selectRuntime } from "./runtimeStateStore";

export function useRuntimeSession(tabId?: string, sessionPath?: string) {
  const snapshot = useSyncExternalStore(runtimeStateStore.subscribe, runtimeStateStore.getSnapshot);
  const failed = useSyncExternalStore(runtimeStateStore.subscribe, runtimeStateStore.getFailed);
  const session = snapshot?.sessions.find(session => session.open && session.tabId === tabId && (!sessionPath || session.sessionPath === sessionPath));
  return selectRuntime(session, failed);
}

export function useRuntimeStateSync() {
  useEffect(() => {
    if (!app.GetRuntimeStateSnapshot) return;
    let disposed = false;
    let stop: (() => void) | undefined;
    void import("./runtimeStateSync").then(({ startAppRuntimeStateSync }) => {
      if (!disposed) stop = startAppRuntimeStateSync();
    });
    return () => { disposed = true; stop?.(); };
  }, []);
}
