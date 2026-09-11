import type { RuntimeProjection, RuntimeState, createRuntimeStateStore } from "./runtimeStateStore";

function validState(state: RuntimeState) {
  if (!state || state.schemaVersion !== 1) return Boolean(state);
  return Boolean(state.runtimeEpoch) && Number.isSafeInteger(state.revision) && state.revision > 0
    && ["idle", "executing", "finishing", "closed"].includes(state.phase)
    && [state.running, state.pendingPrompt, state.cancelRequested, state.cancellable].every(value => typeof value === "boolean")
    && Number.isSafeInteger(state.backgroundJobs) && state.backgroundJobs >= 0;
}

export function acceptRuntimeState(store: ReturnType<typeof createRuntimeStateStore>, next: RuntimeProjection, authoritative = false): "accepted" | "duplicate" | "stale" | "conflict" {
  const snapshot = store.getSnapshot();
  if (!next?.epoch || !Number.isSafeInteger(next.revision) || !Array.isArray(next.sessions) || !Array.isArray(next.topics)) return "stale";
  if (snapshot && snapshot.epoch !== next.epoch && !authoritative) return "conflict";
  if (snapshot?.epoch === next.epoch) {
    if (next.revision < snapshot.revision) return "stale";
    if (next.revision === snapshot.revision) {
      if (JSON.stringify(next) !== JSON.stringify(snapshot)) return "conflict";
      store.commit(snapshot);
      return "duplicate";
    }
  }
  if (next.sessions.some(session => !validState(session?.state))) return "conflict";
  // Preserve per-session identity across unrelated updates.
  const old = new Map(snapshot?.sessions.map(session => [`${session.tabId}\0${session.sessionPath}`, session]));
  const committed = { ...next, topics: structuredClone(next.topics), sessions: next.sessions.map(session => {
    const previous = old.get(`${session.tabId}\0${session.sessionPath}`);
    return previous && JSON.stringify(previous) === JSON.stringify(session) ? previous : Object.freeze({ ...session, state: Object.freeze({ ...session.state }) });
  }) };
  store.commit(committed);
  return "accepted";
}
