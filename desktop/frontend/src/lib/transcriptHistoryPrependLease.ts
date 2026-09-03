import type { RefObject } from "react";
import { createTranscriptSurfaceTransactions, type TranscriptSurfaceTransaction } from "./transcriptSurfaceTransaction";

export type TranscriptHistoryPrependLease = {
  pendingRef: RefObject<boolean>;
  generationRef: RefObject<number>;
  requestRef: RefObject<number>;
  mutationBaselineRef: RefObject<number>;
  begin: (mutationSeq: number) => number;
  noteMutation: (generation: number, mutationSeq?: number) => void;
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
  transactionContext?: () => Pick<TranscriptSurfaceTransaction, "surfaceGeneration" | "ownershipEpoch" | "geometryRevision">;
  captureAnchor?: () => TranscriptSurfaceTransaction["anchor"];
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
  currentTransaction: () => TranscriptSurfaceTransaction | null;
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
  const surfaceTransactions = createTranscriptSurfaceTransactions();
  let surfaceTransaction: TranscriptSurfaceTransaction | null = null;

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
    if (surfaceTransaction) {
      surfaceTransactions.update(surfaceTransaction.token, { phase: "settling" }, "coverage-ready");
      surfaceTransactions.finish(surfaceTransaction.token, "committed");
      surfaceTransaction = null;
    }
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
    if (!continuing) {
      surfaceTransaction = surfaceTransactions.begin({
        kind: "reader-prepend",
        ...(runtime?.transactionContext?.() ?? {
          surfaceGeneration: generationRef.current,
          ownershipEpoch: 0,
          geometryRevision: 0,
        }),
        mutationSeq,
        anchor: runtime?.captureAnchor?.(),
      });
    } else if (surfaceTransaction) {
      surfaceTransactions.update(surfaceTransaction.token, { phase: "mutating", mutationSeq }, "next-page");
    }
    if (runtime) {
      runtime.layoutTransientRef.current = true;
      runtime.publishPending(true);
      runtime.holdReaderGeometryCommit(!continuing);
    }
    return generationRef.current;
  };
  const noteMutation = (generation: number, mutationSeq = mutationBaselineRef.current) => {
    if (!pendingRef.current || generationRef.current !== generation) return;
    commitReadyRef.current = false;
    coverageReadyRef.current = false;
    if (surfaceTransaction) {
      surfaceTransactions.update(surfaceTransaction.token, {
        phase: "mutating",
        mutationSeq,
        anchor: runtime?.captureAnchor?.() ?? surfaceTransaction.anchor,
      }, "mutation");
    }
    runtime?.holdReaderGeometryCommit(false);
  };
  const noteCoverage = (generation: number, mounted: number, _total: number) => {
    if (!pendingRef.current || generationRef.current !== generation) return;
    // Never require the whole list to mount. A bounded reader corridor is
    // sufficient as long as the logical anchor is mounted; full-list mounts
    // make every Markdown row measure at once and recreate the field jump.
    coverageReadyRef.current = mounted > 0 && Boolean(runtime?.readerAnchorIsMounted());
    if (surfaceTransaction) surfaceTransactions.update(surfaceTransaction.token, { phase: "mounting" }, "coverage");
    finish(generation);
  };
  const cancel = (generation: number) => {
    if (!pendingRef.current || generationRef.current !== generation) return false;
    if (surfaceTransaction) {
      surfaceTransactions.finish(surfaceTransaction.token, "cancelled", "cancel");
      surfaceTransaction = null;
    }
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
      if (cancelled) {
        if (surfaceTransaction) {
          surfaceTransactions.finish(surfaceTransaction.token, "cancelled", "reader-cancelled");
          surfaceTransaction = null;
        }
        clear();
      }
      else finish(generationRef.current);
    },
    invalidate: () => {
      generationRef.current += 1;
      if (surfaceTransaction) {
        surfaceTransactions.finish(surfaceTransaction.token, "cancelled", "invalidate");
        surfaceTransaction = null;
      }
      clear();
    },
    currentTransaction: () => surfaceTransaction,
  };
}
