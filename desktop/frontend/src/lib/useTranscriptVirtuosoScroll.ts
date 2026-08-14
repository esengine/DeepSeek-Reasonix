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
  INITIAL_TRANSCRIPT_SCROLL_STATE,
  isTranscriptSelectionMode,
  reduceTranscriptScroll,
  type TranscriptScrollCommand,
  type TranscriptScrollEvent,
  type TranscriptScrollMode,
  type TranscriptScrollOwner,
  type TranscriptScrollState,
} from "./transcriptScrollController";

declare global {
  interface Window {
    __REASONIX_TRANSCRIPT_SCROLL_WRITE__?: (owner: TranscriptScrollOwner, top: number) => void;
  }
}

const SCROLL_UP_KEYS = new Set(["ArrowUp", "PageUp", "Home"]);
const SCROLL_DOWN_KEYS = new Set(["ArrowDown", "PageDown", "End", " ", "Spacebar"]);
export const TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX = 4;
// LAST/end can stop just above the native extent when the scroller owns
// vertical padding or fractional row measurements. A bounded positive offset
// is clamped by the browser and keeps the write within Virtuoso's API.
const TRANSCRIPT_TAIL_CLAMP_OFFSET_PX = 64;

export function nativeTranscriptDistanceFromBottom(element: {
  scrollHeight: number;
  scrollTop: number;
  clientHeight: number;
}) {
  return element.scrollHeight - element.scrollTop - element.clientHeight;
}

export function nativeTranscriptBottomTop(element: { scrollHeight: number; clientHeight: number }) {
  return Math.max(0, element.scrollHeight - element.clientHeight);
}

export function hasTranscriptScrollableRange(
  element: { scrollHeight: number; clientHeight: number },
  threshold = TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX,
) {
  return nativeTranscriptBottomTop(element) > threshold;
}

/** One scroll coordinator around React Virtuoso. No native scrollTop writes. */
export function useTranscriptVirtuosoScroll() {
  const virtuosoRef = useRef<VirtuosoHandle>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const stateRef = useRef<TranscriptScrollState>(INITIAL_TRANSCRIPT_SCROLL_STATE);
  const pinnedRef = useRef(true);
  const modeRef = useRef<TranscriptScrollMode>("tail-follow");
  const touchStartYRef = useRef<number | null>(null);
  const nativeScrollbarDragRef = useRef(false);
  const followFrameRef = useRef<number | null>(null);
  const tailSettleFrameRef = useRef<number | null>(null);
  const resizeSettleFrameRef = useRef<number | null>(null);
  const [nativeScrollbarDragging, setNativeScrollbarDragging] = useState(false);
  const [isAtBottom, setIsAtBottom] = useState(true);
  const [scrollElement, setScrollElement] = useState<HTMLDivElement | null>(null);

  const scheduleTailSettle = useCallback(() => {
    if (tailSettleFrameRef.current !== null) cancelAnimationFrame(tailSettleFrameRef.current);
    tailSettleFrameRef.current = requestAnimationFrame(() => {
      tailSettleFrameRef.current = null;
      if (modeRef.current !== "tail-follow") return;
      virtuosoRef.current?.scrollToIndex({
        index: "LAST",
        align: "end",
        offset: TRANSCRIPT_TAIL_CLAMP_OFFSET_PX,
        behavior: "auto",
      });
    });
  }, []);

  const publishState = useCallback((state: TranscriptScrollState) => {
    stateRef.current = state;
    modeRef.current = state.mode;
    pinnedRef.current = state.mode === "tail-follow";
    setIsAtBottom(state.atBottom);
    if (scrollRef.current) scrollRef.current.dataset.scrollMode = state.mode;
  }, []);

  const runCommand = useCallback((command: TranscriptScrollCommand) => {
    const handle = virtuosoRef.current;
    switch (command.type) {
      case "AUTOSCROLL_TO_BOTTOM":
        handle?.autoscrollToBottom();
        scheduleTailSettle();
        return;
      case "SCROLL_TO_LAST":
        handle?.scrollToIndex({
          index: "LAST",
          align: "end",
          offset: TRANSCRIPT_TAIL_CLAMP_OFFSET_PX,
          behavior: command.behavior,
        });
        // The first LAST request can use Virtuoso's pre-measurement size tree.
        // Retry once on the next frame, after the mounted tail row has been
        // measured. This stays inside Virtuoso and is cancelled by explicit
        // user ownership changes below.
        scheduleTailSettle();
        return;
      case "SCROLL_TO_INDEX":
        handle?.scrollToIndex({ index: command.index, align: "start", behavior: command.behavior });
        return;
      case "SCROLL_TO_OFFSET":
        window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__?.(command.owner, command.top);
        handle?.scrollTo({ top: command.top, behavior: command.behavior });
    }
  }, [scheduleTailSettle]);

  const dispatch = useCallback((event: TranscriptScrollEvent) => {
    if (
      event.type === "USER_SCROLL_INTENT"
      || event.type === "USER_RESIZE_BEGIN"
      || event.type === "SELECTION_BEGIN"
      || event.type === "PROGRAMMATIC_BEGIN"
      || event.type === "JUMP_TO_INDEX"
      || event.type === "SCROLL_TO_OFFSET"
    ) {
      if (tailSettleFrameRef.current !== null) cancelAnimationFrame(tailSettleFrameRef.current);
      tailSettleFrameRef.current = null;
    }
    const result = reduceTranscriptScroll(stateRef.current, event);
    publishState(result.state);
    for (const command of result.commands) runCommand(command);
    return result;
  }, [publishState, runCommand]);

  const setMode = useCallback((mode: TranscriptScrollMode, _reason?: string) => {
    switch (mode) {
      case "tail-follow": dispatch({ type: "RESET" }); break;
      case "manual": dispatch({ type: "USER_SCROLL_INTENT" }); break;
      case "user-resize": dispatch({ type: "USER_RESIZE_BEGIN" }); break;
      case "selection": dispatch({ type: "SELECTION_BEGIN" }); break;
      case "restoring": dispatch({ type: "PROGRAMMATIC_BEGIN" }); break;
    }
  }, [dispatch]);

  const finishNativeScrollbarDrag = useCallback(() => {
    if (!nativeScrollbarDragRef.current) return;
    nativeScrollbarDragRef.current = false;
    const element = scrollRef.current;
    if (element) delete element.dataset.nativeScrollbarDrag;
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

  useEffect(() => () => {
    if (followFrameRef.current !== null) cancelAnimationFrame(followFrameRef.current);
    if (tailSettleFrameRef.current !== null) cancelAnimationFrame(tailSettleFrameRef.current);
    if (resizeSettleFrameRef.current !== null) cancelAnimationFrame(resizeSettleFrameRef.current);
  }, []);

  const itemSize = useCallback<SizeFunction>((element, field) => {
    return measureTranscriptVirtuosoItem(element, field, nativeScrollbarDragRef.current || nativeScrollbarDragging);
  }, [nativeScrollbarDragging]);

  const scrollerRef = useCallback((node: HTMLElement | Window | null) => {
    const element = node instanceof HTMLElement ? node as HTMLDivElement : null;
    if (scrollRef.current !== element) finishNativeScrollbarDrag();
    scrollRef.current = element;
    if (element) {
      element.dataset.scrollMode = stateRef.current.mode;
      dispatch({
        type: "AT_BOTTOM_CHANGED",
        atBottom: nativeTranscriptDistanceFromBottom(element) <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX,
        scrollable: hasTranscriptScrollableRange(element),
      });
    }
    setScrollElement((current) => current === element ? current : element);
  }, [dispatch, finishNativeScrollbarDrag]);

  const releaseTailFollow = useCallback(() => {
    if (isTranscriptSelectionMode(modeRef.current)) return;
    const element = scrollRef.current;
    if (element && !stateRef.current.scrollable && hasTranscriptScrollableRange(element)) {
      dispatch({
        type: "AT_BOTTOM_CHANGED",
        atBottom: nativeTranscriptDistanceFromBottom(element) <= TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX,
        scrollable: true,
      });
    }
    dispatch({ type: "USER_SCROLL_INTENT" });
  }, [dispatch]);

  const followGrowingTail = useCallback(() => {
    if (followFrameRef.current !== null) return;
    followFrameRef.current = requestAnimationFrame(() => {
      followFrameRef.current = null;
      dispatch({ type: "LAYOUT_HEIGHT_CHANGED" });
    });
  }, [dispatch]);

  const beginUserResize = useCallback(() => {
    dispatch({ type: "USER_RESIZE_BEGIN" });
    if (resizeSettleFrameRef.current !== null) cancelAnimationFrame(resizeSettleFrameRef.current);
    resizeSettleFrameRef.current = requestAnimationFrame(() => {
      resizeSettleFrameRef.current = requestAnimationFrame(() => {
        resizeSettleFrameRef.current = null;
        dispatch({ type: "USER_RESIZE_END" });
      });
    });
  }, [dispatch]);

  const atBottomStateChange = useCallback((atBottom: boolean) => {
    const element = scrollRef.current;
    dispatch({
      type: "AT_BOTTOM_CHANGED",
      atBottom,
      scrollable: element ? hasTranscriptScrollableRange(element) : stateRef.current.scrollable,
    });
  }, [dispatch]);

  const reset = useCallback(() => dispatch({ type: "RESET" }), [dispatch]);

  const writeOffset = useCallback((owner: TranscriptScrollOwner, top: number, behavior: ScrollBehavior = "auto") => {
    if (isTranscriptSelectionMode(modeRef.current) && owner !== "selection-edge-scroll") return false;
    if (!scrollRef.current) return false;
    dispatch({ type: "SCROLL_TO_OFFSET", owner, top, behavior });
    return true;
  }, [dispatch]);

  const scrollToBottom = useCallback((behavior: ScrollBehavior = "auto") => {
    if (isTranscriptSelectionMode(modeRef.current)) return;
    dispatch({ type: "JUMP_TO_BOTTOM", behavior });
  }, [dispatch]);

  const scrollToDataIndex = useCallback((firstItemIndex: number, dataIndex: number, behavior: "auto" | "smooth" = "auto") => {
    if (isTranscriptSelectionMode(modeRef.current)) return;
    dispatch({ type: "JUMP_TO_INDEX", index: firstItemIndex + dataIndex, behavior });
  }, [dispatch]);

  const finishProgrammaticScroll = useCallback(() => dispatch({ type: "PROGRAMMATIC_END" }), [dispatch]);

  const restoreTailIfNotScrollable = useCallback(() => {
    const element = scrollRef.current;
    if (!element || hasTranscriptScrollableRange(element)) return false;
    dispatch({ type: "AT_BOTTOM_CHANGED", atBottom: true, scrollable: false });
    return true;
  }, [dispatch]);

  const onWheelIntent = useCallback((event: ReactWheelEvent<HTMLElement>) => {
    if (event.ctrlKey || event.deltaY === 0 || Math.abs(event.deltaX) > Math.abs(event.deltaY)) return false;
    if (restoreTailIfNotScrollable()) return false;
    if (event.deltaY < 0 || !pinnedRef.current) {
      releaseTailFollow();
      return true;
    }
    return false;
  }, [releaseTailFollow, restoreTailIfNotScrollable]);

  const onTouchStartIntent = useCallback((event: ReactTouchEvent<HTMLElement>) => {
    touchStartYRef.current = event.touches[0]?.clientY ?? null;
  }, []);

  const onTouchMoveIntent = useCallback((event: ReactTouchEvent<HTMLElement>) => {
    const start = touchStartYRef.current;
    const current = event.touches[0]?.clientY;
    if (start == null || current == null || Math.abs(current - start) < 2) return false;
    if (restoreTailIfNotScrollable()) return false;
    if (current > start || !pinnedRef.current) {
      releaseTailFollow();
      return true;
    }
    return false;
  }, [releaseTailFollow, restoreTailIfNotScrollable]);

  const onKeyScrollIntent = useCallback((event: ReactKeyboardEvent<HTMLElement>) => {
    if (isEditableTarget(event.target)) return false;
    if (!SCROLL_UP_KEYS.has(event.key) && !SCROLL_DOWN_KEYS.has(event.key)) return false;
    if (restoreTailIfNotScrollable()) return false;
    if (SCROLL_UP_KEYS.has(event.key) || !pinnedRef.current) {
      releaseTailFollow();
      return true;
    }
    return false;
  }, [releaseTailFollow, restoreTailIfNotScrollable]);

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
    if (event.button !== 1 || restoreTailIfNotScrollable()) return false;
    releaseTailFollow();
    return true;
  }, [releaseTailFollow, restoreTailIfNotScrollable]);

  const onNestedScrollIntent = useCallback((deltaY: number) => {
    if (deltaY === 0 || restoreTailIfNotScrollable()) return false;
    if (deltaY < 0 || !pinnedRef.current) {
      releaseTailFollow();
      return true;
    }
    return false;
  }, [releaseTailFollow, restoreTailIfNotScrollable]);

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
    beginUserResize,
    atBottomStateChange,
    onWheelIntent,
    onTouchStartIntent,
    onTouchMoveIntent,
    onKeyScrollIntent,
    onPointerDownIntent,
    onNestedScrollIntent,
  };
}
