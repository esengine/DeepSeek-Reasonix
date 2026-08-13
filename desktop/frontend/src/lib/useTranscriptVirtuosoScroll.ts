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

/**
 * Product-level scroll intent around React Virtuoso.
 *
 * Virtuoso exclusively owns measurement and anchor compensation. This hook
 * only records whether the reader follows the tail and exposes explicit
 * navigation commands; it never tries to correct measured row positions.
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
  const [nativeScrollbarDragging, setNativeScrollbarDragging] = useState(false);
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

  const beginBottomRequest = useCallback(() => {
    clearBottomRequest();
    bottomRequestRef.current = true;
    // Virtuoso can briefly report `false` while its LAST-item request is
    // converging. Keep tail intent through that window, then derive the final
    // state from the native scroller so a stale request can never mask later
    // user scrolling.
    bottomRequestTimerRef.current = window.setTimeout(() => {
      bottomRequestTimerRef.current = null;
      bottomRequestRef.current = false;
      const element = scrollRef.current;
      const atBottom = element != null
        && element.scrollHeight - element.scrollTop - element.clientHeight <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX;
      pinnedRef.current = atBottom;
      setIsAtBottom(atBottom);
      if (!isTranscriptSelectionMode(modeRef.current)) {
        publishMode(atBottom ? "tail-follow" : "manual");
      }
    }, 500);
  }, [clearBottomRequest, publishMode]);

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
    return measureTranscriptVirtuosoItem(element, field, nativeScrollbarDragRef.current || nativeScrollbarDragging);
  }, [nativeScrollbarDragging]);

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
    });
  }, []);

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
    // Even inside a bottomRequest window, honor a state change if the reader
    // has genuinely scrolled away from the bottom. Virtuoso fires this callback
    // based on its measured threshold; double-check the native scroll position
    // so non-wheel upward scrolls (native scrollbar drag, middle-button autoscroll)
    // release tail-follow immediately rather than waiting for the timer.
    if (!atBottom && bottomRequestRef.current) {
      if (element) {
        const distanceFromBottom = element.scrollHeight - element.scrollTop - element.clientHeight;
        if (distanceFromBottom > TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX) {
          clearBottomRequest();
          pinnedRef.current = false;
          setIsAtBottom(false);
          if (!isTranscriptSelectionMode(modeRef.current)) publishMode("manual");
        }
      }
      return;
    }
    if (atBottom) {
      clearBottomRequest();
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
  }, [clearBottomRequest, followGrowingTail, publishMode]);

  const reset = useCallback(() => {
    clearBottomRequest();
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
    } else if (owner !== "selection-edge-scroll") {
      pinnedRef.current = false;
      setIsAtBottom(false);
      publishMode("programmatic");
    }
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__?.(owner, top);
    virtuosoRef.current?.scrollTo({ top, behavior });
    return true;
  }, [beginBottomRequest, publishMode]);

  const scrollToBottom = useCallback((behavior: ScrollBehavior = "auto") => {
    if (isTranscriptSelectionMode(modeRef.current)) return;
    beginBottomRequest();
    pinnedRef.current = true;
    setIsAtBottom(true);
    publishMode("tail-follow");
    const handle = virtuosoRef.current;
    handle?.scrollToIndex({ index: "LAST", align: "end", behavior: behavior === "smooth" ? "smooth" : "auto" });
    // `align: end` positions the last item, but theme spacing on the list can
    // leave a few native scroll pixels below it. Finish at the actual scroll
    // extent so at-bottom state and the visual position agree exactly.
    handle?.scrollTo({ top: Number.MAX_SAFE_INTEGER, behavior });
    requestAnimationFrame(() => handle?.autoscrollToBottom());
  }, [beginBottomRequest, publishMode]);

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
    return false;
  }, [releaseTailFollow]);

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
    return false;
  }, [releaseTailFollow]);

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
    return false;
  }, [releaseTailFollow]);

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
