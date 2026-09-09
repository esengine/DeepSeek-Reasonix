import type { SessionCatalogStatus } from "./sessionCatalogTypes";

export type SessionCatalogNotice = "indexing" | "repair-active" | "repair-deferred" | "repair-blocked" | "failed" | "rebuild";

export function sessionCatalogNotice(s: SessionCatalogStatus): SessionCatalogNotice | null {
  const { state, repairActive, repairDeferred, repairBlocked } = s;
  return state === "opening" || state === "rebuilding"
    ? "indexing"
    : state === "degraded" || s.lastError
      ? (s.canRebuild ? "rebuild" : "failed")
      : (s.unindexedTargetCount ?? 0) > 0
        ? "indexing"
        : (repairActive ?? (repairDeferred === undefined && repairBlocked === undefined ? s.repairPending : 0)) > 0
          ? "repair-active"
          : (repairDeferred ?? 0) > 0
            ? "repair-deferred"
            : (repairBlocked ?? 0) > 0
              ? "repair-blocked"
              : null;
}
