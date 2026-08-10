import { useCallback, useEffect, useRef, useState } from "react";
import type { KeyboardEvent as ReactKeyboardEvent, TouchEvent as ReactTouchEvent, WheelEvent as ReactWheelEvent } from "react";
import { DUR_FAST, prefersReducedMotion } from "./motion";
import { isEditableTarget } from "./keyboardShortcuts";
import {
  canTranscriptScrollOwnerWrite,
  isTranscriptSelectionMode,
  type TranscriptScrollMode,
  type TranscriptScrollOwner,
  type TranscriptViewportAnchor,
} from "./transcriptScrollController";

const BOTTOM_THRESHOLD_PX = 80;
const TOUCH_SCROLL_THRESHOLD_PX = 2;
const SCROLL_BREAK_KEYS = new Set([
  "ArrowUp",
  "PageUp",
  "Home",
]);
const CONDITIONAL_SCROLL_KEYS = new Set([
  "ArrowDown",
  "PageDown",
  "End",
  " ",
  "Spacebar",
]);

function isNearBottom(el: HTMLElement): boolean {
  return el.scrollHeight - el.scrollTop - el.clientHeight < BOTTOM_THRESHOLD_PX;
}

function isScrollable(el: HTMLElement): boolean {
  return el.scrollHeight - el.clientHeight > 1;
}

/**
 * useScrollManager — frame-batched auto-scroll for the transcript container.
 *
 * - Auto-pins to the bottom when content is near the edge.
 * - Smooth scroll for jump-to-question navigation.
 * - Batches ResizeObserver callbacks into a single animation frame.
 */
export function useScrollManager() {
  const scrollRef = useRef<HTMLDivElement>(null);
  const stick = useRef(true);
  const prevQuestionsLen = useRef(0);
  const resizeFrame = useRef<number | null>(null);
  const repinFrame = useRef<number | null>(null);
  const smoothScrollTimer = useRef<number | null>(null);
  const pendingRepinHeightDelta = useRef(0);
  const layoutScrollFrames = useRef<number[]>([]);
  const touchStartY = useRef<number | null>(null);
  const lastClientHeight = useRef<number | null>(null);
  const lastFooterHeight = useRef<number | null>(null);
  const [isAtBottom, setIsAtBottom] = useState(true);
  const modeRef = useRef<TranscriptScrollMode>("tail-follow");
  const generationRef = useRef(0);

  useEffect(() => {
    return () => {
      if (resizeFrame.current !== null) cancelAnimationFrame(resizeFrame.current);
      if (repinFrame.current !== null) cancelAnimationFrame(repinFrame.current);
      if (smoothScrollTimer.current !== null) clearTimeout(smoothScrollTimer.current);
      for (const frame of layoutScrollFrames.current) cancelAnimationFrame(frame);
      layoutScrollFrames.current = [];
    };
  }, []);

  const updateBottomState = useCallback((el: HTMLElement) => {
    const atBottom = isNearBottom(el);
    stick.current = atBottom;
    setIsAtBottom(atBottom);
    if (!isTranscriptSelectionMode(modeRef.current)) {
      modeRef.current = atBottom ? "tail-follow" : "manual";
    }
    return atBottom;
  }, []);

  const cancelPendingBottomScroll = useCallback(() => {
    if (resizeFrame.current !== null) {
      cancelAnimationFrame(resizeFrame.current);
      resizeFrame.current = null;
    }
    if (repinFrame.current !== null) {
      cancelAnimationFrame(repinFrame.current);
      repinFrame.current = null;
    }
    pendingRepinHeightDelta.current = 0;
    for (const frame of layoutScrollFrames.current) cancelAnimationFrame(frame);
    layoutScrollFrames.current = [];
  }, []);

  const setMode = useCallback((mode: TranscriptScrollMode, _reason?: string) => {
    modeRef.current = mode;
    if (scrollRef.current) scrollRef.current.dataset.scrollMode = mode;
    if (isTranscriptSelectionMode(mode)) cancelPendingBottomScroll();
  }, [cancelPendingBottomScroll]);

  const writeOffset = useCallback((owner: TranscriptScrollOwner, top: number, behavior: ScrollBehavior = "auto") => {
    const el = scrollRef.current;
    if (!el || !canTranscriptScrollOwnerWrite(modeRef.current, owner)) return false;
    if (typeof el.scrollTo === "function") el.scrollTo({ top, behavior });
    else el.scrollTop = top;
    return true;
  }, []);

  const releaseAutoScroll = useCallback(() => {
    const el = scrollRef.current;
    if (isTranscriptSelectionMode(modeRef.current)) return;
    if (smoothScrollTimer.current !== null) {
      clearTimeout(smoothScrollTimer.current);
      smoothScrollTimer.current = null;
    }
    // A same-position instant scroll cancels an in-flight native smooth scroll.
    if (el) writeOffset("virtualizer", el.scrollTop);
    cancelPendingBottomScroll();
    stick.current = false;
    setIsAtBottom(false);
    modeRef.current = "manual";
  }, [cancelPendingBottomScroll, writeOffset]);

  const onWheelIntent = useCallback((event: ReactWheelEvent<HTMLElement>) => {
    const el = scrollRef.current;
    // ctrlKey marks a pinch-zoom gesture synthesized as a wheel event (trackpads on
    // macOS/Chrome), not a scroll — treating it as scroll intent would release
    // tail-follow on a zoom that never actually moved scrollTop.
    if (!el || isTranscriptSelectionMode(modeRef.current) || !isScrollable(el) || event.ctrlKey || event.deltaY === 0 || Math.abs(event.deltaX) > Math.abs(event.deltaY)) return false;
    if (event.deltaY < 0 || !isNearBottom(el)) {
      releaseAutoScroll();
      return true;
    }
    return false;
  }, [releaseAutoScroll]);

  const onTouchStartIntent = useCallback((event: ReactTouchEvent<HTMLElement>) => {
    touchStartY.current = event.touches[0]?.clientY ?? null;
  }, []);

  const onTouchMoveIntent = useCallback((event: ReactTouchEvent<HTMLElement>) => {
    const el = scrollRef.current;
    const startY = touchStartY.current;
    const currentY = event.touches[0]?.clientY;
    if (!el || isTranscriptSelectionMode(modeRef.current) || !isScrollable(el) || startY === null || currentY === undefined) return false;
    const deltaY = currentY - startY;
    if (Math.abs(deltaY) < TOUCH_SCROLL_THRESHOLD_PX) return false;
    if (deltaY > 0 || !isNearBottom(el)) {
      releaseAutoScroll();
      return true;
    }
    return false;
  }, [releaseAutoScroll]);

  const onKeyScrollIntent = useCallback((event: ReactKeyboardEvent<HTMLElement>) => {
    const el = scrollRef.current;
    // The transcript's scroll keys (Home/End/arrows/space/page keys) are also
    // ordinary text-editing keys. This listener runs on the capture phase, ahead
    // of a nested message-edit textarea's own key handling, so without this guard
    // moving the cursor while editing an earlier message would release tail-follow
    // on a completely unrelated stream, even though nothing was scrolled.
    if (!el || isTranscriptSelectionMode(modeRef.current) || !isScrollable(el) || isEditableTarget(event.target)) return false;
    if (SCROLL_BREAK_KEYS.has(event.key) || (CONDITIONAL_SCROLL_KEYS.has(event.key) && !isNearBottom(el))) {
      releaseAutoScroll();
      return true;
    }
    return false;
  }, [releaseAutoScroll]);

  const onScroll = useCallback(() => {
    const el = scrollRef.current;
    if (el) updateBottomState(el);
  }, [updateBottomState]);

  /** Scroll smoothly to a specific element.  Used by the JumpBar. */
  const smoothScrollTo = useCallback((element: HTMLElement, offset = 12) => {
    const el = scrollRef.current;
    if (!el) return;
    stick.current = false;
    setIsAtBottom(false);
    modeRef.current = "programmatic";
    if (resizeFrame.current !== null) {
      cancelAnimationFrame(resizeFrame.current);
      resizeFrame.current = null;
    }
    const rect = element.getBoundingClientRect();
    const containerRect = el.getBoundingClientRect();
    const top = el.scrollTop + rect.top - containerRect.top - offset;
    const reduced = prefersReducedMotion();
    const target = Math.max(0, top);
    writeOffset("jump", target, reduced ? "auto" : "smooth");
    if (smoothScrollTimer.current !== null) clearTimeout(smoothScrollTimer.current);
    if (reduced) updateBottomState(el);
    else {
      const generation = generationRef.current;
      smoothScrollTimer.current = window.setTimeout(() => {
        smoothScrollTimer.current = null;
        if (generation !== generationRef.current) return;
        updateBottomState(el);
      }, DUR_FAST * 2 * 1000);
    }
  }, [updateBottomState, writeOffset]);

  /** Force-scroll to the bottom — used when a new question is sent. */
  const scrollToBottom = useCallback((force = false, owner: TranscriptScrollOwner = "stream") => {
    const el = scrollRef.current;
    if (!el || !canTranscriptScrollOwnerWrite(modeRef.current, owner)) return;
    if (force) {
      modeRef.current = "tail-follow";
      stick.current = true;
      setIsAtBottom(true);
    }
    if (!stick.current && !force) return;
    if (resizeFrame.current !== null) {
      cancelAnimationFrame(resizeFrame.current);
      resizeFrame.current = null;
    }
    resizeFrame.current = requestAnimationFrame(() => {
      resizeFrame.current = null;
      if (!stick.current && !force) return;
      if (force) {
        stick.current = true;
        setIsAtBottom(true);
      }
      // Streaming tail-follow should settle in one frame. Smooth tweens queue
      // behind token/layout updates and are a common source of WebView jank.
      if (!writeOffset(owner, el.scrollHeight)) return;
      stick.current = true;
      setIsAtBottom(true);
    });
  }, [writeOffset]);

  const snapToBottom = useCallback((owner: TranscriptScrollOwner = "jump-bottom") => {
    const el = scrollRef.current;
    if (!el) return;
    if (resizeFrame.current !== null) {
      cancelAnimationFrame(resizeFrame.current);
      resizeFrame.current = null;
    }
    if (smoothScrollTimer.current !== null) {
      clearTimeout(smoothScrollTimer.current);
      smoothScrollTimer.current = null;
    }
    stick.current = true;
    writeOffset(owner, el.scrollHeight);
    setIsAtBottom(true);
  }, [writeOffset]);

  const scrollToBottomAfterLayout = useCallback((frames = 4, owner: TranscriptScrollOwner = "jump-bottom") => {
    for (const frame of layoutScrollFrames.current) cancelAnimationFrame(frame);
    layoutScrollFrames.current = [];
    snapToBottom(owner);
    let remaining = Math.max(0, frames);
    const tick = () => {
      if (remaining <= 0) return;
      const frame = requestAnimationFrame(() => {
        layoutScrollFrames.current = layoutScrollFrames.current.filter((id) => id !== frame);
        snapToBottom(owner);
        remaining -= 1;
        tick();
      });
      layoutScrollFrames.current.push(frame);
    };
    tick();
  }, [snapToBottom]);

  /** Call when a new question is submitted — overrides stick state. */
  const onNewQuestion = useCallback(() => {
    stick.current = true;
    scrollToBottom(true, "jump-bottom");
  }, [scrollToBottom]);

  /**
   * Refresh pin state on resize — call from a ResizeObserver on the container.
   */
  const repinIfWasPinned = useCallback(
    (containerHeightDelta: number, owner: TranscriptScrollOwner = "container-resize") => {
      const el = scrollRef.current;
      if (!el) return;
      const bottomDistance = el.scrollHeight - el.scrollTop - el.clientHeight;
      if (!stick.current && bottomDistance + containerHeightDelta >= BOTTOM_THRESHOLD_PX) return;
      stick.current = true;
      setIsAtBottom(true);
      scrollToBottom(false, owner);
    },
    [scrollToBottom],
  );

  const scheduleRepinIfWasPinned = useCallback(
    (containerHeightDelta: number, owner: TranscriptScrollOwner = "container-resize") => {
      pendingRepinHeightDelta.current += containerHeightDelta;
      if (repinFrame.current !== null) return;
      repinFrame.current = requestAnimationFrame(() => {
        repinFrame.current = null;
        const delta = pendingRepinHeightDelta.current;
        pendingRepinHeightDelta.current = 0;
        if (canTranscriptScrollOwnerWrite(modeRef.current, owner)) repinIfWasPinned(delta, owner);
      });
    },
    [repinIfWasPinned],
  );

  const resetGeneration = useCallback((_tabId?: string, _revealSignal?: number) => {
    generationRef.current += 1;
    cancelPendingBottomScroll();
    if (smoothScrollTimer.current !== null) {
      clearTimeout(smoothScrollTimer.current);
      smoothScrollTimer.current = null;
      const el = scrollRef.current;
      modeRef.current = "programmatic";
      if (el) writeOffset("virtualizer", el.scrollTop);
    }
    modeRef.current = "tail-follow";
    if (scrollRef.current) scrollRef.current.dataset.scrollMode = "tail-follow";
    stick.current = true;
    setIsAtBottom(true);
    return generationRef.current;
  }, [cancelPendingBottomScroll, writeOffset]);

  const canVirtualizerAdjust = useCallback(() => !isTranscriptSelectionMode(modeRef.current), []);

  const captureViewportAnchor = useCallback((): TranscriptViewportAnchor | null => {
    const el = scrollRef.current;
    if (!el) return null;
    const containerTop = el.getBoundingClientRect().top;
    const rows = Array.from(el.querySelectorAll<HTMLElement>(".transcript__row[data-row-key]"));
    const anchor = rows
      .map((row) => ({ row, offset: row.getBoundingClientRect().top - containerTop }))
      .filter(({ row, offset }) => offset + row.getBoundingClientRect().height >= 0)
      .sort((a, b) => Math.abs(a.offset) - Math.abs(b.offset))[0];
    const rowKey = anchor?.row.dataset.rowKey;
    return rowKey == null ? null : { rowKey, viewportOffset: anchor.offset, generation: generationRef.current };
  }, []);

  const reconcileViewportAnchor = useCallback((snapshot: TranscriptViewportAnchor | null) => {
    const el = scrollRef.current;
    if (!el || !snapshot || snapshot.generation !== generationRef.current) return false;
    const row = Array.from(el.querySelectorAll<HTMLElement>(".transcript__row[data-row-key]"))
      .find((candidate) => candidate.dataset.rowKey === snapshot.rowKey);
    if (!row) return false;
    const currentOffset = row.getBoundingClientRect().top - el.getBoundingClientRect().top;
    const delta = currentOffset - snapshot.viewportOffset;
    if (Math.abs(delta) < 0.5) return true;
    modeRef.current = "reconciling";
    const wrote = writeOffset("virtualizer", el.scrollTop + delta);
    modeRef.current = "manual";
    return wrote;
  }, [writeOffset]);

  /**
   * Track question count changes to call onNewQuestion.
   * Returns the previous length ref for useEffect comparison.
   */
  const trackQuestions = useCallback(
    (questionsLen: number) => {
      if (questionsLen > prevQuestionsLen.current) {
        onNewQuestion();
      }
      prevQuestionsLen.current = questionsLen;
    },
    [onNewQuestion],
  );

  return {
    scrollRef,
    stick,
    onScroll,
    onWheelIntent,
    onTouchStartIntent,
    onTouchMoveIntent,
    onKeyScrollIntent,
    isAtBottom,
    smoothScrollTo,
    scrollToBottom,
    scrollToBottomAfterLayout,
    onNewQuestion,
    repinIfWasPinned,
    scheduleRepinIfWasPinned,
    trackQuestions,
    resizeFrame,
    lastClientHeight,
    lastFooterHeight,
    modeRef,
    setMode,
    writeOffset,
    resetGeneration,
    canVirtualizerAdjust,
    captureViewportAnchor,
    reconcileViewportAnchor,
  };
}
