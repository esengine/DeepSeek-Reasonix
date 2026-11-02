import type { RuntimeView, TreeSession } from "../port/hub";

export type SessionScope = "all" | "live" | "pinned" | "archived";

// An archived conversation leaves every list but Archived — except that a
// search is a request to find it, so it answers from the whole list and the row
// says it is archived. A session 1.x wrote can be kept out of the lists the same
// way, unless it is running or being searched for.
export function inScope(session: TreeSession, scope: SessionScope, searching: boolean, live: boolean, pinned: boolean, hideLegacy = false): boolean {
  if (scope === "archived") return !!session.archived;
  if (session.archived) return searching && scope === "all";
  if (hideLegacy && session.legacy && !live && !searching) return false;
  if (scope === "live") return live;
  if (scope === "pinned") return pinned;
  return true;
}

// Whether a session belongs in the running list: while its turn runs, and for
// the grace the reader chose after the turn was seen to finish.
export function listedAsLive(live: boolean, finishedAt: number | undefined, now: number, graceMinutes: number): boolean {
  return live || (graceMinutes > 0 && finishedAt !== undefined && now - finishedAt < graceMinutes * 60_000);
}

export function sessionRuntime(session: TreeSession, runtimes: RuntimeView[], host = ""): RuntimeView | undefined {
  return runtimes.find((runtime) => (runtime.host || "") === host &&
    (runtime.sessionPath === session.path || (!runtime.sessionPath && runtime.id === session.runtimeId)));
}
