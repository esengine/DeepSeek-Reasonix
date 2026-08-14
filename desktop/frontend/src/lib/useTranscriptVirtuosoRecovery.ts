import { useCallback, useEffect, useRef, useState, type RefObject } from "react";
import type { VirtuosoHandle } from "react-virtuoso";
import type { TranscriptRow } from "./transcriptRows";
import { useTranscriptVirtuosoFirstItemIndex } from "./transcriptVirtuosoIndex";
import {
  captureTranscriptLayoutAnchor,
  transcriptAnchorInitialLocation,
  transcriptElementViewportIsBlank,
  type TranscriptLayoutAnchor,
} from "./transcriptVirtuosoRecovery";

const LAYOUT_INVALIDATION_BATCH_MS = 48;

/** Rebuilds stale Virtuoso size trees while preserving the logical viewport. */
export function useTranscriptVirtuosoRecovery({
  surfaceKey,
  historyLayoutRevision,
  rows,
  rowIndexByKey,
  scrollRef,
  pinnedRef,
  virtuosoRef,
  readyRef,
  scrollToBottom,
}: {
  surfaceKey: string;
  historyLayoutRevision: number;
  rows: readonly TranscriptRow[];
  rowIndexByKey: ReadonlyMap<string, number>;
  scrollRef: RefObject<HTMLDivElement | null>;
  pinnedRef: RefObject<boolean>;
  virtuosoRef: RefObject<VirtuosoHandle | null>;
  readyRef: RefObject<boolean>;
  scrollToBottom: () => void;
}) {
  const [resetEpoch, setResetEpoch] = useState(0);
  const appliedRevisionRef = useRef(historyLayoutRevision);
  const latestRevisionRef = useRef(historyLayoutRevision);
  const resetTimerRef = useRef<number | null>(null);
  const blankCheckFrameRef = useRef<number | null>(null);
  const pendingAnchorRef = useRef<{ surfaceKey: string; anchor: TranscriptLayoutAnchor } | null>(null);
  const stableManualAnchorRef = useRef<Extract<TranscriptLayoutAnchor, { mode: "manual" }> | null>(null);
  const lastBlankRecoveryRef = useRef("");
  latestRevisionRef.current = historyLayoutRevision;

  useEffect(() => {
    appliedRevisionRef.current = latestRevisionRef.current;
    pendingAnchorRef.current = null;
    stableManualAnchorRef.current = null;
    lastBlankRecoveryRef.current = "";
    if (resetTimerRef.current !== null) window.clearTimeout(resetTimerRef.current);
    if (blankCheckFrameRef.current !== null) cancelAnimationFrame(blankCheckFrameRef.current);
    resetTimerRef.current = null;
    blankCheckFrameRef.current = null;
  }, [surfaceKey]);

  useEffect(() => () => {
    if (resetTimerRef.current !== null) window.clearTimeout(resetTimerRef.current);
    if (blankCheckFrameRef.current !== null) cancelAnimationFrame(blankCheckFrameRef.current);
  }, []);

  const requestReset = useCallback((): boolean => {
    const element = scrollRef.current;
    if (!element || pendingAnchorRef.current?.surfaceKey === surfaceKey) return false;
    const anchor = stableManualAnchorRef.current
      ?? (pinnedRef.current ? { mode: "tail" } as const : captureTranscriptLayoutAnchor(element, false));
    if (!anchor) return false;
    pendingAnchorRef.current = { surfaceKey, anchor };
    readyRef.current = false;
    setResetEpoch((epoch) => epoch + 1);
    return true;
  }, [pinnedRef, readyRef, scrollRef, surfaceKey]);

  useEffect(() => {
    if (appliedRevisionRef.current === historyLayoutRevision) return;
    if (resetTimerRef.current !== null) window.clearTimeout(resetTimerRef.current);
    const flush = () => {
      resetTimerRef.current = null;
      if (pendingAnchorRef.current?.surfaceKey === surfaceKey) {
        // A previous rebuild may still be restoring its anchor. Retry instead
        // of dropping a later lazy-content invalidation in that narrow window.
        resetTimerRef.current = window.setTimeout(flush, LAYOUT_INVALIDATION_BATCH_MS);
        return;
      }
      // If the surface disappeared during the batch window, there is nothing
      // left to rebuild. Mark the revision consumed rather than polling forever.
      requestReset();
      appliedRevisionRef.current = historyLayoutRevision;
    };
    resetTimerRef.current = window.setTimeout(flush, LAYOUT_INVALIDATION_BATCH_MS);
    return () => {
      if (resetTimerRef.current !== null) window.clearTimeout(resetTimerRef.current);
      resetTimerRef.current = null;
    };
  }, [historyLayoutRevision, requestReset, surfaceKey]);

  const resetKey = `${surfaceKey}:${resetEpoch}`;
  const firstItemIndex = useTranscriptVirtuosoFirstItemIndex(rows, resetKey);
  const pendingAnchor = pendingAnchorRef.current?.surfaceKey === surfaceKey ? pendingAnchorRef.current.anchor : undefined;
  const restoreLocation = transcriptAnchorInitialLocation(pendingAnchor, rowIndexByKey, firstItemIndex);

  const scheduleBlankViewportCheck = useCallback(() => {
    if (
      appliedRevisionRef.current === historyLayoutRevision
      && resetTimerRef.current === null
      && pendingAnchorRef.current?.surfaceKey !== surfaceKey
    ) {
      const element = scrollRef.current;
      const anchor = element ? captureTranscriptLayoutAnchor(element, pinnedRef.current) : undefined;
      if (anchor?.mode === "manual") stableManualAnchorRef.current = anchor;
      else if (anchor?.mode === "tail") stableManualAnchorRef.current = null;
    }
    if (
      blankCheckFrameRef.current !== null
      || resetTimerRef.current !== null
      || pendingAnchorRef.current?.surfaceKey === surfaceKey
    ) return;
    blankCheckFrameRef.current = requestAnimationFrame(() => {
      blankCheckFrameRef.current = requestAnimationFrame(() => {
        blankCheckFrameRef.current = null;
        const element = scrollRef.current;
        if (!element || !transcriptElementViewportIsBlank(element)) return;
        const recoveryKey = `${surfaceKey}:${historyLayoutRevision}:${Math.round(element.scrollTop)}`;
        if (lastBlankRecoveryRef.current === recoveryKey) return;
        lastBlankRecoveryRef.current = recoveryKey;
        requestReset();
      });
    });
  }, [historyLayoutRevision, pinnedRef, requestReset, scrollRef, surfaceKey]);

  const restoreAnchor = useCallback((anchor: TranscriptLayoutAnchor) => {
    if (pendingAnchorRef.current?.surfaceKey !== surfaceKey) return;
    if (anchor.mode === "tail") {
      pendingAnchorRef.current = null;
      scrollToBottom();
      return;
    }
    const restore = (remainingAttempts: number, stableFrames: number) => {
      if (pendingAnchorRef.current?.surfaceKey !== surfaceKey) return;
      const element = scrollRef.current;
      if (!element) {
        pendingAnchorRef.current = null;
        return;
      }
      const row = Array.from(element.querySelectorAll<HTMLElement>(".transcript__row[data-row-key]"))
        .find((candidate) => candidate.dataset.rowKey === anchor.rowKey);
      if (!row && remainingAttempts > 0) {
        const location = transcriptAnchorInitialLocation(anchor, rowIndexByKey, firstItemIndex);
        if (location) virtuosoRef.current?.scrollToIndex(location);
        requestAnimationFrame(() => restore(remainingAttempts - 1, 0));
        return;
      }
      if (!row) {
        pendingAnchorRef.current = null;
        return;
      }
      const viewportTop = element.getBoundingClientRect().top;
      const correction = row.getBoundingClientRect().top - viewportTop - anchor.offset;
      if (Math.abs(correction) > 1) virtuosoRef.current?.scrollBy({ top: correction, behavior: "auto" });
      const nextStableFrames = Math.abs(correction) <= 1 ? stableFrames + 1 : 0;
      if (remainingAttempts > 0 && nextStableFrames < 2) {
        requestAnimationFrame(() => restore(remainingAttempts - 1, nextStableFrames));
        return;
      }
      pendingAnchorRef.current = null;
      stableManualAnchorRef.current = anchor;
    };
    requestAnimationFrame(() => restore(8, 0));
  }, [firstItemIndex, rowIndexByKey, scrollRef, scrollToBottom, virtuosoRef]);

  const handleItemsRendered = useCallback((renderedCount: number) => {
    if (!readyRef.current && renderedCount > 0) {
      readyRef.current = true;
      const pending = pendingAnchorRef.current;
      if (pending?.surfaceKey === surfaceKey) restoreAnchor(pending.anchor);
      else requestAnimationFrame(scrollToBottom);
    }
    scheduleBlankViewportCheck();
  }, [readyRef, restoreAnchor, scheduleBlankViewportCheck, scrollToBottom, surfaceKey]);

  return {
    resetKey,
    firstItemIndex,
    restoreLocation,
    handleItemsRendered,
    scheduleBlankViewportCheck,
  };
}
