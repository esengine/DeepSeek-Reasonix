import { app } from "./bridge";
import { desktopHost } from "./desktopHost";
import { acceptRuntimeState } from "./runtimeStateReducer";
import { runtimeStateStore, type RuntimeProjection } from "./runtimeStateStore";
export interface RuntimeSyncPorts {
  subscribe: (accept: (snapshot: RuntimeProjection) => void) => () => void;
  read: () => Promise<RuntimeProjection>;
  timer: (callback: () => void, delay: number) => unknown;
  clearTimer: (timer: unknown) => void;
  focus: (callback: () => void) => () => void;
  recover?: (callback: () => void) => () => void;
  diagnostic?: (data: { reason: string; revision?: number; stale: number; conflicts: number; failures: number }) => void;
}
export function startRuntimeStateSync(ports: RuntimeSyncPorts, store = runtimeStateStore) {
  let disposed = false, inFlight = false, failures = 0;
  let stale = 0, conflicts = 0;
  let recoveryVersion = 0, recoveryQueued = false;
  let timer: unknown;
  const note = (reason: string, revision?: number) => ports.diagnostic?.({ reason, revision, stale, conflicts, failures });
  const sync = async (reason = "initial") => {
    if (disposed || inFlight) return;
    inFlight = true;
    const version = recoveryVersion;
    ports.clearTimer(timer);
    const before = store.getSnapshot();
    try {
      const snapshot = await ports.read();
      if (disposed || version !== recoveryVersion) return;
      const result = acceptRuntimeState(store, snapshot, before === store.getSnapshot());
      if (result === "stale") { stale++; note("stale-read", snapshot.revision); }
      if (result === "conflict") { conflicts++; throw new Error("Runtime snapshot version conflict"); }
      failures = snapshot.sessions.some(session => session.remote && session.freshness !== "synced") ? failures + 1 : 0;
    } catch {
      if (!disposed && version === recoveryVersion) { failures++; store.fail(); note(reason); }
    } finally {
      inFlight = false;
      if (!disposed && recoveryQueued) {
        recoveryQueued = false;
        void sync("event-recovery");
      } else if (!disposed) timer = ports.timer(() => { void sync("periodic"); }, failures ? [5000, 10000, 20000, 30000][Math.min(failures - 1, 3)] : 30000);
    }
  };
  const off = ports.subscribe(snapshot => {
    if (disposed) return;
    const result = acceptRuntimeState(store, snapshot);
    if (result === "stale") { stale++; note("stale-event", snapshot.revision); }
    if (result === "conflict") { conflicts++; note("conflicting-event", snapshot.revision); void sync("conflicting-event"); }
  });
  const offFocus = ports.focus(() => { void sync("focus-or-connection"); });
  const offRecover = ports.recover?.(() => {
    recoveryVersion++;
    store.fail();
    if (inFlight) recoveryQueued = true;
    else void sync("event-recovery");
  });
  void sync();
  return () => { disposed = true; off(); offFocus(); offRecover?.(); ports.clearTimer(timer); };
}

export function startAppRuntimeStateSync() {
  return startRuntimeStateSync({
      diagnostic: data => console.debug("runtime synchronization", { source: "desktop-runtime", ...data }),
      subscribe: accept => desktopHost().events.on("runtime-state:changed", (snapshot: unknown) => accept(snapshot as RuntimeProjection)),
      read: async () => {
        if (!app.SyncRuntimeState) return app.GetRuntimeStateSnapshot!();
        try { return await app.SyncRuntimeState(); }
        catch { return app.GetRuntimeStateSnapshot!(); }
      },
      timer: (callback, delay) => window.setTimeout(callback, delay),
      clearTimer: timer => { if (timer !== undefined) window.clearTimeout(timer as number); },
      recover: callback => desktopHost().events.on("desktop:resync", callback),
      focus: callback => {
        const connections = new Map<string, string>();
        const off = desktopHost().events.on("remote-tab:updated", (payload: unknown) => {
          const tab = payload as { id?: string; remoteState?: string };
          if (tab.id && connections.get(tab.id) !== tab.remoteState) { connections.set(tab.id, tab.remoteState ?? ""); callback(); }
        });
        window.addEventListener("focus", callback);
        window.addEventListener("online", callback);
        return () => { off?.(); window.removeEventListener("focus", callback); window.removeEventListener("online", callback); };
      },
      });
}
