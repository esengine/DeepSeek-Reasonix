import { useCallback, useEffect, useRef, type RefObject } from "react";
import type { ListItem } from "react-virtuoso";
import { noteTranscriptRowCounts } from "./sessionDiagnostics";
import type { TranscriptGeometryChangeSource } from "./transcriptGeometryRevision";
import type { TranscriptScrollMode } from "./transcriptScrollArbiter";
import type { TranscriptRow } from "./transcriptRows";
import type { HistoryMutation } from "./useController";

/** Bridges Virtuoso lifecycle delivery into the coalesced geometry controller. */
export function useTranscriptGeometryLifecycle({
  virtualRowCount,
  hydrating,
  readerTransactionActive,
  historyMutation,
  scrollModeRef,
  followGrowingTail,
  revalidateTail,
  reconcileLogicalFocus,
  handleRecoveryItemsRendered,
  scheduleActiveQuestionSync,
  markSurfaceItemsRendered,
}: {
  virtualRowCount: number;
  hydrating: boolean;
  readerTransactionActive: boolean;
  historyMutation?: HistoryMutation;
  scrollModeRef: RefObject<TranscriptScrollMode>;
  followGrowingTail: (source: TranscriptGeometryChangeSource) => void;
  revalidateTail: () => void;
  reconcileLogicalFocus: () => void;
  handleRecoveryItemsRendered: (count: number) => void;
  scheduleActiveQuestionSync: () => void;
  markSurfaceItemsRendered: (count: number) => void;
}) {
  const handleItemsRendered = useCallback((rendered: ListItem<TranscriptRow>[]) => {
    noteTranscriptRowCounts(rendered.length, virtualRowCount);
    reconcileLogicalFocus();
    handleRecoveryItemsRendered(rendered.length);
    scheduleActiveQuestionSync();
    markSurfaceItemsRendered(rendered.length);
    if (!hydrating || scrollModeRef.current === "tail-follow") {
      followGrowingTail("items-rendered");
      if (hydrating) revalidateTail();
    }
  }, [followGrowingTail, handleRecoveryItemsRendered, hydrating, markSurfaceItemsRendered, reconcileLogicalFocus, revalidateTail, scheduleActiveQuestionSync, scrollModeRef, virtualRowCount]);

  const previousReaderActiveRef = useRef(false);
  useEffect(() => {
    const previous = previousReaderActiveRef.current;
    previousReaderActiveRef.current = readerTransactionActive;
    if (previous && !readerTransactionActive && !hydrating) {
      followGrowingTail("items-rendered");
      revalidateTail();
    }
  }, [followGrowingTail, hydrating, readerTransactionActive, revalidateTail]);

  const previousHydratingRef = useRef(hydrating);
  useEffect(() => {
    const previous = previousHydratingRef.current;
    previousHydratingRef.current = hydrating;
    if (previous && !hydrating) {
      followGrowingTail("data-change");
      revalidateTail();
    }
  }, [followGrowingTail, hydrating, revalidateTail]);

  useEffect(() => {
    if (!hydrating) return;
    const interval = setInterval(() => {
      if (scrollModeRef.current === "tail-follow") revalidateTail();
    }, 500);
    return () => clearInterval(interval);
  }, [hydrating, revalidateTail, scrollModeRef]);

  const prependTransientRef = useRef(false);
  useEffect(() => {
    if (historyMutation?.kind !== "prepend") return;
    prependTransientRef.current = true;
    let frame = requestAnimationFrame(() => {
      frame = requestAnimationFrame(() => { prependTransientRef.current = false; });
    });
    return () => {
      cancelAnimationFrame(frame);
      prependTransientRef.current = false;
    };
  }, [historyMutation?.kind, historyMutation?.seq]);

  // Virtuoso's raw height delivery is observational. The coalesced revision
  // owns both diagnostics and any eventual writer decision.
  const handleTotalListHeightChanged = useCallback(() => {
    if ((hydrating && scrollModeRef.current !== "tail-follow") || prependTransientRef.current) return;
    followGrowingTail("row-measure");
    if (hydrating) revalidateTail();
  }, [followGrowingTail, hydrating, revalidateTail, scrollModeRef]);

  return { handleItemsRendered, handleTotalListHeightChanged };
}
