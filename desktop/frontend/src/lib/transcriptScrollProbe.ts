/**
 * Observability probe for every imperative scroll write against the
 * transcript Virtuoso handle. The single-writer contract (only the scroll
 * arbiter may call `virtuosoRef.current.scroll*`) is enforced statically by
 * scripts/check-single-scroll-writer.mjs; this probe is the runtime mirror:
 * tests and diagnostics can observe who wrote, what kind of write, and where
 * it landed, without intercepting the DOM.
 */
import { isFrontendDiagnosticsBuild } from "./frontendDiagnosticsBuild";
import { recordFrontendDiagnostic } from "./frontendDiagnosticBridge";
import { isStableCompactTranscriptVariant, isTranscriptRowLayoutVariant } from "./transcriptRowGeometry";

export type TranscriptScrollWriteRecord = {
  /** Logical writer, e.g. "tail-follow", "jump", "recovery", or a
   *  TranscriptScrollOwner such as "selection-edge-scroll". */
  owner: string;
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
  // Keep the legacy scroll trace intact while forwarding the same content-free
  // geometry into the broader frontend interaction timeline.
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
      owner: write.owner,
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

function finiteDatasetNumber(value: string | undefined): number | undefined {
  const parsed = Number.parseFloat(value ?? "");
  return Number.isFinite(parsed) ? parsed : undefined;
}

function rowFoldState(element: HTMLElement): { foldState: "none" | "open" | "closed" | "mixed"; disclosureCount: number } {
  const disclosures = Array.from(element.querySelectorAll<HTMLElement>("[aria-expanded]"));
  const layoutElement = element.querySelector<HTMLElement>("[data-transcript-layout-variant]");
  const layoutVariant = layoutElement?.dataset.transcriptLayoutVariant ?? element.dataset.transcriptLayoutVariant;
  if (isTranscriptRowLayoutVariant(layoutVariant)) {
    if (layoutVariant.endsWith("-expanded")) return { foldState: "open", disclosureCount: disclosures.length };
    if (layoutVariant !== "static" && layoutVariant !== "text-flow") return { foldState: "closed", disclosureCount: disclosures.length };
  }
  if (disclosures.length === 0) return { foldState: "none", disclosureCount: 0 };
  const states = new Set(disclosures.map((node) => node.getAttribute("aria-expanded") === "true"));
  return {
    foldState: states.size > 1 ? "mixed" : states.has(true) ? "open" : "closed",
    disclosureCount: disclosures.length,
  };
}

/** Records only geometry and fixed row classifications; text and row keys never leave the DOM. */
export function noteTranscriptRowMeasurement(element: HTMLElement, field: "offsetHeight" | "offsetWidth", measuredSize: number): void {
  if (field !== "offsetHeight") return;
  // A physical Virtuoso row can be rebound to a different logical row before
  // its known size is refreshed. Treat that size as recycled-node metadata,
  // not as the current row's geometry contract.
  const previousSize = element.dataset.transcriptRecycled === "true"
    ? undefined
    : finiteDatasetNumber(element.dataset.knownSize);
  const estimatedSize = finiteDatasetNumber(element.dataset.estimatedSize);
  const comparisonSize = previousSize ?? estimatedSize;
  if (comparisonSize !== undefined && Math.abs(measuredSize - comparisonSize) <= 0.5) return;
  const rowIndex = finiteDatasetNumber(element.dataset.logicalIndex) ?? finiteDatasetNumber(element.dataset.index);
  const contentRevision = finiteDatasetNumber(element.dataset.contentRevision);
  const layoutVersion = element.dataset.layoutVersion;
  const layoutElement = element.querySelector<HTMLElement>("[data-transcript-layout-variant]");
  const layoutVariant = layoutElement?.dataset.transcriptLayoutVariant ?? element.dataset.transcriptLayoutVariant;
  const estimateSource = element.dataset.estimateSource;
  const { foldState, disclosureCount } = rowFoldState(element);
  recordTranscriptScrollDiagnostic("row-measure", {
    rowIndex,
    rowKind: element.dataset.rowKind,
    estimatedSize,
    previousSize,
    measuredSize,
    sizeDelta: comparisonSize === undefined ? undefined : measuredSize - comparisonSize,
    contentRevision,
    ...(layoutVersion ? { layoutVersion } : {}),
    ...(isTranscriptRowLayoutVariant(layoutVariant) ? { layoutVariant } : {}),
    ...(estimateSource ? { estimateSource } : {}),
    foldState,
    disclosureCount,
  });
  if (comparisonSize !== undefined && isTranscriptRowLayoutVariant(layoutVariant) && isStableCompactTranscriptVariant(layoutVariant)) {
    const absoluteError = Math.abs(measuredSize - comparisonSize);
    const relativeError = comparisonSize > 0 ? absoluteError / comparisonSize : 0;
    if (absoluteError > 8 || relativeError > 0.2) {
      recordTranscriptScrollDiagnostic("geometry-contract-violation", {
        rowIndex,
        rowKind: element.dataset.rowKind,
        estimatedSize: comparisonSize,
        measuredSize,
        sizeDelta: measuredSize - comparisonSize,
        relativeError,
        layoutVariant,
        ...(estimateSource ? { estimateSource } : {}),
      });
    }
  }
}
