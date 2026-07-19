export type TargetScopedRequest = Readonly<{
  targetIdentityGen: number;
  requestSeq: number;
}>;

export function targetScopedResponseCanCommit(
  request: TargetScopedRequest,
  targetIdentityGen: number,
  latestCommittedRequestSeq: number,
): boolean {
  return request.targetIdentityGen === targetIdentityGen && request.requestSeq > latestCommittedRequestSeq;
}

export function commitTargetScopedValue<T>(
  request: TargetScopedRequest,
  targetIdentityGen: number,
  latestCommittedRequestSeq: number,
  value: T,
  commit: (accepted: T) => void,
): boolean {
  if (!targetScopedResponseCanCommit(request, targetIdentityGen, latestCommittedRequestSeq)) return false;
  commit(value);
  return true;
}

export function workspaceScopeKeyForAuthority(parts: Readonly<{
  targetIdentityGen: number;
  activeTabId?: string;
  tabSessionPath?: string;
  metaSessionPath?: string;
  cwd?: string;
  sessionGen: number;
  workspaceControllerEpoch: number;
}>): string {
  return [
    parts.targetIdentityGen,
    parts.activeTabId ?? "",
    parts.tabSessionPath ?? "",
    parts.metaSessionPath ?? "",
    parts.cwd ?? "",
    parts.sessionGen,
    parts.workspaceControllerEpoch,
  ].join("\u0000");
}
