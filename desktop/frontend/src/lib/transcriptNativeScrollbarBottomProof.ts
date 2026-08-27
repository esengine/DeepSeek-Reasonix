import {
  nativeTranscriptDistanceFromBottom,
  TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX,
} from "./transcriptScrollGeometry";

type MutableRef<T> = { current: T };

export type TranscriptNativeScrollbarBottomProof = {
  begin: (element: HTMLDivElement, pointerY?: number) => void;
  observe: (element: HTMLDivElement, pointerY?: number) => void;
  finish: (element: HTMLDivElement | null) => readonly [moved: boolean, reachedBottom: boolean];
  cancel: () => void;
};

/**
 * Retains physical-bottom evidence for one native thumb transaction.
 *
 * Chromium/WebKit can update the native scroller before React delivers the
 * corresponding scroll event. A passive animation-frame sampler closes that
 * release race without writing scrollTop or changing arbiter ownership.
 */
export function createTranscriptNativeScrollbarBottomProof({
  scrollRef,
}: {
  scrollRef: MutableRef<HTMLDivElement | null>;
}): TranscriptNativeScrollbarBottomProof {
  let activeElement: HTMLDivElement | null = null;
  let initialTop = 0;
  let initialPointerY: number | null = null;
  let frame: number | null = null;
  let pollTimer: number | null = null;
  let pollView: Window | null = null;
  let moved = false;
  let reachedBottom = false;

  const cancelFrame = () => {
    if (frame !== null) cancelAnimationFrame(frame);
    frame = null;
    if (pollTimer !== null) pollView?.clearInterval(pollTimer);
    pollTimer = null;
    pollView = null;
  };

  const observe = (element: HTMLDivElement, pointerY?: number) => {
    if (activeElement !== element) return;
    // A native thumb can leave and return to the bottom between both rAF and
    // scroll delivery. Pointer movement proves the transaction moved; finish
    // still requires the real scroller to be physically at bottom.
    moved ||= Math.abs(element.scrollTop - initialTop) > 1
      || (pointerY !== undefined && initialPointerY !== null && Math.abs(pointerY - initialPointerY) > 2);
    reachedBottom ||= moved && nativeTranscriptDistanceFromBottom(element) <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX;
  };

  const sample = () => {
    frame = null;
    const element = activeElement;
    if (!element || scrollRef.current !== element) return;
    observe(element);
    frame = requestAnimationFrame(sample);
  };

  const cancel = () => {
    cancelFrame();
    activeElement = null;
    initialPointerY = null;
    moved = false;
    reachedBottom = false;
  };

  const begin = (element: HTMLDivElement, pointerY?: number) => {
    cancel();
    activeElement = element;
    initialTop = element.scrollTop;
    initialPointerY = pointerY ?? null;
    frame = requestAnimationFrame(sample);
    // Native GTK/Chromium scrollbar tracking can suspend rAF and consume DOM
    // pointermove events while the thumb owns the compositor. A passive task
    // sampler retains only the fact that the real scrollTop moved; it neither
    // writes geometry nor extends ownership, and is cancelled on every
    // terminal path. This keeps a stationary bottom click distinguishable
    // from an away-and-back drag whose intermediate events were coalesced.
    pollView = element.ownerDocument?.defaultView ?? null;
    if (pollView) pollTimer = pollView.setInterval(() => observe(element), 8);
  };

  const finish = (element: HTMLDivElement | null) => {
    const matchesActiveElement = Boolean(element && activeElement === element && scrollRef.current === element);
    const movedBeforeRelease = Boolean(matchesActiveElement && element && (moved || Math.abs(element.scrollTop - initialTop) > 1));
    const release = [
      movedBeforeRelease,
      Boolean(
        element
        && movedBeforeRelease
        && (reachedBottom || nativeTranscriptDistanceFromBottom(element) <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX)
      ),
    ] as const;
    cancel();
    return release;
  };

  return { begin, observe, finish, cancel };
}
