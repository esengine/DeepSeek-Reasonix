import type { ProjectNode, SessionMeta } from "./types";

export function sessionActivityTime(session: SessionMeta): number {
  return session.lastActivityAt ?? session.modTime;
}

// topicActivityTime returns the last-activity timestamp for a sidebar topic
// node. It returns 0 when no timestamp is known, matching the Go backend's
// int64 zero value so that topics with no sessions are distinguishable from
// genuinely recent ones.
export function topicActivityTime(node: ProjectNode): number {
  return node.lastActivityAt ?? 0;
}
