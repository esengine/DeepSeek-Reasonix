import type { TranscriptProjection } from "./transcriptStore";
import type { Meta } from "./types";

export function historyFingerprintMatchesMeta(history: { revision: number; revisionKnown?: boolean; digest?: string }, meta: Meta): boolean {
  const expectedDigest = (meta.sessionDigest ?? "").trim();
  if (expectedDigest && history.digest !== expectedDigest) return false;
  const expectedRevision = meta.sessionRevision ?? 0;
  return expectedRevision <= 0 || Boolean(history.revisionKnown && history.revision === expectedRevision);
}

export function historyRevisionIsOlder(current: number | undefined, incoming: number | undefined): boolean {
  return typeof current === "number" && current > 0
    && typeof incoming === "number" && incoming > 0
    && incoming < current;
}

export function historyReplaceAction(projection: TranscriptProjection) {
  return {
    type: "history_replace" as const, items: projection.items,
    startTurn: projection.startTurn, endTurn: projection.endTurn, totalTurns: projection.totalTurns,
    hasOlder: projection.hasOlder, hasNewer: projection.hasNewer,
    revision: projection.revisionKnown ? projection.revision : undefined,
    digest: projection.digest || undefined,
  };
}
