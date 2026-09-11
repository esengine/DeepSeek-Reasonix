export function resolveTurnStartedAt(current: number, reported: number | undefined, now = Date.now()): number {
  if (typeof reported === "number" && Number.isFinite(reported) && reported > 0 && reported <= now) return reported;
  if (Number.isFinite(current) && current > 0 && current <= now) return current;
  return now;
}

// Snapshots may complete out of order, so a live clock can advance but never rewind.
export function resolveSnapshotTurnStartedAt(current: number, reported: number | undefined, now = Date.now()): number {
  const resolved = resolveTurnStartedAt(current, reported, now);
  if (!Number.isFinite(current) || current <= 0 || current > now) return resolved;
  return Math.max(current, resolved);
}

// Both values come from the same monotonic clock; ties are conservatively stale.
export function snapshotPredatesTurnLifecycle(observedAt: number | undefined, snapshotAt: number | undefined): boolean {
  if (snapshotAt === undefined || observedAt === undefined) return false;
  return snapshotAt <= observedAt;
}
