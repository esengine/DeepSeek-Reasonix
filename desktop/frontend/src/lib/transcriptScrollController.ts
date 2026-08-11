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
  | "row-size"
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
  if (owner === "stream" || owner === "container-resize" || owner === "footer-resize" || owner === "row-size") {
    return mode === "tail-follow";
  }
  if (owner === "virtualizer") return true;
  if (EXPLICIT_OWNERS.has(owner)) return true;
  return mode === "reconciling";
}

/**
 * Whether the virtualizer may compensate the scroll position when a row's
 * measured size changes. Anchored compensation is keyed to the anchor row:
 * while the transcript is pinned to the bottom (tail-follow), compensation
 * lifts the viewport off the tail (or, for the streaming tail row, fights the
 * per-frame repin), and the subsequent bottom-state re-evaluation sees a
 * distance ≥ threshold — which permanently disables auto-scroll. While
 * pinned, size changes must be handled by the stream repin path instead
 * (scheduleRepinIfWasPinned / the row-measurement repin in Transcript), so
 * the virtualizer must not compensate here at all.
 */
export function shouldAdjustScrollOnItemSizeChange(pinned: boolean, mode: TranscriptScrollMode): boolean {
  if (pinned) return false;
  return !isTranscriptSelectionMode(mode);
}

/**
 * Whether a stream-end repin should run: the live stream just transitioned
 * from present to absent (turn_done settles the assistant row) and the
 * transcript is still pinned. The repin then waits a few frames for the
 * synchronous markdown layout to settle; async worker-rendered growth is
 * covered by the row-measurement repin. A user who scrolled away (not pinned)
 * is never yanked back to the bottom.
 */
export function shouldRunStreamEndRepin(hadLive: boolean, hasLive: boolean, pinned: boolean): boolean {
  return hadLive && !hasLive && pinned;
}
