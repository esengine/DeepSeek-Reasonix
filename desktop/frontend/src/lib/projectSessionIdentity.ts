import type { ProjectNode } from "./types";
import { sessionIdentityBaseKey } from "./sessionIdentity";

/** Stable row identity; topicId is only the last resort for an empty draft. */
export function projectSessionIdentity(node: ProjectNode): string {
  if (node.session?.sessionId) return sessionIdentityBaseKey(node);
  if (node.remoteSession) {
    const remote = node.remoteSession;
    if (remote.sessionId) return sessionIdentityBaseKey({ session: { hostId: remote.hostId, sessionId: remote.sessionId } });
    return `source\u0000${remote.hostId}\u0000${remote.workspace}\u0000${remote.path || remote.name}`;
  }
  if (node.source) return `source\u0000${node.source.hostId || "local"}\u0000${node.source.sourceKey || `${node.source.path}\u0000${node.source.headId || ""}`}`;
  if (node.sessionPath) return `path\u0000${node.sessionPath.trim()}`;
  if (node.tabId) return `tab\u0000local\u0000${node.tabId}`;
  return `topic\u0000${node.topicId || node.key}`;
}

/** Only owner-verified aliases may join a source to its adopted session. */
export function projectSessionKeys(node: ProjectNode): string[] {
  return [projectSessionIdentity(node), ...(node.identityAliases ?? [])];
}

export function canonicalProjectSession(node: ProjectNode): string | undefined {
  return node.session?.sessionId || node.remoteSession?.sessionId ? projectSessionIdentity(node) : undefined;
}

/** Resolve legacy rows only when the complete page/overlay proves one owner. */
export function projectSessionResolver(nodes: readonly ProjectNode[]): (node: ProjectNode) => string {
  const owners = new Map<string, { identity: string | null; canonical: boolean }>();
  for (const node of nodes) {
    const canonical = Boolean(canonicalProjectSession(node));
    const owner = canonicalProjectSession(node) ?? (node.tabId ? projectSessionIdentity(node) : undefined);
    if (!owner) continue;
    for (const key of projectSessionKeys(node)) {
      const previous = owners.get(key);
      if (previous?.canonical && !canonical) continue;
      const identity = !previous || (canonical && !previous.canonical) || previous.identity === owner ? owner : null;
      owners.set(key, { identity, canonical });
    }
  }
  return node => canonicalProjectSession(node) ?? owners.get(projectSessionIdentity(node))?.identity ?? projectSessionIdentity(node);
}

export function projectSessionExcluded(node: ProjectNode, excluded: ReadonlySet<string>): boolean {
  const canonical = canonicalProjectSession(node);
  if (canonical) return excluded.has(canonical);
  return projectSessionKeys(node).some(key => excluded.has(key)) || Boolean(node.topicId && excluded.has(node.topicId));
}

/** A source keeps its mounted row key when its owner publishes canonical aliases. */
const mountedRowKeys = new Map<string, { key: string; owner?: string }>();
export function projectSessionRowKey(node: ProjectNode): string {
  const keys = projectSessionKeys(node);
  const identity = keys[0];
  const canonical = canonicalProjectSession(node);
  // An ambiguous source can coexist with its historical versions. Its old
  // mounted key already belongs to an adopted canonical row in that case.
  if (!canonical && mountedRowKeys.get(identity)?.owner) return `source-row\u0000${identity}`;
  // A legacy alias may outlive an adoption or arrive from an older host.
  // It can preserve a source's mounted row, but cannot merge two SessionRefs.
  const entry: { key: string; owner?: string } = keys.map(key => mountedRowKeys.get(key)).find(entry =>
    entry && (!entry.owner || entry.owner === canonical)) ?? { key: identity };
  entry.owner ??= canonical;
  for (const key of keys) {
    const previous = mountedRowKeys.get(key);
    if (!previous?.owner || previous.owner === canonical) mountedRowKeys.set(key, entry);
  }
  return entry.key;
}

export function sameProjectSession(a: ProjectNode, b: ProjectNode): boolean {
  if (canonicalProjectSession(a) && canonicalProjectSession(b)) return projectSessionIdentity(a) === projectSessionIdentity(b);
  const keys = new Set(projectSessionKeys(a));
  return projectSessionKeys(b).some(key => keys.has(key));
}
