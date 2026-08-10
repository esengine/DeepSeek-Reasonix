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
  const measureElement = useMemo(() => createTranscriptMeasureElement({
    tabId: tabId ?? "",
    getLayoutSnapshot: () => layoutSnapshotRef.current,
  }), [tabId]);
  return { estimateSize, layoutSnapshotRef, measureElement };
}
