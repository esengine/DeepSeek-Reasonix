import {
  canTranscriptScrollOwnerWrite,
  isTranscriptSelectionMode,
  type TranscriptScrollMode,
  type TranscriptScrollOwner,
} from "./transcriptScrollController";

/**
 * How long after the last vertical wheel/touch sample we treat the user as
 * still gesturing. During this window, virtualizer/stream/resize must not
 * rewrite scrollTop — that fight is the main source of trackpad stall/jump.
 */
export const GESTURE_HOLD_MS = 220;
export const SCROLLEND_QUIET_MS = 48;

export type TranscriptUserScrollSource =
  | "wheel"
  | "touch"
  | "keyboard"
  | "middle-button"
  | "native-scroll"
  | "nested-scroll";

/** Owners that compete with native trackpad inertia and must stay silent mid-gesture. */
const GESTURE_BLOCKED_OWNERS = new Set<TranscriptScrollOwner>([
  "virtualizer",
  "stream",
  "container-resize",
  "footer-resize",
  "row-size",
]);

export function noteUserGesture(now = Date.now(), holdMs = GESTURE_HOLD_MS): number {
  return now + holdMs;
}

export function isUserGestureActive(gestureUntil: number, now = Date.now()): boolean {
  return gestureUntil > now;
}

/** Reject a late scrollend from an older gesture if fresh input just arrived. */
export function canScrollEndSettle(
  lastActivityAt: number,
  now = Date.now(),
  quietMs = SCROLLEND_QUIET_MS,
): boolean {
  return lastActivityAt > 0 && now - lastActivityAt >= quietMs;
}

/**
 * Scroll-write arbitration with a short-lived user-gesture lock layered on top
 * of the existing mode/owner matrix.
 *
 * Explicit user actions (jump, jump-bottom, custom scrollbar, selection edge)
 * still pass when the base matrix allows them. Compensating writers do not.
 */
export function canTranscriptScrollOwnerWriteNow(
  mode: TranscriptScrollMode,
  owner: TranscriptScrollOwner,
  gestureUntil: number,
  now = Date.now(),
): boolean {
  if (isUserGestureActive(gestureUntil, now) && GESTURE_BLOCKED_OWNERS.has(owner)) {
    return false;
  }
  return canTranscriptScrollOwnerWrite(mode, owner);
}

/** Virtualizer height compensation is forbidden while the user is scrolling or selecting. */
export function canVirtualizerAdjustScroll(
  mode: TranscriptScrollMode,
  gestureUntil: number,
  now = Date.now(),
): boolean {
  if (isTranscriptSelectionMode(mode)) return false;
  if (isUserGestureActive(gestureUntil, now)) return false;
  return true;
}
