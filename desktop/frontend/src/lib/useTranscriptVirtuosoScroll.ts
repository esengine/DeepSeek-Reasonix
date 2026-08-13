import { useCallback, useEffect, useRef, useState } from "react";
import type {
  KeyboardEvent as ReactKeyboardEvent,
  PointerEvent as ReactPointerEvent,
  TouchEvent as ReactTouchEvent,
  WheelEvent as ReactWheelEvent,
} from "react";
import type { SizeFunction, VirtuosoHandle } from "react-virtuoso";
import { isEditableTarget } from "./keyboardShortcuts";
import { isNativeVerticalScrollbarPointer, measureTranscriptVirtuosoItem } from "./transcriptNativeScrollbar";
import {
  isTranscriptSelectionMode,
  type TranscriptScrollMode,
  type TranscriptScrollOwner,
} from "./transcriptScrollController";

declare global {
  interface Window {
    __REASONIX_TRANSCRIPT_SCROLL_WRITE__?: (owner: TranscriptScrollOwner, top: number) => void;
  }
}

const SCROLL_UP_KEYS = new Set(["ArrowUp", "PageUp", "Home"]);
const SCROLL_DOWN_KEYS = new Set(["ArrowDown", "PageDown", "End", " ", "Spacebar"]);
export const TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX = 4;
// Thumb/middle-button leaves jump farther than a last-row LAST undershoot.
const BOTTOM_REQUEST_LEAVE_PX = 160;

export function isPinnedTranscriptLayoutGrowth({
  pinned,
  previousScrollHeight,
  previousScrollTop,
  scrollHeight,
  scrollTop,
}: {
  pinned: boolean;
  previousScrollHeight: number;
  previousScrollTop: number;
  scrollHeight: number;
  scrollTop: number;
}) {
  return pinned
    && previousScrollHeight > 0
    && scrollHeight > previousScrollHeight + 1
    && scrollTop >= previousScrollTop - 1;
}

export function isPinnedTranscriptViewportChange({
  pinned,
  previousClientHeight,
  clientHeight,
}: {
  pinned: boolean;
  previousClientHeight: number;
  clientHeight: number;
}) {
  // Composer chrome such as the todo shelf changes the transcript viewport
  // without a user scroll. Virtuoso can then publish atBottom=false and even
  // reset scrollTop to the start of the loaded window.
  return pinned
    && previousClientHeight > 0
    && Math.abs(clientHeight - previousClientHeight) > 1;
}

export function shouldKeepPinnedOnAtBottomFalse({
  pinned,
  previousScrollHeight,
  previousScrollTop,
  previousClientHeight,
  scrollHeight,
  scrollTop,
  clientHeight,
}: {
  pinned: boolean;
  previousScrollHeight: number;
  previousScrollTop: number;
  previousClientHeight: number;
  scrollHeight: number;
  scrollTop: number;
  clientHeight: number;
}) {
  return isPinnedTranscriptLayoutGrowth({
    pinned,
    previousScrollHeight,
    previousScrollTop,
    scrollHeight,
    scrollTop,
  }) || isPinnedTranscriptViewportChange({
    pinned,
    previousClientHeight,
    clientHeight,
  });
}

export function nativeTranscriptDistanceFromBottom(element: {
  scrollHeight: number;
  scrollTop: number;
  clientHeight: number;
}) {
  return element.scrollHeight - element.scrollTop - element.clientHeight;
}

export function nativeTranscriptBottomTop(element: {
  scrollHeight: number;
  clientHeight: number;
}) {
  return Math.max(0, element.scrollHeight - element.clientHeight);
}

export function isPhysicallyAtTranscriptBottom(
  element: { scrollHeight: number; scrollTop: number; clientHeight: number },
  threshold = TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX,
) {
  return nativeTranscriptDistanceFromBottom(element) <= threshold;
}

// Virtuoso LAST after a native snap writes a shorter size-tree top. That drop
// is undershoot, not the reader leaving. Pointer/key leave already unpins.
export function shouldReleaseBottomRequestOnAtBottomFalse({
  distanceFromBottom,
  scrollTop,
  previousScrollTop,
  previousScrollHeight,
  previousClientHeight,
  scrollHeight,
  clientHeight,
}: {
  distanceFromBottom: number;
  scrollTop: number;
  previousScrollTop: number;
  previousScrollHeight?: number;
  previousClientHeight?: number;
  scrollHeight: number;
  clientHeight: number;
}) {
  if (distanceFromBottom <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX) return false;
  const previousNativeMax = nativeTranscriptBottomTop({
    scrollHeight: previousScrollHeight ?? scrollHeight,
    clientHeight: previousClientHeight ?? clientHeight,
  });
  if (previousScrollTop >= previousNativeMax - TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX) return false;
  return previousScrollTop - scrollTop > BOTTOM_REQUEST_LEAVE_PX;
}

export function shouldClearBottomRequestOnAtBottomTrue() {
  return false;
}

export function shouldRemeasureMountedRowsForTailFinish({
  remeasuredThisCommand,
  allowRemeasure = true,
}: {
  remeasuredThisCommand: boolean;
  allowRemeasure?: boolean;
}) {
  return allowRemeasure && !remeasuredThisCommand;
}

export function shouldFinishTailOnBottomRequestTimer({
  pinned,
  bottomRequestWasActive,
}: {
  pinned: boolean;
  bottomRequestWasActive: boolean;
}) {
  return pinned && bottomRequestWasActive;
}

export function shouldClearBottomRequestOnWriteOffset(owner: TranscriptScrollOwner) {
  return owner !== "jump-bottom" && owner !== "selection-edge-scroll";
}

export function shouldFinishTailOnAtBottomFalse({
  pinned,
  bottomRequestActive,
}: {
  pinned: boolean;
  bottomRequestActive: boolean;
}) {
  return pinned || bottomRequestActive;
}

export function shouldSnapPinnedWheelToNativeBottom({
  pinned,
  deltaY,
  distanceFromBottom,
}: {
  pinned: boolean;
  deltaY: number;
  distanceFromBottom: number;
}) {
  return pinned && deltaY > 0 && distanceFromBottom > TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX;
}

/**
 * Product-level scroll intent around React Virtuoso.
 *
 * Virtuoso owns row measurement and anchors. This hook records tail-follow
 * vs manual reading and finishes LAST undershoot at scrollHeight-clientHeight.
 */
export function useTranscriptVirtuosoScroll() {
  const virtuosoRef = useRef<VirtuosoHandle>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const pinnedRef = useRef(true);
  const bottomRequestRef = useRef(false);
  const bottomRequestTimerRef = useRef<number | null>(null);
  const pinnedMetricsRef = useRef({ scrollHeight: 0, scrollTop: 0, clientHeight: 0 });
  const modeRef = useRef<TranscriptScrollMode>("tail-follow");
  const touchStartYRef = useRef<number | null>(null);
  const nativeScrollbarDragRef = useRef(false);
  const remeasureForTailRef = useRef(false);
  const [nativeScrollbarDragging, setNativeScrollbarDragging] = useState(false);
  const [measureGeneration, setMeasureGeneration] = useState(0);
  const [isAtBottom, setIsAtBottom] = useState(true);
  const [scrollElement, setScrollElement] = useState<HTMLDivElement | null>(null);

  const publishMode = useCallback((mode: TranscriptScrollMode) => {
    modeRef.current = mode;
    if (scrollRef.current) scrollRef.current.dataset.scrollMode = mode;
  }, []);

  const clearBottomRequest = useCallback(() => {
    bottomRequestRef.current = false;
    if (bottomRequestTimerRef.current != null) {
      window.clearTimeout(bottomRequestTimerRef.current);
      bottomRequestTimerRef.current = null;
    }
  }, []);

  const snapToNativeBottom = useCallback(() => {
    const element = scrollRef.current;
    if (!element || isTranscriptSelectionMode(modeRef.current)) return false;
    const max = nativeTranscriptBottomTop(element);
    virtuosoRef.current?.scrollTo({ top: Number.MAX_SAFE_INTEGER, behavior: "auto" });
    if (element.scrollTop < max) element.scrollTop = max;
    const atBottom = isPhysicallyAtTranscriptBottom(element);
    if (atBottom) {
      pinnedMetricsRef.current = {
        scrollHeight: element.scrollHeight,
        scrollTop: element.scrollTop,
        clientHeight: element.clientHeight,
      };
    }
    return atBottom;
  }, []);

  const finishTailAtNativeBottom = useCallback((opts?: { force?: boolean; allowRemeasure?: boolean }) => {
    if (isTranscriptSelectionMode(modeRef.current)) return;
    if (!opts?.force && !pinnedRef.current && !bottomRequestRef.current) return;

    const applySnap = () => {
      if (isTranscriptSelectionMode(modeRef.current)) return;
      if (!opts?.force && !pinnedRef.current && !bottomRequestRef.current) return;
      const atBottom = snapToNativeBottom();
      if (atBottom) {
        pinnedRef.current = true;
        setIsAtBottom(true);
        publishMode("tail-follow");
        return;
      }
      setIsAtBottom(false);
    };

    if (shouldRemeasureMountedRowsForTailFinish({
      remeasuredThisCommand: remeasureForTailRef.current,
      allowRemeasure: opts?.allowRemeasure,
    })) {
      remeasureForTailRef.current = true;
      setMeasureGeneration((generation) => generation + 1);
      requestAnimationFrame(applySnap);
      return;
    }
    applySnap();
  }, [publishMode, snapToNativeBottom]);

  const beginBottomRequest = useCallback(() => {
    remeasureForTailRef.current = false;
    clearBottomRequest();
    bottomRequestRef.current = true;
    // LAST can overwrite a native snap. Keep the window so a still-pinned
    // reader finishes at the native extent. An explicit leave cancels this.
    bottomRequestTimerRef.current = window.setTimeout(() => {
      const bottomRequestWasActive = bottomRequestRef.current;
      bottomRequestTimerRef.current = null;
      bottomRequestRef.current = false;
      if (!shouldFinishTailOnBottomRequestTimer({
        pinned: pinnedRef.current,
        bottomRequestWasActive,
      })) return;
      finishTailAtNativeBottom();
    }, 500);
  }, [clearBottomRequest, finishTailAtNativeBottom]);

  const setMode = useCallback((mode: TranscriptScrollMode, _reason?: string) => {
    publishMode(mode);
  }, [publishMode]);

  const finishNativeScrollbarDrag = useCallback(() => {
    if (!nativeScrollbarDragRef.current) return;
    nativeScrollbarDragRef.current = false;
    const element = scrollRef.current;
    if (element) delete element.dataset.nativeScrollbarDrag;
    // Changing itemSize re-attaches Virtuoso's ResizeObserver, so rows first
    // visited during the drag are measured once after the thumb is released.
    setNativeScrollbarDragging(false);
  }, []);

  useEffect(() => {
    window.addEventListener("pointerup", finishNativeScrollbarDrag, true);
    window.addEventListener("pointercancel", finishNativeScrollbarDrag, true);
    window.addEventListener("blur", finishNativeScrollbarDrag);
    return () => {
      window.removeEventListener("pointerup", finishNativeScrollbarDrag, true);
      window.removeEventListener("pointercancel", finishNativeScrollbarDrag, true);
      window.removeEventListener("blur", finishNativeScrollbarDrag);
    };
  }, [finishNativeScrollbarDrag]);

  const itemSize = useCallback<SizeFunction>((element, field) => {
    // The drag state intentionally changes this callback identity on release.
    // Virtuoso then re-observes and records the real mounted row sizes.
    // measureGeneration does the same after a tail command undershoots.
    return measureTranscriptVirtuosoItem(element, field, nativeScrollbarDragRef.current || nativeScrollbarDragging);
  }, [measureGeneration, nativeScrollbarDragging]);

  const scrollerRef = useCallback((node: HTMLElement | Window | null) => {
    const element = node instanceof HTMLElement ? node as HTMLDivElement : null;
    if (scrollRef.current !== element) finishNativeScrollbarDrag();
    scrollRef.current = element;
    if (element) {
      element.dataset.scrollMode = modeRef.current;
      if (pinnedRef.current) {
        pinnedMetricsRef.current = {
          scrollHeight: element.scrollHeight,
          scrollTop: element.scrollTop,
          clientHeight: element.clientHeight,
        };
      }
    }
    setScrollElement((current) => current === element ? current : element);
  }, [finishNativeScrollbarDrag]);

  const releaseTailFollow = useCallback(() => {
    if (isTranscriptSelectionMode(modeRef.current)) return;
    clearBottomRequest();
    pinnedRef.current = false;
    setIsAtBottom(false);
    publishMode("manual");
  }, [clearBottomRequest, publishMode]);

  const followGrowingTail = useCallback(() => {
    if (!pinnedRef.current || isTranscriptSelectionMode(modeRef.current)) return;
    const handle = virtuosoRef.current;
    handle?.autoscrollToBottom();
    requestAnimationFrame(() => {
      if (!pinnedRef.current || isTranscriptSelectionMode(modeRef.current)) return;
      handle?.scrollTo({ top: Number.MAX_SAFE_INTEGER, behavior: "auto" });
      finishTailAtNativeBottom({ allowRemeasure: false });
    });
  }, [finishTailAtNativeBottom]);

  const atBottomStateChange = useCallback((atBottom: boolean) => {
    const element = scrollRef.current;
    if (!atBottom && element && shouldKeepPinnedOnAtBottomFalse({
      pinned: pinnedRef.current,
      previousScrollHeight: pinnedMetricsRef.current.scrollHeight,
      previousScrollTop: pinnedMetricsRef.current.scrollTop,
      previousClientHeight: pinnedMetricsRef.current.clientHeight,
      scrollHeight: element.scrollHeight,
      scrollTop: element.scrollTop,
      clientHeight: element.clientHeight,
    })) {
      // A mounted row grew, or composer chrome resized the viewport, while the
      // reader still owned the tail. Virtuoso can publish `false` and even jump
      // scrollTop to the loaded-window start before we follow the new extent.
      pinnedMetricsRef.current = {
        scrollHeight: element.scrollHeight,
        scrollTop: element.scrollTop,
        clientHeight: element.clientHeight,
      };
      followGrowingTail();
      return;
    }
    if (!atBottom && shouldFinishTailOnAtBottomFalse({
      pinned: pinnedRef.current,
      bottomRequestActive: bottomRequestRef.current,
    })) {
      if (element && shouldReleaseBottomRequestOnAtBottomFalse({
        distanceFromBottom: nativeTranscriptDistanceFromBottom(element),
        scrollTop: element.scrollTop,
        previousScrollTop: pinnedMetricsRef.current.scrollTop,
        previousScrollHeight: pinnedMetricsRef.current.scrollHeight,
        previousClientHeight: pinnedMetricsRef.current.clientHeight,
        scrollHeight: element.scrollHeight,
        clientHeight: element.clientHeight,
      })) {
        clearBottomRequest();
        pinnedRef.current = false;
        setIsAtBottom(false);
        if (!isTranscriptSelectionMode(modeRef.current)) publishMode("manual");
        return;
      }
      finishTailAtNativeBottom();
      return;
    }
    if (atBottom) {
      if (shouldClearBottomRequestOnAtBottomTrue()) clearBottomRequest();
      if (element) {
        pinnedMetricsRef.current = {
          scrollHeight: element.scrollHeight,
          scrollTop: element.scrollTop,
          clientHeight: element.clientHeight,
        };
      }
    }
    pinnedRef.current = atBottom;
    setIsAtBottom(atBottom);
    if (!isTranscriptSelectionMode(modeRef.current)) {
      publishMode(atBottom ? "tail-follow" : "manual");
    }
  }, [clearBottomRequest, finishTailAtNativeBottom, followGrowingTail, publishMode]);

  const reset = useCallback(() => {
    clearBottomRequest();
    remeasureForTailRef.current = false;
    pinnedRef.current = true;
    setIsAtBottom(true);
    publishMode("tail-follow");
  }, [clearBottomRequest, publishMode]);

  const writeOffset = useCallback((owner: TranscriptScrollOwner, top: number, behavior: ScrollBehavior = "auto") => {
    if (isTranscriptSelectionMode(modeRef.current) && owner !== "selection-edge-scroll") return false;
    const element = scrollRef.current;
    if (!element) return false;
    if (owner === "jump-bottom") {
      beginBottomRequest();
      pinnedRef.current = true;
      setIsAtBottom(true);
      publishMode("tail-follow");
    } else if (shouldClearBottomRequestOnWriteOffset(owner)) {
      clearBottomRequest();
      pinnedRef.current = false;
      setIsAtBottom(false);
      publishMode("programmatic");
    }
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__?.(owner, top);
    virtuosoRef.current?.scrollTo({ top, behavior });
    return true;
  }, [beginBottomRequest, clearBottomRequest, publishMode]);

  const scrollToBottom = useCallback((behavior: ScrollBehavior = "auto") => {
    if (isTranscriptSelectionMode(modeRef.current)) return;
    beginBottomRequest();
    pinnedRef.current = true;
    setIsAtBottom(true);
    publishMode("tail-follow");
    const handle = virtuosoRef.current;
    handle?.scrollToIndex({ index: "LAST", align: "end", behavior: behavior === "smooth" ? "smooth" : "auto" });
    // LAST mounts the last rows. Remeasure those sizes, then finish at the
    // native extent. Do not autoscrollToBottom — that LAST listener overwrites
    // the native snap.
    requestAnimationFrame(() => finishTailAtNativeBottom());
  }, [beginBottomRequest, finishTailAtNativeBottom, publishMode]);

  const scrollToDataIndex = useCallback((firstItemIndex: number, dataIndex: number, behavior: "auto" | "smooth" = "auto") => {
    if (isTranscriptSelectionMode(modeRef.current)) return;
    clearBottomRequest();
    pinnedRef.current = false;
    setIsAtBottom(false);
    publishMode("programmatic");
    virtuosoRef.current?.scrollToIndex({ index: firstItemIndex + dataIndex, align: "start", behavior });
  }, [clearBottomRequest, publishMode]);

  const finishProgrammaticScroll = useCallback(() => {
    if (isTranscriptSelectionMode(modeRef.current)) return;
    publishMode(pinnedRef.current ? "tail-follow" : "manual");
  }, [publishMode]);

  const onWheelIntent = useCallback((event: ReactWheelEvent<HTMLElement>) => {
    if (event.ctrlKey || event.deltaY === 0 || Math.abs(event.deltaX) > Math.abs(event.deltaY)) return false;
    if (event.deltaY < 0 || !pinnedRef.current) {
      releaseTailFollow();
      return true;
    }
    const element = scrollRef.current;
    if (element && shouldSnapPinnedWheelToNativeBottom({
      pinned: pinnedRef.current,
      deltaY: event.deltaY,
      distanceFromBottom: nativeTranscriptDistanceFromBottom(element),
    })) {
      finishTailAtNativeBottom();
      return true;
    }
    return false;
  }, [finishTailAtNativeBottom, releaseTailFollow]);

  const onTouchStartIntent = useCallback((event: ReactTouchEvent<HTMLElement>) => {
    touchStartYRef.current = event.touches[0]?.clientY ?? null;
  }, []);

  const onTouchMoveIntent = useCallback((event: ReactTouchEvent<HTMLElement>) => {
    const start = touchStartYRef.current;
    const current = event.touches[0]?.clientY;
    if (start == null || current == null || Math.abs(current - start) < 2) return false;
    if (current > start || !pinnedRef.current) {
      releaseTailFollow();
      return true;
    }
    return false;
  }, [releaseTailFollow]);

  const onKeyScrollIntent = useCallback((event: ReactKeyboardEvent<HTMLElement>) => {
    if (isEditableTarget(event.target)) return false;
    if (SCROLL_UP_KEYS.has(event.key) || (SCROLL_DOWN_KEYS.has(event.key) && !pinnedRef.current)) {
      releaseTailFollow();
      return true;
    }
    const element = scrollRef.current;
    if (SCROLL_DOWN_KEYS.has(event.key) && element && shouldSnapPinnedWheelToNativeBottom({
      pinned: pinnedRef.current,
      deltaY: 1,
      distanceFromBottom: nativeTranscriptDistanceFromBottom(element),
    })) {
      finishTailAtNativeBottom();
      return true;
    }
    return false;
  }, [finishTailAtNativeBottom, releaseTailFollow]);

  const onPointerDownIntent = useCallback((event: ReactPointerEvent<HTMLElement>) => {
    const element = scrollRef.current;
    if (element && isNativeVerticalScrollbarPointer(element, event.nativeEvent)) {
      if (!nativeScrollbarDragRef.current) {
        nativeScrollbarDragRef.current = true;
        element.dataset.nativeScrollbarDrag = "true";
        setNativeScrollbarDragging(true);
      }
      releaseTailFollow();
      return true;
    }
    if (event.button !== 1) return false;
    releaseTailFollow();
    return true;
  }, [releaseTailFollow]);

  const onNestedScrollIntent = useCallback((deltaY: number) => {
    if (deltaY < 0 || !pinnedRef.current) {
      releaseTailFollow();
      return true;
    }
    const element = scrollRef.current;
    if (element && shouldSnapPinnedWheelToNativeBottom({
      pinned: pinnedRef.current,
      deltaY,
      distanceFromBottom: nativeTranscriptDistanceFromBottom(element),
    })) {
      finishTailAtNativeBottom();
      return true;
    }
    return false;
  }, [finishTailAtNativeBottom, releaseTailFollow]);

  return {
    virtuosoRef,
    scrollRef,
    scrollElement,
    itemSize,
    nativeScrollbarDragging,
    pinnedRef,
    isAtBottom,
    modeRef,
    scrollerRef,
    setMode,
    reset,
    writeOffset,
    scrollToBottom,
    followGrowingTail,
    scrollToDataIndex,
    finishProgrammaticScroll,
    releaseTailFollow,
    atBottomStateChange,
    onWheelIntent,
    onTouchStartIntent,
    onTouchMoveIntent,
    onKeyScrollIntent,
    onPointerDownIntent,
    onNestedScrollIntent,
  };
}
