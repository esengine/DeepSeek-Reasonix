/**
 * Bounded, session-aware transcript geometry cache.
 *
 * Exact samples are keyed by session + row + layout state + content version +
 * readable width + typography. Stable compact rows may share a same-state
 * median; content-flow rows use a static-estimate bucket ratio so a short
 * answer can never replace the estimate for a very long one.
 */

import {
  estimateTranscriptRowGeometry,
  isStableCompactTranscriptVariant,
  transcriptRowLayoutVariant,
  type TranscriptEstimateSource,
  type TranscriptGeometryEnvironment,
  type TranscriptRowLayoutVariant,
} from "./transcriptRowGeometry";
import { transcriptRowMeasurementVersion, type TranscriptRow } from "./transcriptRows";

const DEFAULT_SESSION_CAP = 8;
const DEFAULT_ROW_CAP = 4_096;
const DEFAULT_SAMPLE_CAP = 100;

type GeometrySample = {
  rowKey: string;
  kind: TranscriptRow["kind"];
  layoutVariant: TranscriptRowLayoutVariant;
  height: number;
  contentWidth?: number;
  typographySignature: string;
  measurementVersion: string;
  staticEstimate?: number;
};

type SessionMeasurements = { rows: Map<string, GeometrySample> };

export type TranscriptGeometryRecord = {
  rowKey: string;
  kind: TranscriptRow["kind"];
  layoutVariant: TranscriptRowLayoutVariant;
  height: number;
  environment: TranscriptGeometryEnvironment;
  measurementVersion: string;
  staticEstimate?: number;
};

export type TranscriptSynthesizedSizes = {
  heightEstimates: number[];
  estimateSources: TranscriptEstimateSource[];
};

export type TranscriptMeasuredSizes = {
  recordGeometry: (sessionKey: string, record: TranscriptGeometryRecord) => void;
  synthesizeDetailed: (
    sessionKey: string,
    rows: readonly TranscriptRow[],
    environment: TranscriptGeometryEnvironment,
  ) => TranscriptSynthesizedSizes;
};

export type TranscriptMeasuredSizesOptions = {
  maxSessions?: number;
  maxRowsPerSession?: number;
  maxCalibrationSamples?: number;
};

function medianOf(samples: readonly number[]): number | undefined {
  if (samples.length === 0) return undefined;
  const sorted = [...samples].sort((left, right) => left - right);
  const middle = sorted.length >> 1;
  return sorted.length % 2 === 1 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2;
}

function normalizedWidth(width: number | undefined): number | undefined {
  return Number.isFinite(width) && (width ?? 0) > 0 ? width : undefined;
}

function environmentMatches(sample: GeometrySample, environment: TranscriptGeometryEnvironment): boolean {
  if (sample.typographySignature !== environment.typographySignature) return false;
  const width = normalizedWidth(environment.contentWidth);
  return width === undefined || (sample.contentWidth !== undefined && Math.abs(sample.contentWidth - width) <= 1);
}

function calibrationBucket(staticEstimate: number): number {
  return Math.max(0, Math.min(8, Math.floor(Math.log2(Math.max(64, staticEstimate) / 64))));
}

export function createTranscriptMeasuredSizes(options: TranscriptMeasuredSizesOptions = {}): TranscriptMeasuredSizes {
  const maxSessions = Math.max(1, Math.round(options.maxSessions ?? DEFAULT_SESSION_CAP));
  const maxRowsPerSession = Math.max(1, Math.round(options.maxRowsPerSession ?? DEFAULT_ROW_CAP));
  const maxCalibrationSamples = Math.max(1, Math.round(options.maxCalibrationSamples ?? DEFAULT_SAMPLE_CAP));
  const sessions = new Map<string, SessionMeasurements>();

  const touchSession = (sessionKey: string): SessionMeasurements => {
    const key = sessionKey || "__default__";
    const existing = sessions.get(key);
    const session = existing ?? { rows: new Map<string, GeometrySample>() };
    if (existing) sessions.delete(key);
    sessions.set(key, session);
    while (sessions.size > maxSessions) {
      const oldest = sessions.keys().next().value as string | undefined;
      if (oldest === undefined) break;
      sessions.delete(oldest);
    }
    return session;
  };

  const storeRecord = (sessionKey: string, record: Omit<GeometrySample, "contentWidth" | "typographySignature"> & {
    environment: TranscriptGeometryEnvironment;
  }) => {
    if (!Number.isFinite(record.height) || record.height <= 0) return;
    const session = touchSession(sessionKey);
    const sample: GeometrySample = {
      rowKey: record.rowKey,
      kind: record.kind,
      layoutVariant: record.layoutVariant,
      height: record.height,
      contentWidth: normalizedWidth(record.environment.contentWidth),
      typographySignature: record.environment.typographySignature,
      measurementVersion: record.measurementVersion,
      staticEstimate: record.staticEstimate,
    };
    // One latest observation per logical row; animation frames never add weight.
    session.rows.delete(record.rowKey);
    session.rows.set(record.rowKey, sample);
    while (session.rows.size > maxRowsPerSession) {
      const oldest = session.rows.keys().next().value as string | undefined;
      if (oldest === undefined) break;
      session.rows.delete(oldest);
    }
  };

  const synthesizeDetailed: TranscriptMeasuredSizes["synthesizeDetailed"] = (sessionKey, rows, environment) => {
    const session = touchSession(sessionKey);
    // A late-content patch invalidates that row immediately and must not leave
    // its old ratio available to calibrate a sibling.
    for (const row of rows) {
      const rowKey = String(row.key);
      const sample = session.rows.get(rowKey);
      if (sample && sample.measurementVersion !== transcriptRowMeasurementVersion(row)) session.rows.delete(rowKey);
    }

    const samples = [...session.rows.values()];
    const heightEstimates: number[] = [];
    const estimateSources: TranscriptEstimateSource[] = [];
    for (const row of rows) {
      const rowKey = String(row.key);
      const layoutVariant = transcriptRowLayoutVariant(row);
      const measurementVersion = transcriptRowMeasurementVersion(row);
      const staticEstimate = estimateTranscriptRowGeometry(row, environment);
      const exact = session.rows.get(rowKey);
      if (
        exact
        && exact.kind === row.kind
        && exact.layoutVariant === layoutVariant
        && exact.measurementVersion === measurementVersion
        && environmentMatches(exact, environment)
      ) {
        heightEstimates.push(exact.height);
        estimateSources.push("exact");
        continue;
      }

      if (isStableCompactTranscriptVariant(layoutVariant)) {
        const compactMedian = medianOf(samples
          .filter((sample) => sample.layoutVariant === layoutVariant && environmentMatches(sample, environment))
          .slice(-maxCalibrationSamples)
          .map((sample) => sample.height));
        if (compactMedian !== undefined) {
          heightEstimates.push(compactMedian);
          estimateSources.push("compact-median");
          continue;
        }
      } else {
        const bucket = calibrationBucket(staticEstimate);
        const ratios = samples
          .filter((sample) => sample.kind === row.kind
            && sample.layoutVariant === layoutVariant
            && environmentMatches(sample, environment)
            && sample.staticEstimate !== undefined
            && sample.staticEstimate > 0
            && calibrationBucket(sample.staticEstimate) === bucket)
          .slice(-maxCalibrationSamples)
          .map((sample) => sample.height / sample.staticEstimate!);
        const ratio = medianOf(ratios);
        if (ratio !== undefined) {
          heightEstimates.push(Math.max(1, Math.round(staticEstimate * ratio * 2) / 2));
          estimateSources.push("calibrated");
          continue;
        }
      }

      heightEstimates.push(staticEstimate);
      estimateSources.push("static");
    }
    return { heightEstimates, estimateSources };
  };

  return {
    recordGeometry: (sessionKey, record) => storeRecord(sessionKey, record),
    synthesizeDetailed,
  };
}
