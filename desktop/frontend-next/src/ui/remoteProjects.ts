import type { RuntimeView, TreeWorkspace } from "../port/hub";
import { workspaceContains } from "../port/hub";
import type { RemoteHost } from "../port/remote";
import { workspacesOf } from "./Remotes";
import { inScope, type SessionScope } from "./sessionScope";
import { pinnedFirst } from "./pinorder";

const leaf = (dir: string) => dir.replace(/[\\/]+$/, "").split(/[\\/]/).pop() || dir;

export const remoteProjectKey = (host: string, root: string) => JSON.stringify([host, root]);

export function remoteWorkspaces(host: RemoteHost, tree: TreeWorkspace[] | null | undefined, panes: RuntimeView[] = []): TreeWorkspace[] {
  const known = workspacesOf(host);
  const out = [...(tree ?? [])];
  for (const dir of known) {
    if (!out.some((ws) => workspaceContains(ws, dir))) out.push({ root: dir, name: leaf(dir), sessions: [] });
  }
  for (const pane of panes) {
    if (pane.host === host.name && pane.root && !out.some((ws) => workspaceContains(ws, pane.root))) {
      out.push({ root: pane.root, name: leaf(pane.root), sessions: [] });
    }
  }
  return out.filter((ws) => !host.hiddenWorkspaces?.includes(ws.root) || known.some((root) => workspaceContains(ws, root)));
}

export function remoteProjects(host: RemoteHost, tree: TreeWorkspace[] | null | undefined, runtimes: RuntimeView[], scope: SessionScope, needle: string, listedIds: (ids: string[]) => string[], hideLegacy = false) {
  const panes = runtimes.filter((rt) => rt.host === host.name);
  const named = host.name.toLowerCase().includes(needle) || host.target.toLowerCase().includes(needle);
  return remoteWorkspaces(host, tree, panes).flatMap((ws) => {
    const projectHit = !needle || named || ws.name.toLowerCase().includes(needle);
    const matches = (name: string) => projectHit || name.toLowerCase().includes(needle);
    const sessions = ws.sessions.filter((session) => {
      const held = panes.find((rt) => rt.sessionPath === session.path);
      return matches(session.title || session.name || "") &&
        inScope(session, scope, !!needle, !!held && listedIds([held.id]).length > 0, !!session.pinned, hideLegacy);
    });
    const loose = panes.filter((rt) => workspaceContains(ws, rt.root) && !ws.sessions.some((session) => session.path === rt.sessionPath))
      .filter((rt) => matches(rt.name) && (scope === "all" || scope === "live" && listedIds([rt.id]).length > 0));
    if (!sessions.length && !loose.length && !(scope === "all" && projectHit)) return [];
    const pinned = new Set(sessions.filter((session) => session.pinned).map((session) => session.path));
    return [{ workspace: { ...ws, sessions: pinnedFirst(sessions, pinned) }, loose, older: Math.max(0, (ws.total ?? ws.sessions.length) - ws.sessions.length) }];
  });
}
