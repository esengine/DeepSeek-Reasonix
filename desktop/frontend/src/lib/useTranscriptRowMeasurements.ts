import { useCallback, useMemo, useRef } from "react";
import { estimateTranscriptRowSize, type TranscriptRow } from "./transcriptRows";
import {
  createTranscriptMeasureElement,
  EMPTY_TRANSCRIPT_LAYOUT_SNAPSHOT,
  estimateTranscriptRowHeightForLayout,
} from "./transcriptHeightCache";

export function useTranscriptRowMeasurements(tabId: string | undefined, rows: readonly TranscriptRow[]) {
  const layoutSnapshotRef = useRef(EMPTY_TRANSCRIPT_LAYOUT_SNAPSHOT);
  const estimateSize = useCallback((index: number) => {
    const row = rows[index];
    return estimateTranscriptRowHeightForLayout({
      tabId: tabId ?? "",
      layout: layoutSnapshotRef.current,
      rowKey: String(row?.key ?? index),
      row,
      fallback: estimateTranscriptRowSize(row),
    });
  }, [rows, tabId]);
  // Streaming rows are measured but never cached: their height is a moving
  // target during tail growth / live code highlighting, and caching it would
  // poison later estimates for the same row key (see transcriptHeightCache).
  // Both answer and reasoning rows stream (answer: item.streaming; reasoning:
  // item.streaming && !reasoningComplete, same rule as segmentHasRunningWork).
  const skipCacheWriteWhen = useCallback((element: HTMLDivElement): boolean => {
    const rowKey = element.dataset.rowKey;
    if (!rowKey) return false;
    const row = rows.find((candidate) => String(candidate.key) === rowKey);
    if (!row) return false;
    if (row.kind === "answer") return row.item.streaming;
    if (row.kind === "reasoning") return row.item.streaming && !row.item.reasoningComplete;
    return false;
  }, [rows]);
  const measureElement = useMemo(() => createTranscriptMeasureElement({
    tabId: tabId ?? "",
    getLayoutSnapshot: () => layoutSnapshotRef.current,
    skipCacheWriteWhen,
  }), [tabId, skipCacheWriteWhen]);
  return { estimateSize, layoutSnapshotRef, measureElement };
}
