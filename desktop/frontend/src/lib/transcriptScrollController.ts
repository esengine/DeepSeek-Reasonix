export type TranscriptScrollMode =
  | "tail-follow"
  | "manual"
  | "native-selecting"
  | "logical-selecting"
  | "programmatic"
  | "reconciling";

export type TranscriptScrollOwner =
  | "stream"
  | "container-resize"
  | "footer-resize"
  | "jump"
  | "rewind"
  | "jump-bottom"
  | "custom-scrollbar"
  | "selection-edge-scroll"
  | "virtualizer";

export type TranscriptViewportAnchor = {
  rowKey: string;
  viewportOffset: number;
  generation: number;
};

const EXPLICIT_OWNERS = new Set<TranscriptScrollOwner>([
  "jump",
  "rewind",
  "jump-bottom",
  "custom-scrollbar",
]);

export function isTranscriptSelectionMode(mode: TranscriptScrollMode): boolean {
  return mode === "native-selecting" || mode === "logical-selecting";
}

/**
 * Central scroll-write arbitration. Browser-originated scrolling does not use
 * this path; every programmatic scrollTop write must name its owner here.
 */
export function canTranscriptScrollOwnerWrite(mode: TranscriptScrollMode, owner: TranscriptScrollOwner): boolean {
  if (isTranscriptSelectionMode(mode)) return owner === "selection-edge-scroll";
  if (owner === "selection-edge-scroll") return false;
  if (owner === "stream" || owner === "container-resize" || owner === "footer-resize") {
    return mode === "tail-follow";
  }
  if (owner === "virtualizer") return true;
  if (EXPLICIT_OWNERS.has(owner)) return true;
  return mode === "reconciling";
}
