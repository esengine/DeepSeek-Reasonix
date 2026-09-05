import type { SessionMeta } from "../lib/types";

export type HistoryScopeFilter = { scope: "global" | "project"; workspaceRoot: string };
export type HistoryViewState =
  | { kind: "history"; source: "scope"; filter: HistoryScopeFilter; sessions: SessionMeta[] }
  | { kind: "history"; source: "all"; sessions: SessionMeta[] };
export function sessionsForScope(sessions: SessionMeta[], filter: HistoryScopeFilter): SessionMeta[] {
  return filter.scope === "project"
    ? sessions.filter(session => session.scope === "project" && session.workspaceRoot === filter.workspaceRoot)
    : sessions.filter(session => (session.scope || "global") === "global");
}
export function refreshHistoryProjection(current: HistoryViewState | null, sessions: SessionMeta[]): HistoryViewState | null {
  if (!current || current.kind !== "history") return current;
  return { ...current, sessions: current.source === "scope" ? sessionsForScope(sessions, current.filter) : sessions };
}
