import type { TreeSession } from "../port/hub";

export type SessionScope = "all" | "live" | "pinned" | "archived";

// An archived conversation leaves every list but Archived — except that a
// search is a request to find it, so it answers from the whole list and the row
// says it is archived.
export function inScope(session: TreeSession, scope: SessionScope, searching: boolean, live: boolean, pinned: boolean): boolean {
  if (scope === "archived") return !!session.archived;
  if (session.archived) return searching && scope === "all";
  if (scope === "live") return live;
  if (scope === "pinned") return pinned;
  return true;
}
