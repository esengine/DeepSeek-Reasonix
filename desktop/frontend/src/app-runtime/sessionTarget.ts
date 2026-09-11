export type SessionIdentityInput = {
  tabId?: string;
  sessionPath?: string;
  sessionGeneration?: number;
  scope?: string;
  workspaceRoot?: string;
  topicId?: string;
};

/** Runtime session identity; intentionally distinct from draft/workspace keys. */
export function sessionIdentityKey(input: SessionIdentityInput): string {
  const sessionPath = (input.sessionPath ?? "").trim();
  if (sessionPath) {
    return ["session", sessionPath, String(input.sessionGeneration ?? 0)].join("\u0000");
  }
  return [
    "topic",
    input.scope ?? "",
    input.workspaceRoot ?? "",
    input.topicId ?? "",
    input.tabId ?? "",
  ].join("\u0000");
}

export type SessionSurfaceOwnership = Readonly<{
  revision: number;
  tabId: string;
  sessionKey: string;
}>;

/** Commit-owned UI fence; A → B → A advances revision and never revives A. */
export function createSessionSurfaceFence() {
  let revision = 0;
  let current: SessionSurfaceOwnership | undefined;
  const owns = (ownership: SessionSurfaceOwnership): boolean => Boolean(
    current
    && current.revision === ownership.revision
    && current.tabId === ownership.tabId
    && current.sessionKey === ownership.sessionKey,
  );
  return {
    commit(tabId: string | undefined, sessionKey: string): SessionSurfaceOwnership | undefined {
      if (!tabId) {
        if (current) revision += 1;
        current = undefined;
        return undefined;
      }
      if (!current || current.tabId !== tabId || current.sessionKey !== sessionKey) revision += 1;
      current = Object.freeze({ revision, tabId, sessionKey });
      return current;
    },
    capture(): SessionSurfaceOwnership | undefined {
      return current;
    },
    owns,
    ownsUnknown(ownership: unknown): boolean {
      if (!ownership || typeof ownership !== "object") return false;
      return owns(ownership as SessionSurfaceOwnership);
    },
    dispose(): void {
      revision += 1;
      current = undefined;
    },
  };
}
