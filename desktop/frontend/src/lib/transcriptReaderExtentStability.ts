import type { TranscriptLayoutAnchor } from "./transcriptVirtuosoRecovery";
import type { TranscriptScrollEvent } from "./transcriptScrollArbiter";

// A downward gesture this close to the physical bottom has no meaningful
// reader extent to recover from. Export the policy threshold so the scroll
// arbiter can keep the extent guard out of that wheel path.
export const MIN_REVERSE_JUMP_PX = 96;
export const TRANSCRIPT_READER_IDLE_MS = 180;
export const TRANSCRIPT_READER_SETTLE_MS = 1_000;

export function transcriptReaderIdleDeadlineReached(startedAt: number, now: number): boolean {
  return now - startedAt >= TRANSCRIPT_READER_IDLE_MS;
}

export function transcriptReaderDirection(deltaY: number): -1 | 1 | undefined {
  if (!Number.isFinite(deltaY) || deltaY === 0) return undefined;
  return deltaY < 0 ? -1 : 1;
}

export function transcriptReaderTransactionCanReuse(direction: -1 | 1, deltaY: number): boolean {
  return transcriptReaderDirection(deltaY) === direction;
}
const REVERSE_JUMP_VIEWPORT_RATIO = 0.5;
const EXTENT_REBOUND_VIEWPORT_RATIO = 0.5;

export type TranscriptExtentSnapshot = {
  scrollTop: number;
  scrollHeight: number;
  clientHeight: number;
};

export type TranscriptReaderExtentGuard = {
  direction: -1 | 1;
  baselineTop: number;
  baselineHeight: number;
  minimumHeight: number;
  clientHeight: number;
  expectedTop: number;
  anchor?: Extract<TranscriptLayoutAnchor, { mode: "manual" }>;
  targetAnchorOffset?: number;
};

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

export function createTranscriptReaderExtentGuard(
  snapshot: TranscriptExtentSnapshot,
  anchor: TranscriptLayoutAnchor | undefined,
  deltaY: number,
): TranscriptReaderExtentGuard | undefined {
  if (!Number.isFinite(deltaY) || deltaY === 0 || snapshot.clientHeight <= 0) return undefined;
  const maxTop = Math.max(0, snapshot.scrollHeight - snapshot.clientHeight);
  const manualAnchor = anchor?.mode === "manual" ? anchor : undefined;
  return {
    direction: deltaY < 0 ? -1 : 1,
    baselineTop: snapshot.scrollTop,
    baselineHeight: snapshot.scrollHeight,
    minimumHeight: snapshot.scrollHeight,
    clientHeight: snapshot.clientHeight,
    expectedTop: clamp(snapshot.scrollTop + deltaY, 0, maxTop),
    anchor: manualAnchor,
    targetAnchorOffset: manualAnchor ? manualAnchor.offset - deltaY : undefined,
  };
}

export function observeTranscriptReaderExtent(
  guard: TranscriptReaderExtentGuard,
  snapshot: TranscriptExtentSnapshot,
): void {
  guard.minimumHeight = Math.min(guard.minimumHeight, snapshot.scrollHeight);
}

export function transcriptReaderExtentHasCollapsed(guard: TranscriptReaderExtentGuard): boolean {
  const collapseThreshold = Math.max(MIN_REVERSE_JUMP_PX, guard.clientHeight * REVERSE_JUMP_VIEWPORT_RATIO);
  return guard.baselineHeight - guard.minimumHeight >= collapseThreshold;
}

export function transcriptReaderExtentCanCorrect(
  guard: TranscriptReaderExtentGuard,
  snapshot: TranscriptExtentSnapshot,
): boolean {
  if (Math.abs(snapshot.clientHeight - guard.clientHeight) > 1) return false;
  const collapseThreshold = Math.max(MIN_REVERSE_JUMP_PX, guard.clientHeight * REVERSE_JUMP_VIEWPORT_RATIO);
  if (!transcriptReaderExtentHasCollapsed(guard)) return false;
  const reverseDisplacement = guard.direction > 0
    ? guard.baselineTop - snapshot.scrollTop
    : snapshot.scrollTop - guard.baselineTop;
  const reverseThreshold = collapseThreshold;
  if (reverseDisplacement < reverseThreshold) return false;
  const reboundTolerance = Math.max(8, guard.clientHeight * EXTENT_REBOUND_VIEWPORT_RATIO);
  return snapshot.scrollHeight >= guard.baselineHeight - reboundTolerance;
}

export function resolveTranscriptReaderExtentCorrection(
  guard: TranscriptReaderExtentGuard,
  snapshot: TranscriptExtentSnapshot,
  currentAnchorOffset?: number,
): number | undefined {
  if (!transcriptReaderExtentCanCorrect(guard, snapshot)) return undefined;
  const maxTop = Math.max(0, snapshot.scrollHeight - snapshot.clientHeight);
  const anchorTarget = guard.anchor
    && guard.targetAnchorOffset !== undefined
    && currentAnchorOffset !== undefined
    && Number.isFinite(currentAnchorOffset)
    ? snapshot.scrollTop + currentAnchorOffset - guard.targetAnchorOffset
    : guard.expectedTop;
  const targetTop = clamp(anchorTarget, 0, maxTop);
  const correction = targetTop - snapshot.scrollTop;
  return guard.direction * correction > 1 ? correction : undefined;
}

export function transcriptKeyboardScrollDelta(
  key: string,
  shiftKey: boolean,
  snapshot: TranscriptExtentSnapshot,
): number | undefined {
  const page = Math.max(1, snapshot.clientHeight * 0.9);
  switch (key) {
    case "ArrowUp": return -40;
    case "ArrowDown": return 40;
    case "PageUp": return -page;
    case "PageDown": return page;
    case "Home": return -snapshot.scrollTop;
    case "End": return Math.max(0, snapshot.scrollHeight - snapshot.clientHeight - snapshot.scrollTop);
    case " ":
    case "Spacebar":
      return shiftKey ? -page : page;
    default:
      return undefined;
  }
}

export function transcriptScrollEventCancelsReaderExtentGuard(type: TranscriptScrollEvent["type"]): boolean {
  return type === "RESET"
    || type === "MANUAL_READING"
    || type === "NATIVE_SCROLLBAR_BEGIN"
    || type === "VIEWPORT_RESIZED"
    || type === "USER_RESIZE_BEGIN"
    || type === "SELECTION_BEGIN"
    || type === "PROGRAMMATIC_BEGIN"
    || type === "JUMP_TO_BOTTOM"
    || type === "JUMP_TO_INDEX"
    || type === "SCROLL_TO_OFFSET"
    || type === "RECOVERY_BEGIN";
}
