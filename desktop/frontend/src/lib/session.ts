import type { ProjectNode, SessionMeta } from "./types";

export function sessionActivityTime(session: SessionMeta): number {
  return session.lastActivityAt ?? session.modTime;
}

// topicActivityTime returns the last-activity timestamp for a sidebar topic
// node. Falls back to the topic's creation time so blank topics (no session
// files yet) are still visible under time-based filters.
export function topicActivityTime(node: ProjectNode): number {
  return node.lastActivityAt || node.createdAt || 0;
}
