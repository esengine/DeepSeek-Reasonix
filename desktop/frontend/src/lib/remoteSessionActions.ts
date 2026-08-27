import { app } from "./bridge";
import type { TabMeta } from "./types";

export async function renameCurrentRemoteSession(tab: TabMeta | undefined, title: string): Promise<boolean> {
  if (!tab?.remote) return false;
  const sessions = await app.RemoteProjectSessions(tab.remote.hostId, tab.remote.workspace);
  const current = sessions.find((session) => session.current);
  if (current) await app.RenameRemoteProjectSession(tab.remote.hostId, tab.remote.workspace, current.name, title);
  return true;
}
