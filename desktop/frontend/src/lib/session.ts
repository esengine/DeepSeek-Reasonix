import type { HistorySearchHit, ProjectNode, SessionMeta } from "./types";

export function sessionActivityTime(session: SessionMeta): number {
  return session.lastActivityAt ?? session.modTime;
}

export function historySessionDisplayTitle(session: Pick<SessionMeta, "preview" | "title" | "topicTitle">, fallback: string): string {
  return session.topicTitle?.trim() || session.title?.trim() || session.preview?.trim() || fallback;
}

export function historySearchHitDisplayTitle(
  hit: Pick<HistorySearchHit, "sessionPath" | "sessionTitle" | "topicTitle">,
): string {
  return hit.topicTitle?.trim() || hit.sessionTitle?.trim() || hit.sessionPath;
}

export function paletteSessionDisplayTitle(session: Pick<SessionMeta, "preview" | "title" | "topicTitle">, fallback: string): string {
  return session.topicTitle?.trim() || session.title?.trim() || session.preview?.trim() || fallback;
}

export function paletteSessionHint(
  session: Pick<SessionMeta, "preview" | "title" | "topicTitle" | "topicId" | "workspaceRoot">,
): string | undefined {
  const primary = paletteSessionDisplayTitle(session, "");
  const title = session.title?.trim();
  const preview = session.preview?.trim();
  const workspace = session.workspaceRoot?.trim();
  const secondary = session.topicId
    ? preview && preview !== primary ? preview : ""
    : title && title !== primary ? title : preview && preview !== primary ? preview : "";
  const hint = [secondary, workspace].filter(Boolean).join(" · ");
  return hint || undefined;
}

export function paletteSessionKeywords(session: Pick<SessionMeta, "preview" | "title" | "topicId">): string[] {
  return [session.topicId ? undefined : session.title?.trim(), session.preview?.trim()].filter((value): value is string => Boolean(value));
}

// topicActivityTime returns the last-activity timestamp for a sidebar topic
// node. Falls back to the topic's creation time so blank topics (no session
// files yet) are still visible under time-based filters.
export function topicActivityTime(node: ProjectNode): number {
  return node.lastActivityAt || node.createdAt || 0;
}
