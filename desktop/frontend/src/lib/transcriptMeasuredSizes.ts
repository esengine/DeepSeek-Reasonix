/**
 * Session-scoped cache of measured transcript row heights.
 *
 * Virtuoso applies `heightEstimates` only while its size tree is empty, so
 * every full remount collapses the tree back to the static priors — and any
 * estimate-based scrollToIndex landing after the remount inherits the
 * prior's error, which grows with transcript length (#8657). This store
 * records the heights Virtuoso has already measured (`data-known-size` on
 * mounted rows) and synthesizes estimates from them:
 *
 *   measured height for this rowKey
 *   → median of recently measured rows of the same kind
 *   → static EAW-aware prior
 *
 * A remount that must happen anyway (surface switch) then restarts from
 * measured geometry instead of guesses, so the collapse mechanism is
 * disarmed even when a remount cannot be avoided.
 */

import { estimateTranscriptRowSize, type TranscriptRow } from "./transcriptRows";

/** Samples kept per row kind for the median fallback. */
const KIND_SAMPLE_CAP = 100;

export type TranscriptMeasuredSizes = {
  /** Record one measured row height (px). Non-positive/NaN ignored. */
  record: (rowKey: string, kind: TranscriptRow["kind"], height: number) => void;
  /** Best-known height estimate for a row. */
  estimateFor: (row: TranscriptRow) => number;
  /** Estimate array aligned with `rows`, for Virtuoso's heightEstimates. */
  synthesize: (rows: readonly TranscriptRow[]) => number[];
  /** Drop all measurements (surface switch). */
  clear: () => void;
};

function medianOf(samples: readonly number[]): number | undefined {
  if (samples.length === 0) return undefined;
  const sorted = [...samples].sort((a, b) => a - b);
  const middle = sorted.length >> 1;
  return sorted.length % 2 === 1
    ? sorted[middle]
    : (sorted[middle - 1] + sorted[middle]) / 2;
}

export function createTranscriptMeasuredSizes(): TranscriptMeasuredSizes {
  const measured = new Map<string, number>();
  const kindSamples = new Map<TranscriptRow["kind"], number[]>();
  const medianCache = new Map<TranscriptRow["kind"], number | undefined>();

  const record: TranscriptMeasuredSizes["record"] = (rowKey, kind, height) => {
    if (!Number.isFinite(height) || height <= 0) return;
    measured.set(rowKey, height);
    let samples = kindSamples.get(kind);
    if (!samples) {
      samples = [];
      kindSamples.set(kind, samples);
    }
    samples.push(height);
    if (samples.length > KIND_SAMPLE_CAP) samples.splice(0, samples.length - KIND_SAMPLE_CAP);
    medianCache.delete(kind);
  };

  const kindMedian = (kind: TranscriptRow["kind"]): number | undefined => {
    if (!medianCache.has(kind)) {
      medianCache.set(kind, medianOf(kindSamples.get(kind) ?? []));
    }
    return medianCache.get(kind);
  };

  const estimateFor: TranscriptMeasuredSizes["estimateFor"] = (row) => {
    const exact = measured.get(String(row.key));
    if (exact !== undefined) return exact;
    return kindMedian(row.kind) ?? estimateTranscriptRowSize(row);
  };

  return {
    record,
    estimateFor,
    synthesize: (rows) => rows.map((row) => estimateFor(row)),
    clear: () => {
      measured.clear();
      kindSamples.clear();
      medianCache.clear();
    },
  };
}
