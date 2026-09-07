/**
 * Observability probe for every imperative scroll write against the
 * transcript viewport. The single-writer contract is enforced statically by
 * scripts/check-single-scroll-writer.mjs; this probe is the runtime mirror:
 * tests and diagnostics can observe who wrote, what kind of write, and where
 * it landed, without intercepting the DOM.
 */
import { isFrontendDiagnosticsBuild } from "./frontendDiagnosticsBuild";
import { recordFrontendDiagnostic } from "./frontendDiagnosticBridge";

export type TranscriptScrollWriteRecord = {
  session?: string;
  transaction?: number;
  owner: string;
  intent?: string;
  requestedOffset?: number;
  acceptedOffset?: number;
  outcome?: string;
  kind: "scrollTo" | "scrollBy" | "scrollToIndex" | "pinTail";
  top?: number;
  index?: number | "LAST";
  source?: string;
  phase?: "mount-anchor" | "correct-offset" | "initial" | "settle";
  scrollTop?: number;
  scrollHeight?: number;
  clientHeight?: number;
  bottomDistance?: number;
  mode?: string;
  sequence?: number;
  generation?: number;
  ownershipEpoch?: number;
  geometryRevision?: number;
  transactionId?: number;
  rejectedReason?: string;
  settleFrame?: number;
  offBottomFrames?: number;
  stagnantFrames?: number;
};

type DiagnosticSink = (type: string, fields: Record<string, unknown>) => void;
let diagnosticSink: DiagnosticSink | undefined;
const CAPTURE_SCROLL_DIAGNOSTIC_DETAILS = isFrontendDiagnosticsBuild(
  typeof __BUILD_CHANNEL__ === "string" ? __BUILD_CHANNEL__ : "development",
  Boolean(import.meta.env?.DEV),
);

export function isTranscriptScrollDiagnosticsBuild(channel: string, development: boolean): boolean {
  return isFrontendDiagnosticsBuild(channel, development);
}

export function setTranscriptScrollDiagnosticSink(sink: DiagnosticSink): void {
  diagnosticSink = sink;
}

export function recordTranscriptScrollDiagnostic(type: string, fields: Record<string, unknown> = {}): void {
  diagnosticSink?.(type, fields);
  // Forward the same content-free geometry into the broader frontend timeline.
  recordFrontendDiagnostic("transcript", `transcript.${type}`, fields);
  // The bench harness (desktop/frontend/bench) installs this page-side hook to
  // attach the diagnostic stream to replay failure output.
  if (typeof window !== "undefined") window.__REASONIX_TRANSCRIPT_SCROLL_DIAGNOSTIC__?.(type, fields);
}

declare global {
  interface Window {
    __REASONIX_TRANSCRIPT_SCROLL_WRITE__?: (write: TranscriptScrollWriteRecord) => void;
    __REASONIX_TRANSCRIPT_SCROLL_DIAGNOSTIC__?: (type: string, fields: Record<string, unknown>) => void;
  }
}

export function noteTranscriptScrollWrite(write: TranscriptScrollWriteRecord): void {
  if (CAPTURE_SCROLL_DIAGNOSTIC_DETAILS) {
    recordTranscriptScrollDiagnostic("scroll-write", {
      session: write.session,
      transaction: write.transaction,
      owner: write.owner,
      intent: write.intent,
      requestedOffset: write.requestedOffset,
      acceptedOffset: write.acceptedOffset,
      outcome: write.outcome,
      writeKind: write.kind,
      targetTop: write.top,
      targetIndex: write.index,
      source: write.source,
      phase: write.phase,
      scrollTop: write.scrollTop,
      scrollHeight: write.scrollHeight,
      clientHeight: write.clientHeight,
      bottomDistance: write.bottomDistance,
      mode: write.mode,
      sequence: write.sequence,
      generation: write.generation,
      ownershipEpoch: write.ownershipEpoch,
      geometryRevision: write.geometryRevision,
      transactionId: write.transactionId,
      rejectedReason: write.rejectedReason,
      settleFrame: write.settleFrame,
      offBottomFrames: write.offBottomFrames,
      stagnantFrames: write.stagnantFrames,
    });
  }
  window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__?.(write);
}
