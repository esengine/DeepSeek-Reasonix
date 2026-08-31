import { useCallback, useEffect, useLayoutEffect, useRef, type RefObject } from "react";
import type { ListItem } from "react-virtuoso";
import { noteTranscriptRowCounts } from "./sessionDiagnostics";
import type { TranscriptGeometryChangeSource } from "./transcriptGeometryRevision";
import type { TranscriptHistoryPrependLease } from "./transcriptHistoryPrependLease";
import type { TranscriptScrollMode } from "./transcriptScrollArbiter";
import type { TranscriptRow } from "./transcriptRows";
import type { HistoryMutation } from "./useController";

type ActivePrependSettle = {
  generation: number;
  request: number;
  targetRowCount: number;
  mutationSeq: number;
};

/** Bridges Virtuoso lifecycle delivery into the coalesced geometry controller. */
export function useTranscriptGeometryLifecycle({
  virtualRowCount,
  hydrating,
  readerTransactionActive,
  historyMutation,
  historyPrependLease,
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
  historyPrependLease: TranscriptHistoryPrependLease;
  scrollModeRef: RefObject<TranscriptScrollMode>;
  followGrowingTail: (source: TranscriptGeometryChangeSource) => void;
  revalidateTail: () => void;
  reconcileLogicalFocus: () => void;
  handleRecoveryItemsRendered: (count: number) => void;
  scheduleActiveQuestionSync: () => void;
  markSurfaceItemsRendered: (count: number) => void;
}) {
  const activePrependRef = useRef<ActivePrependSettle | null>(null);
  const syncActivePrepend = useCallback(() => {
    if (!historyPrependLease.pendingRef.current || !historyMutation) {
      activePrependRef.current = null;
      return null;
    }
    const existing = activePrependRef.current;
    const generation = historyPrependLease.generationRef.current;
    const request = historyPrependLease.requestRef.current;
    const ownsRequest = existing?.generation === generation && existing.request === request;
    if (!ownsRequest && historyMutation.seq <= historyPrependLease.mutationBaselineRef.current) return null;
    if (!ownsRequest && historyMutation.kind !== "prepend") {
      historyPrependLease.cancel(generation);
      activePrependRef.current = null;
      return null;
    }
    if (ownsRequest
      && existing.targetRowCount === virtualRowCount
      && existing.mutationSeq === historyMutation.seq) return existing;
    historyPrependLease.noteMutation(generation);
    const active = { generation, request, targetRowCount: virtualRowCount, mutationSeq: historyMutation.seq };
    activePrependRef.current = active;
    return active;
  }, [historyMutation, historyPrependLease, virtualRowCount]);

  const handleItemsRendered = useCallback((rendered: ListItem<TranscriptRow>[]) => {
    noteTranscriptRowCounts(rendered.length, virtualRowCount);
    reconcileLogicalFocus();
    handleRecoveryItemsRendered(rendered.length);
    scheduleActiveQuestionSync();
    markSurfaceItemsRendered(rendered.length);
    if (historyPrependLease.pendingRef.current) {
      const activePrepend = syncActivePrepend();
      if (activePrepend?.targetRowCount === virtualRowCount) {
        historyPrependLease.noteCoverage(activePrepend.generation, rendered.length, virtualRowCount);
      }
      return;
    }
    if (!hydrating || scrollModeRef.current === "tail-follow") {
      followGrowingTail("items-rendered");
      if (hydrating) revalidateTail();
    }
  }, [followGrowingTail, handleRecoveryItemsRendered, historyPrependLease, hydrating, markSurfaceItemsRendered, reconcileLogicalFocus, revalidateTail, scheduleActiveQuestionSync, scrollModeRef, syncActivePrepend, virtualRowCount]);

  const previousReaderActiveRef = useRef(false);
  useEffect(() => {
    const previous = previousReaderActiveRef.current;
    previousReaderActiveRef.current = readerTransactionActive;
    if (previous && !readerTransactionActive && !hydrating && !historyPrependLease.pendingRef.current) {
      followGrowingTail("items-rendered");
      revalidateTail();
    }
  }, [followGrowingTail, historyPrependLease, hydrating, readerTransactionActive, revalidateTail]);

  const previousHydratingRef = useRef(hydrating);
  useEffect(() => {
    const previous = previousHydratingRef.current;
    previousHydratingRef.current = hydrating;
    if (previous && !hydrating && !historyPrependLease.pendingRef.current) {
      followGrowingTail("data-change");
      revalidateTail();
    }
  }, [followGrowingTail, historyPrependLease, hydrating, revalidateTail]);

  useEffect(() => {
    if (!hydrating) return;
    const interval = setInterval(() => {
      if (scrollModeRef.current === "tail-follow") revalidateTail();
    }, 500);
    return () => clearInterval(interval);
  }, [hydrating, revalidateTail, scrollModeRef]);

  useLayoutEffect(() => {
    syncActivePrepend();
  }, [syncActivePrepend]);

  // Virtuoso's raw height delivery is observational. During a prepend the
  // generation-bound reader owner withholds the writer decision.
  const handleTotalListHeightChanged = useCallback(() => {
    if (historyPrependLease.pendingRef.current) return;
    if (hydrating && scrollModeRef.current !== "tail-follow") return;
    followGrowingTail("row-measure");
    if (hydrating) revalidateTail();
  }, [followGrowingTail, historyPrependLease, hydrating, revalidateTail, scrollModeRef]);

  return { handleItemsRendered, handleTotalListHeightChanged };
}
