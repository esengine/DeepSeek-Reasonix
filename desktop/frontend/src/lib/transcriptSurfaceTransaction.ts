import { recordTranscriptScrollDiagnostic } from "./transcriptScrollProbe";

export type TranscriptSurfaceTransactionKind = "reader-prepend" | "question-jump" | "scrollbar-drag";
export type TranscriptSurfaceTransactionPhase = "loading" | "mutating" | "mounting" | "settling" | "committed" | "cancelled";

export type TranscriptSurfaceTransaction = {
  token: number;
  kind: TranscriptSurfaceTransactionKind;
  phase: TranscriptSurfaceTransactionPhase;
  surfaceGeneration: number;
  ownershipEpoch: number;
  geometryRevision: number;
  mutationSeq: number;
  anchor?: { rowKey: string; logicalIndex: number; viewportOffset: number };
};

/**
 * Shared token/phase bookkeeping for asynchronous transcript owners.  The
 * controller does not write scrollTop; it only fences stale completions and
 * emits content-free evidence for field replays.
 */
export function createTranscriptSurfaceTransactions() {
  let nextToken = 0;
  let current: TranscriptSurfaceTransaction | null = null;

  const publish = (transaction: TranscriptSurfaceTransaction, result?: string) => {
    recordTranscriptScrollDiagnostic("surface-transaction", {
      transactionId: transaction.token,
      source: transaction.kind,
      transactionKind: transaction.kind,
      phase: transaction.phase,
      result,
      generation: transaction.surfaceGeneration,
      ownershipEpoch: transaction.ownershipEpoch,
      geometryRevision: transaction.geometryRevision,
      sequence: transaction.mutationSeq,
      anchorIndex: transaction.anchor?.logicalIndex,
      anchorOffset: transaction.anchor?.viewportOffset,
    });
  };

  return {
    begin(input: Omit<TranscriptSurfaceTransaction, "token" | "phase">): TranscriptSurfaceTransaction {
      const transaction: TranscriptSurfaceTransaction = { ...input, token: ++nextToken, phase: "loading" };
      current = transaction;
      publish(transaction, "begin");
      return transaction;
    },
    update(token: number, patch: Partial<Omit<TranscriptSurfaceTransaction, "token">>, result = "update"): boolean {
      if (!current || current.token !== token) return false;
      current = { ...current, ...patch };
      publish(current, result);
      return true;
    },
    isCurrent(token: number): boolean { return current?.token === token; },
    finish(token: number, phase: "committed" | "cancelled", result: string = phase): boolean {
      if (!current || current.token !== token) return false;
      current = { ...current, phase };
      publish(current, result);
      current = null;
      return true;
    },
    current: () => current,
  };
}
