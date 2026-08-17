import type { StateSnapshot } from "react-virtuoso";

/**
 * One captured Virtuoso state snapshot bound to the row key sequence it was
 * taken from. Ranges are Virtuoso-internal size-tree indexes (data-relative);
 * the row keys are the source of truth for whether the snapshot still
 * describes the current data.
 */
export type TranscriptStateSnapshot = {
  keys: readonly string[];
  snapshot: StateSnapshot;
};

/**
 * Returns a captured snapshot only for the exact row sequence it measured.
 * React Virtuoso's restoreStateFrom contract requires the same data and
 * totalCount; changed data falls back to measured-height estimates plus the
 * logical anchor instead of translating Virtuoso's internal ranges.
 *
 * This is intentionally stricter than key-overlap checks: append, prepend,
 * rewind, and session switches all change totalCount and must discard.
 */
export function resolveTranscriptStateSnapshot(
  record: TranscriptStateSnapshot | null,
  currentKeys: readonly string[],
): StateSnapshot | undefined {
  if (!record || record.keys.length === 0 || currentKeys.length === 0) return undefined;
  if (record.keys.length !== currentKeys.length) return undefined;
  for (let index = 0; index < record.keys.length; index += 1) {
    if (record.keys[index] !== currentKeys[index]) return undefined;
  }
  return record.snapshot;
}
