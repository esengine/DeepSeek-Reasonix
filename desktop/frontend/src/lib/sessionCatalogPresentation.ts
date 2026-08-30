import type { SessionCatalogStatus } from "./sessionCatalogTypes";

export type SessionCatalogNotice = "working" | "failed" | "rebuild";

export function sessionCatalogNotice(status: SessionCatalogStatus): SessionCatalogNotice | null {
  const working = status.state === "opening"
    || status.state === "rebuilding"
    || status.repairPending > 0
    || (status.unindexedTargetCount ?? 0) > 0;
  if (working) return "working";
  if (status.state === "degraded" || status.lastError) return status.canRebuild === true ? "rebuild" : "failed";
  return null;
}
