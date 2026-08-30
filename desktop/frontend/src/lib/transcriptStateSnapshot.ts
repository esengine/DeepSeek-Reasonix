import type { StateSnapshot, VirtuosoHandle } from "react-virtuoso";
import type { TranscriptGeometryEnvironment } from "./transcriptRowGeometry";
import { transcriptRowLayoutVariant } from "./transcriptRowGeometry";
import { transcriptRowMeasurementVersion, type TranscriptRow } from "./transcriptRows";

export type TranscriptStateGeometry = {
  sessionKey: string;
  contentWidth?: number;
  typographySignature: string;
  rowSignatures: readonly string[];
};

/**
 * One captured Virtuoso state snapshot bound to the row key sequence it was
 * taken from. Ranges are Virtuoso-internal size-tree indexes (data-relative);
 * the row keys are the source of truth for whether the snapshot still
 * describes the current data.
 */
export type TranscriptStateSnapshot = {
  keys: readonly string[];
  geometry?: TranscriptStateGeometry;
  snapshot: StateSnapshot;
};

export function createTranscriptStateGeometry(
  sessionKey: string,
  rows: readonly TranscriptRow[],
  environment: TranscriptGeometryEnvironment,
): TranscriptStateGeometry {
  return {
    sessionKey,
    contentWidth: Number.isFinite(environment.contentWidth) ? environment.contentWidth : undefined,
    typographySignature: environment.typographySignature,
    rowSignatures: rows.map((row) => [
      String(row.key),
      row.kind,
      transcriptRowLayoutVariant(row),
      transcriptRowMeasurementVersion(row),
    ].join("\u0000")),
  };
}

function sameGeometry(left: TranscriptStateGeometry, right: TranscriptStateGeometry): boolean {
  if (left.sessionKey !== right.sessionKey || left.typographySignature !== right.typographySignature) return false;
  if (Math.abs((left.contentWidth ?? 0) - (right.contentWidth ?? 0)) > 1) return false;
  if (left.rowSignatures.length !== right.rowSignatures.length) return false;
  return left.rowSignatures.every((signature, index) => signature === right.rowSignatures[index]);
}

/** Read Virtuoso's live measured tree and scrollTop synchronously. */
export function captureTranscriptVirtuosoState(handle: VirtuosoHandle | null): StateSnapshot | null {
  if (!handle) return null;
  let state: StateSnapshot | null = null;
  handle.getState((snapshot) => { state = snapshot; });
  return state;
}

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
  currentGeometry?: TranscriptStateGeometry,
): StateSnapshot | undefined {
  if (!record || record.keys.length === 0 || currentKeys.length === 0) return undefined;
  if (record.keys.length !== currentKeys.length) return undefined;
  for (let index = 0; index < record.keys.length; index += 1) {
    if (record.keys[index] !== currentKeys[index]) return undefined;
  }
  // New captures carry the full geometry contract. Legacy in-memory captures
  // remain readable within the current runtime, but geometry-aware callers
  // never accept a partially described snapshot.
  if (record.geometry || currentGeometry) {
    if (!record.geometry || !currentGeometry || !sameGeometry(record.geometry, currentGeometry)) return undefined;
  }
  return record.snapshot;
}
