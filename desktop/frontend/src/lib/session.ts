import type { ProjectNode, SessionMeta } from "./types";

export function sessionActivityTime(session: SessionMeta): number {
  return session.lastActivityAt ?? session.modTime;
}

export function historySessionDisplayTitle(session: Pick<SessionMeta, "preview" | "title" | "topicTitle">, fallback: string): string {
  return session.title?.trim() || session.topicTitle?.trim() || session.preview?.trim() || fallback;
}

export function paletteSessionDisplayTitle(session: Pick<SessionMeta, "preview" | "title" | "topicTitle">, fallback: string): string {
  return session.topicTitle?.trim() || session.title?.trim() || session.preview?.trim() || fallback;
}

export function paletteSessionHint(
  session: Pick<SessionMeta, "preview" | "title" | "topicTitle" | "workspaceRoot">,
): string | undefined {
  const primary = paletteSessionDisplayTitle(session, "");
  const title = session.title?.trim();
  const preview = session.preview?.trim();
  const workspace = session.workspaceRoot?.trim();
  const secondary = title && title !== primary ? title : preview && preview !== primary ? preview : "";
  const hint = [secondary, workspace].filter(Boolean).join(" · ");
  return hint || undefined;
}

export function paletteSessionKeywords(session: Pick<SessionMeta, "preview" | "title">): string[] {
  return [session.title?.trim(), session.preview?.trim()].filter((value): value is string => Boolean(value));
}

// topicActivityTime returns the last-activity timestamp for a sidebar topic
// node. Falls back to the topic's creation time so blank topics (no session
// files yet) are still visible under time-based filters.
export function topicActivityTime(node: ProjectNode): number {
  return node.lastActivityAt || node.createdAt || 0;
}
