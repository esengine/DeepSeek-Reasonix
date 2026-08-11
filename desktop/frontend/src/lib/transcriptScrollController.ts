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
 *
 * Note: user-gesture locking is layered on top in
 * `canTranscriptScrollOwnerWriteNow` (transcriptScrollSession.ts). Prefer that
 * helper from runtime scroll writers so trackpad gestures are not fought by
 * virtualizer/stream compensation.
 */
export function canTranscriptScrollOwnerWrite(mode: TranscriptScrollMode, owner: TranscriptScrollOwner): boolean {
  if (isTranscriptSelectionMode(mode)) return owner === "selection-edge-scroll";
  if (owner === "selection-edge-scroll") return false;
  if (owner === "stream" || owner === "container-resize" || owner === "footer-resize" || owner === "row-size") {
    return mode === "tail-follow";
  }
  // Virtualizer compensation is allowed outside selection; the session layer
  // (`canTranscriptScrollOwnerWriteNow`) additionally blocks it mid-gesture.
  if (owner === "virtualizer") return true;
  if (EXPLICIT_OWNERS.has(owner)) return true;
  return mode === "reconciling";
}

/**
 * A pinned viewport follows row growth through a bottom repin. Applying anchor
 * compensation at the same time lifts it away from the tail and makes the two
 * writers fight. Detached readers still need anchor compensation to preserve
 * their reading position; selection owns the viewport separately.
 */
export function shouldAdjustScrollOnItemSizeChange(pinned: boolean, mode: TranscriptScrollMode): boolean {
  if (pinned) return false;
  return !isTranscriptSelectionMode(mode);
}

/** Run the layout fallback only for a live-to-settled transition that is still pinned. */
export function shouldRunStreamEndRepin(hadLive: boolean, hasLive: boolean, pinned: boolean): boolean {
  return hadLive && !hasLive && pinned;
}
