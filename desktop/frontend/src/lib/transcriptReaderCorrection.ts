import type { RefObject } from "react";
import type { TranscriptScrollWriteRecord } from "./transcriptScrollProbe";
import type { TranscriptScrollWriter } from "./transcriptScrollWriter";

export type TranscriptReaderCorrectionWriter = (write: TranscriptScrollWriteRecord) => boolean;

/** Adapts reader corrections to the generation-fenced single writer. */
export function createTranscriptReaderCorrectionWriter({
  writer,
  generationRef,
  ownershipEpochRef,
  geometryRevisionRef,
}: {
  writer: TranscriptScrollWriter;
  generationRef: RefObject<number>;
  ownershipEpochRef: RefObject<number>;
  geometryRevisionRef: RefObject<number>;
}): TranscriptReaderCorrectionWriter {
  return (write) => {
    if (write.kind === "scrollToIndex" ? write.index === undefined : write.top === undefined) return false;
    return writer.write({
      owner: write.owner,
      operation: write.kind === "pinTail" ? "pinTail" : write.kind === "scrollToIndex" ? "scrollToIndex" : "scrollBy",
      top: write.top,
      index: write.index,
      align: write.kind === "scrollToIndex" ? "start" : undefined,
      behavior: "auto",
      reason: write.source ?? "reader-stability",
      expectedSurfaceGeneration: generationRef.current,
      expectedOwnershipEpoch: ownershipEpochRef.current,
      expectedGeometryRevision: geometryRevisionRef.current,
      transactionId: write.transactionId,
      phase: write.phase,
    });
  };
}
