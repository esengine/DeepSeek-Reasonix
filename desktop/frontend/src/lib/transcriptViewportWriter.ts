import { noteTranscriptScrollWrite, recordTranscriptScrollDiagnostic } from "./transcriptScrollProbe";
import type { TranscriptWriteRequest, TranscriptWriteResult } from "./transcriptKernel";

/** The only production owner allowed to mutate the native Transcript scroll
 * position. Every adapter, gesture, and transaction routes through this class. */
export class TranscriptViewportWriter {
  private element: HTMLElement | null = null;
  private generation = 0;
  private frozen = false;

  attach(element: HTMLElement | null, generation: number): void {
    this.element = element;
    this.generation = generation;
  }

  freeze(value: boolean): void {
    this.frozen = value;
  }

  write = (request: TranscriptWriteRequest): TranscriptWriteResult => {
    const element = this.element;
    if (!element) return this.reject(request, "no-viewport");
    if (request.generation !== this.generation) return this.reject(request, "stale-generation");
    if (this.frozen) return this.reject(request, "user-gesture");
    if (!Number.isFinite(element.scrollHeight) || !Number.isFinite(element.clientHeight)) {
      return this.reject(request, "invalid-geometry");
    }
    const maximum = Math.max(0, element.scrollHeight - element.clientHeight);
    const accepted = Number.isFinite(request.offset)
      ? Math.max(0, Math.min(maximum, request.offset))
      : maximum;
    const before = element.scrollTop;
    const noOp = Math.abs(before - accepted) <= 0.5;
    if (!noOp) element.scrollTop = accepted;
    const landed = element.scrollTop;
    const acceptedByNative = Math.abs(landed - accepted) <= 4;
    noteTranscriptScrollWrite({
      session: request.session,
      transaction: request.transactionId,
      owner: request.owner,
      intent: request.intent,
      requestedOffset: request.offset,
      acceptedOffset: landed,
      outcome: noOp ? "no-op" : acceptedByNative ? "accepted" : "native-clamp",
      kind: request.owner === "tail-follow" ? "pinTail" : "scrollTo",
      top: request.offset,
      scrollTop: landed,
      scrollHeight: element.scrollHeight,
      clientHeight: element.clientHeight,
      bottomDistance: maximum - landed,
      mode: request.intent,
      generation: request.generation,
      geometryRevision: request.geometryRevision,
      transactionId: request.transactionId,
    });
    return { accepted: acceptedByNative, offset: landed, changed: Math.abs(landed - before) > 0.5, reason: acceptedByNative ? undefined : "native-clamp" };
  };

  private reject(request: TranscriptWriteRequest, reason: string): TranscriptWriteResult {
    const offset = this.element?.scrollTop ?? 0;
    recordTranscriptScrollDiagnostic("scroll-write-rejected", {
      session: request.session,
      generation: request.generation,
      transaction: request.transactionId,
      owner: request.owner,
      intent: request.intent,
      geometryRevision: request.geometryRevision,
      requestedOffset: request.offset,
      acceptedOffset: offset,
      outcome: reason,
    });
    noteTranscriptScrollWrite({
      session: request.session,
      transaction: request.transactionId,
      owner: request.owner,
      intent: request.intent,
      requestedOffset: request.offset,
      acceptedOffset: offset,
      outcome: reason,
      kind: "scrollTo",
      top: request.offset,
      scrollTop: offset,
      mode: request.intent,
      generation: request.generation,
      geometryRevision: request.geometryRevision,
      transactionId: request.transactionId,
      rejectedReason: reason,
    });
    return { accepted: false, offset, reason };
  }
}
