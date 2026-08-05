import type { ControlResult, SessionMeta } from "./types";

export interface TaskMonitorNavigationDeps {
  tabID: string;
  taskID: string;
  intentSeq: number;
  isIntentCurrent: (intentSeq: number) => boolean;
  openTaskSessionForTab: (tabID: string, taskID: string) => Promise<ControlResult>;
  listSessionsForTab: (tabID: string) => Promise<SessionMeta[]>;
  sessionIDFromPath: (path: string) => string;
}

// resolveTaskMonitorSession keeps every asynchronous lookup bound to the tab
// that rendered the task row. A newer navigation intent makes late successes
// and late failures inert, preserving last-click-wins behavior.
export async function resolveTaskMonitorSession({
  tabID,
  taskID,
  intentSeq,
  isIntentCurrent,
  openTaskSessionForTab,
  listSessionsForTab,
  sessionIDFromPath,
}: TaskMonitorNavigationDeps): Promise<SessionMeta | null> {
  let result: ControlResult;
  try {
    result = await openTaskSessionForTab(tabID, taskID);
  } catch (error) {
    if (!isIntentCurrent(intentSeq)) return null;
    throw error;
  }
  if (!isIntentCurrent(intentSeq)) return null;
  if (result.error) {
    throw new Error(`${result.error.code}: ${result.error.message}`);
  }

  const sessionID = result.session_id?.trim();
  if (!sessionID) throw new Error("Task session is unavailable");

  let sessions: SessionMeta[];
  try {
    sessions = await listSessionsForTab(tabID);
  } catch (error) {
    if (!isIntentCurrent(intentSeq)) return null;
    throw error;
  }
  if (!isIntentCurrent(intentSeq)) return null;

  const session = sessions.find((candidate) => sessionIDFromPath(candidate.path) === sessionID);
  if (!session) throw new Error("Task session is unavailable");
  return session;
}
