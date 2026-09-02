import type { RefObject } from "react";

export const TRANSCRIPT_READER_FULL_MOUNT_ROW_LIMIT = 1_000;

export type TranscriptHistoryPrependLease = {
  pendingRef: RefObject<boolean>;
  generationRef: RefObject<number>;
  requestRef: RefObject<number>;
  mutationBaselineRef: RefObject<number>;
  begin: (mutationSeq: number) => number;
  noteMutation: (generation: number) => void;
  noteCoverage: (generation: number, mounted: number, total: number) => void;
  cancel: (generation: number) => boolean;
};

type TranscriptHistoryPrependRuntime = {
  layoutTransientRef: RefObject<boolean>;
  publishPending: (pending: boolean) => void;
  holdReaderGeometryCommit: (captureAnchor: boolean) => void;
  readerAnchorIsMounted: () => boolean;
  readerTransactionIsActive: () => boolean;
  commitGeometry: () => void;
};

export type TranscriptHistoryPrependCoordinator = {
  pendingRef: RefObject<boolean>;
  commitReadyRef: RefObject<boolean>;
  stableAnchorRef: RefObject<boolean>;
  lease: TranscriptHistoryPrependLease;
  bind: (runtime: TranscriptHistoryPrependRuntime) => void;
  noteGeometryCommitReady: () => void;
  noteReaderTerminal: (cancelled: boolean) => void;
  invalidate: () => void;
};

/** Owns one or more contiguous history pages without becoming a scroll writer. */
export function createTranscriptHistoryPrependCoordinator(): TranscriptHistoryPrependCoordinator {
  const pendingRef = { current: false };
  const generationRef = { current: 0 };
  const requestRef = { current: 0 };
  const mutationBaselineRef = { current: 0 };
  const commitReadyRef = { current: false };
  const stableAnchorRef = { current: false };
  const coverageReadyRef = { current: false };
  let runtime: TranscriptHistoryPrependRuntime | undefined;

  const clear = (preserveStableAnchor = false) => {
    pendingRef.current = false;
    commitReadyRef.current = false;
    coverageReadyRef.current = false;
    if (!preserveStableAnchor) stableAnchorRef.current = false;
    if (runtime) {
      runtime.layoutTransientRef.current = false;
      runtime.publishPending(false);
    }
  };
  const finish = (generation: number) => {
    if (!pendingRef.current || generationRef.current !== generation) return false;
    if (!coverageReadyRef.current || (runtime?.readerTransactionIsActive() && !commitReadyRef.current)) return false;
    const preserveStableAnchor = Boolean(runtime?.readerTransactionIsActive());
    stableAnchorRef.current = preserveStableAnchor;
    clear(preserveStableAnchor);
    runtime?.commitGeometry();
    return true;
  };
  const begin = (mutationSeq: number) => {
    const continuing = pendingRef.current;
    if (!continuing) generationRef.current += 1;
    requestRef.current += 1;
    mutationBaselineRef.current = mutationSeq;
    pendingRef.current = true;
    commitReadyRef.current = false;
    coverageReadyRef.current = false;
    if (runtime) {
      runtime.layoutTransientRef.current = true;
      runtime.publishPending(true);
      runtime.holdReaderGeometryCommit(!continuing);
    }
    return generationRef.current;
  };
  const noteMutation = (generation: number) => {
    if (!pendingRef.current || generationRef.current !== generation) return;
    commitReadyRef.current = false;
    coverageReadyRef.current = false;
    runtime?.holdReaderGeometryCommit(false);
  };
  const noteCoverage = (generation: number, mounted: number, total: number) => {
    if (!pendingRef.current || generationRef.current !== generation) return;
    coverageReadyRef.current = mounted >= total || (
      total > TRANSCRIPT_READER_FULL_MOUNT_ROW_LIMIT
      && mounted > 0
      && Boolean(runtime?.readerAnchorIsMounted())
    );
    finish(generation);
  };
  const cancel = (generation: number) => {
    if (!pendingRef.current || generationRef.current !== generation) return false;
    clear();
    return true;
  };
  const lease = {
    pendingRef, generationRef, requestRef, mutationBaselineRef,
    begin, noteMutation, noteCoverage, cancel,
  };

  return {
    pendingRef,
    commitReadyRef,
    stableAnchorRef,
    lease,
    bind: (nextRuntime) => {
      runtime = nextRuntime;
      runtime.publishPending(pendingRef.current);
    },
    noteGeometryCommitReady: () => {
      commitReadyRef.current = true;
      finish(generationRef.current);
    },
    noteReaderTerminal: (cancelled) => {
      if (!pendingRef.current) return;
      if (cancelled) clear();
      else finish(generationRef.current);
    },
    invalidate: () => {
      generationRef.current += 1;
      clear();
    },
  };
}
