import type { RefObject } from "react";
import type { VirtuosoHandle } from "react-virtuoso";
import type { TranscriptScrollMode } from "./transcriptScrollArbiter";
import { nativeTranscriptDistanceFromBottom } from "./transcriptScrollGeometry";
import { noteTranscriptScrollWrite } from "./transcriptScrollProbe";

export type TranscriptScrollWriterRequest = {
  owner: string;
  operation: "scrollTo" | "scrollBy" | "scrollToIndex" | "pinTail";
  reason: string;
  top?: number;
  index?: number | "LAST";
  behavior?: ScrollBehavior;
  align?: "start" | "center" | "end";
  phase?: "mount-anchor" | "correct-offset" | "initial" | "settle";
  expectedSurfaceGeneration: number;
  expectedOwnershipEpoch: number;
  expectedGeometryRevision: number;
  transactionId?: number;
  settleFrame?: number;
  offBottomFrames?: number;
  stagnantFrames?: number;
};

export type TranscriptScrollWriter = {
  write: (request: TranscriptScrollWriterRequest) => boolean;
  lastOwner: () => string | undefined;
};

/** The only production gateway allowed to issue imperative Transcript writes. */
export function createTranscriptScrollWriter({
  virtuosoRef,
  scrollRef,
  modeRef,
  generationRef,
  ownershipEpochRef,
  geometryRevisionRef,
}: {
  virtuosoRef: RefObject<VirtuosoHandle | null>;
  scrollRef: RefObject<HTMLDivElement | null>;
  modeRef: RefObject<TranscriptScrollMode>;
  generationRef: RefObject<number>;
  ownershipEpochRef: RefObject<number>;
  geometryRevisionRef: RefObject<number>;
}): TranscriptScrollWriter {
  let sequence = 0;
  let previousOwner: string | undefined;
  const accepted = new Set<string>();
  let acceptedContext = "";

  const write = (request: TranscriptScrollWriterRequest): boolean => {
    const handle = virtuosoRef.current;
    const element = scrollRef.current;
    const generation = generationRef.current;
    const epoch = ownershipEpochRef.current;
    const revision = geometryRevisionRef.current;
    const context = `${generation}:${epoch}:${revision}`;
    if (context !== acceptedContext) {
      acceptedContext = context;
      accepted.clear();
    }
    let rejectedReason: string | undefined;
    if (!handle || !element) rejectedReason = "surface-unavailable";
    else if (modeRef.current === "native-thumb") rejectedReason = "native-thumb-owner";
    else if (request.expectedSurfaceGeneration !== generation) rejectedReason = "stale-surface-generation";
    else if (request.expectedOwnershipEpoch !== epoch) rejectedReason = "stale-ownership-epoch";
    else if (request.expectedGeometryRevision !== revision) rejectedReason = "stale-geometry-revision";
    else if (request.operation === "scrollToIndex" ? request.index === undefined : request.top === undefined) rejectedReason = "invalid-target";
    const acceptanceKey = `${request.owner}:${epoch}:${revision}:${request.settleFrame ?? request.phase ?? request.operation}`;
    if (!rejectedReason && accepted.has(acceptanceKey)) rejectedReason = "duplicate-revision-phase";

    sequence += 1;
    noteTranscriptScrollWrite({
      owner: request.owner,
      kind: request.operation,
      top: request.top,
      index: request.index,
      source: request.reason,
      phase: request.phase,
      scrollTop: element?.scrollTop,
      scrollHeight: element?.scrollHeight,
      clientHeight: element?.clientHeight,
      bottomDistance: element ? nativeTranscriptDistanceFromBottom(element) : undefined,
      mode: modeRef.current,
      sequence,
      generation,
      ownershipEpoch: epoch,
      geometryRevision: revision,
      transactionId: request.transactionId,
      rejectedReason,
      settleFrame: request.settleFrame,
      offBottomFrames: request.offBottomFrames,
      stagnantFrames: request.stagnantFrames,
    });
    if (rejectedReason || !handle || !element) return false;

    accepted.add(acceptanceKey);
    previousOwner = request.owner;

    const behavior = request.behavior === "smooth" ? "smooth" : "auto";
    // pinTail targets the scroller's current physical extent. Routing it back
    // through Virtuoso can defer the write against a stale size tree while the
    // measured range is collapsing/rebounding, leaving an accepted command as
    // a no-op. The writer is also the sole gateway for direct Transcript DOM
    // writes, so this keeps ownership/generation/revision fencing intact.
    if (request.operation === "pinTail") {
      if (typeof element.scrollTo === "function") element.scrollTo({ top: request.top!, behavior });
      else element.scrollTop = request.top!;
    } else if (request.operation === "scrollTo") handle.scrollTo({ top: request.top!, behavior });
    else if (request.operation === "scrollBy") handle.scrollBy({ top: request.top!, behavior });
    else handle.scrollToIndex({ index: request.index!, align: request.align ?? "start", behavior });
    return true;
  };

  return { write, lastOwner: () => previousOwner };
}
