import type { SessionMeta } from "./types";

export function sessionActivityTime(session: SessionMeta): number {
  return session.lastActivityAt ?? session.modTime;
}
