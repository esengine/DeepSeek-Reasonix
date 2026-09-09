import type { ProjectRuntimeTopic } from "./types";

export interface RuntimeState {
  schemaVersion: number;
  runtimeEpoch: string;
  revision: number;
  phase: "idle" | "executing" | "finishing" | "closed";
  running: boolean;
  turnId: string;
  turnStatus: string;
  turnEventSeq: number;
  pendingPrompt: boolean;
  cancelRequested: boolean;
  cancellable: boolean;
  backgroundJobs: number;
  activity: string;
}
export interface RuntimeSession {
  tabId: string;
  scope: string;
  workspaceRoot: string;
  topicId: string;
  sessionPath: string;
  sessionGeneration: number;
  open: boolean;
  remote: boolean;
  hostId?: string;
  freshness: "synced" | "unknown" | "syncing";
  state: RuntimeState;
}
export interface RuntimeProjection {
  epoch: string;
  revision: number;
  sessions: RuntimeSession[];
  topics: ProjectRuntimeTopic[];
}

export function selectRuntime(session?: RuntimeSession, failed = false) {
  const state = session?.state;
  const known = state?.schemaVersion === 1;
  const unknown = Boolean(session && (failed || session.freshness !== "synced"));
  const finishing = known && state.phase === "finishing";
  const kind = unknown ? "unknown" : !known ? "legacy" : finishing ? "finishing"
    : state.cancelRequested ? "cancelling" : state.pendingPrompt ? "waiting_confirmation"
    : state.phase === "executing" ? state.activity === "streaming" ? "streaming" : "thinking"
    : state.backgroundJobs > 0 ? "background_job" : "idle";
  return { kind, known, unknown, finishing, state,
    running: known ? state.running : undefined,
    cancellable: known ? !unknown && !finishing && state.cancellable && !state.cancelRequested : undefined,
    spinning: !unknown && (kind === "thinking" || kind === "streaming" || kind === "cancelling" || kind === "background_job"),
  };
}

export function createRuntimeStateStore() {
  let snapshot: RuntimeProjection | undefined;
  let failed = false;
  const listeners = new Set<() => void>();
  const notify = () => listeners.forEach(listener => listener());
  return {
    getSnapshot: () => snapshot,
    getFailed: () => failed,
    subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener); }; },
    fail() { if (!failed) { failed = true; notify(); } },
    commit(next: RuntimeProjection) {
      if (snapshot === next && !failed) return;
      snapshot = next;
      failed = false;
      notify();
    },
  };
}
export const runtimeStateStore = createRuntimeStateStore();
