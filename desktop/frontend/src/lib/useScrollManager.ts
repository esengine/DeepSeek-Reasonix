import { useCallback, useEffect, useRef, useState } from "react";
import type {
  KeyboardEvent as ReactKeyboardEvent,
  PointerEvent as ReactPointerEvent,
  TouchEvent as ReactTouchEvent,
  UIEvent as ReactUIEvent,
  WheelEvent as ReactWheelEvent,
} from "react";
import { DUR_FAST, prefersReducedMotion } from "./motion";
import { isEditableTarget } from "./keyboardShortcuts";
import {
  isTranscriptSelectionMode,
  type TranscriptScrollMode,
  type TranscriptScrollOwner,
  type TranscriptViewportAnchor,
} from "./transcriptScrollController";
import {
  canTranscriptScrollOwnerWriteNow,
  canVirtualizerAdjustScroll,
  canScrollEndSettle,
  isUserGestureActive,
  noteUserGesture,
  type TranscriptUserScrollSource,
} from "./transcriptScrollSession";

declare global {
  interface Window {
    __REASONIX_TRANSCRIPT_SCROLL_WRITE__?: (owner: TranscriptScrollOwner, top: number) => void;
  }
}

const BOTTOM_THRESHOLD_PX = 80;
const BOTTOM_REENGAGE_PX = 0.5;
const TOUCH_SCROLL_THRESHOLD_PX = 2;
const PROGRAMMATIC_SCROLL_EVENT_HOLD_MS = 96;
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
const PASSIVE_TAIL_OWNERS = new Set<TranscriptScrollOwner>([
  "stream",
  "container-resize",
  "footer-resize",
  "row-size",
]);

function isNearBottom(el: HTMLElement): boolean {
  return el.scrollHeight - el.scrollTop - el.clientHeight < BOTTOM_THRESHOLD_PX;
}

function isAtPhysicalBottom(el: HTMLElement): boolean {
  return el.scrollHeight - el.scrollTop - el.clientHeight <= BOTTOM_REENGAGE_PX;
}

function isScrollable(el: HTMLElement): boolean {
  return el.scrollHeight - el.clientHeight > 1;
}

/** Cancel an in-flight smooth scroll without going through owner arbitration. */
function cancelInFlightSmoothScroll(el: HTMLElement) {
  if (typeof el.scrollTo === "function") el.scrollTo({ top: el.scrollTop, behavior: "auto" });
}

/**
 * useScrollManager — frame-batched auto-scroll for the transcript container.
 *
 * - Auto-pins to the bottom when content is near the edge.
 * - Smooth scroll for jump-to-question navigation.
 * - Batches ResizeObserver callbacks into a single animation frame.
 * - Holds a short user-gesture lock so virtualizer/stream writes cannot fight
 *   native trackpad inertia (stalls/jumps in long transcripts).
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
  /** Epoch ms until which compensating writers must stay silent. */
  const gestureUntilRef = useRef(0);
  const gestureSourceRef = useRef<TranscriptUserScrollSource | null>(null);
  const gestureLastActivityRef = useRef(0);
  const gestureIdleTimerRef = useRef<number | null>(null);
  const gestureIdleListenersRef = useRef(new Set<() => void>());
  const deferredTailRepinRef = useRef<{
    generation: number;
    owner: TranscriptScrollOwner;
  } | null>(null);
  const programmaticScrollRef = useRef<{
    generation: number;
    owner: TranscriptScrollOwner;
    target: number;
    behavior: ScrollBehavior;
    expiresAt: number;
  } | null>(null);
  // Near-bottom is a layout tolerance, not user intent. Once the reader moves
  // upward, keep tail-follow suppressed until they explicitly return to the
  // physical bottom (or use an explicit bottom/reset action).
  const tailFollowSuppressedRef = useRef(false);
  const [isAtBottom, setIsAtBottom] = useState(true);
  const modeRef = useRef<TranscriptScrollMode>("tail-follow");
  const generationRef = useRef(0);

  const flushGestureIdleListeners = useCallback(() => {
    if (isUserGestureActive(gestureUntilRef.current)) return;
    for (const listener of gestureIdleListenersRef.current) {
      try {
        listener();
      } catch {
        // Listener failures must not break scroll ownership.
      }
    }
  }, []);

  const clearGesture = useCallback((notify: boolean) => {
    gestureUntilRef.current = 0;
    gestureSourceRef.current = null;
    gestureLastActivityRef.current = 0;
    if (gestureIdleTimerRef.current !== null) {
      clearTimeout(gestureIdleTimerRef.current);
      gestureIdleTimerRef.current = null;
    }
    const element = scrollRef.current;
    if (element) delete element.dataset.scrollGesture;
    if (notify) flushGestureIdleListeners();
  }, [flushGestureIdleListeners]);

  const scheduleGestureIdle = useCallback(() => {
    if (gestureIdleTimerRef.current !== null) {
      clearTimeout(gestureIdleTimerRef.current);
      gestureIdleTimerRef.current = null;
    }
    const delay = Math.max(0, gestureUntilRef.current - Date.now()) + 16;
    gestureIdleTimerRef.current = window.setTimeout(() => {
      gestureIdleTimerRef.current = null;
      if (isUserGestureActive(gestureUntilRef.current)) {
        scheduleGestureIdle();
        return;
      }
      clearGesture(true);
    }, delay);
  }, [clearGesture]);

  useEffect(() => {
    return () => {
      if (resizeFrame.current !== null) cancelAnimationFrame(resizeFrame.current);
      if (repinFrame.current !== null) cancelAnimationFrame(repinFrame.current);
      if (smoothScrollTimer.current !== null) clearTimeout(smoothScrollTimer.current);
      if (gestureIdleTimerRef.current !== null) clearTimeout(gestureIdleTimerRef.current);
      for (const frame of layoutScrollFrames.current) cancelAnimationFrame(frame);
      layoutScrollFrames.current = [];
      gestureIdleListenersRef.current.clear();
    };
  }, []);

  const markUserGesture = useCallback((source: TranscriptUserScrollSource = "native-scroll") => {
    const now = Date.now();
    gestureUntilRef.current = noteUserGesture(now);
    gestureLastActivityRef.current = now;
    gestureSourceRef.current = source;
    const element = scrollRef.current;
    if (element) element.dataset.scrollGesture = source;
    scheduleGestureIdle();
  }, [scheduleGestureIdle]);

  const finishUserGesture = useCallback(() => {
    if (gestureUntilRef.current === 0 && gestureSourceRef.current === null) return;
    clearGesture(true);
  }, [clearGesture]);

  /**
   * Subscribe to the first quiet frame after a user scroll gesture ends.
   * Used to batch virtualizer.measure() so remount heights settle without
   * fighting trackpad inertia mid-gesture.
   */
  const onGestureIdle = useCallback((listener: () => void) => {
    gestureIdleListenersRef.current.add(listener);
    return () => {
      gestureIdleListenersRef.current.delete(listener);
    };
  }, []);

  const updateBottomState = useCallback((el: HTMLElement, preserveMode = false) => {
    const shouldFollow = isNearBottom(el) && !tailFollowSuppressedRef.current;
    stick.current = shouldFollow;
    setIsAtBottom(shouldFollow);
    if (!preserveMode && !isTranscriptSelectionMode(modeRef.current)) {
      modeRef.current = shouldFollow ? "tail-follow" : "manual";
      el.dataset.scrollMode = modeRef.current;
    }
    return shouldFollow;
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
    deferredTailRepinRef.current = null;
    for (const frame of layoutScrollFrames.current) cancelAnimationFrame(frame);
    layoutScrollFrames.current = [];
  }, []);

  const setMode = useCallback((mode: TranscriptScrollMode, _reason?: string) => {
    if (mode === "tail-follow") tailFollowSuppressedRef.current = false;
    else deferredTailRepinRef.current = null;
    modeRef.current = mode;
    if (scrollRef.current) scrollRef.current.dataset.scrollMode = mode;
    if (isTranscriptSelectionMode(mode)) cancelPendingBottomScroll();
  }, [cancelPendingBottomScroll]);

  const deferTailRepin = useCallback((owner: TranscriptScrollOwner, now: number) => {
    if (!PASSIVE_TAIL_OWNERS.has(owner)) return false;
    if (!isUserGestureActive(gestureUntilRef.current, now)) return false;
    if (modeRef.current !== "tail-follow" || !stick.current || tailFollowSuppressedRef.current) return false;
    deferredTailRepinRef.current = { generation: generationRef.current, owner };
    return true;
  }, []);

  const writeOffset = useCallback((owner: TranscriptScrollOwner, top: number, behavior: ScrollBehavior = "auto") => {
    const el = scrollRef.current;
    if (!el) return false;
    const now = Date.now();
    if (!canTranscriptScrollOwnerWriteNow(modeRef.current, owner, gestureUntilRef.current, now)) {
      deferTailRepin(owner, now);
      return false;
    }
    if (owner === "jump" || owner === "rewind" || owner === "jump-bottom" || owner === "custom-scrollbar") {
      cancelPendingBottomScroll();
      clearGesture(false);
    }
    const target = Math.max(0, Math.min(top, Math.max(0, el.scrollHeight - el.clientHeight)));
    if (owner === "custom-scrollbar") {
      tailFollowSuppressedRef.current = Math.max(0, el.scrollHeight - el.clientHeight) - target > BOTTOM_REENGAGE_PX;
    } else if (owner === "jump-bottom") {
      tailFollowSuppressedRef.current = false;
    }
    programmaticScrollRef.current = {
      generation: generationRef.current,
      owner,
      target,
      behavior,
      expiresAt: Date.now() + (behavior === "smooth" ? DUR_FAST * 2 * 1000 : PROGRAMMATIC_SCROLL_EVENT_HOLD_MS),
    };
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__?.(owner, top);
    if (typeof el.scrollTo === "function") el.scrollTo({ top, behavior });
    else el.scrollTop = top;
    return true;
  }, [cancelPendingBottomScroll, clearGesture, deferTailRepin]);

  useEffect(() => onGestureIdle(() => {
    const pending = deferredTailRepinRef.current;
    deferredTailRepinRef.current = null;
    const el = scrollRef.current;
    if (!pending || !el || pending.generation !== generationRef.current) return;
    if (modeRef.current !== "tail-follow" || !stick.current || tailFollowSuppressedRef.current) return;
    writeOffset(pending.owner, el.scrollHeight);
  }), [onGestureIdle, writeOffset]);

  const consumeProgrammaticScroll = useCallback((top: number) => {
    const marker = programmaticScrollRef.current;
    if (!marker || marker.generation !== generationRef.current || marker.expiresAt < Date.now()) {
      programmaticScrollRef.current = null;
      return false;
    }
    if (marker.behavior === "smooth") return true;
    if (Math.abs(marker.target - top) <= 1) {
      programmaticScrollRef.current = null;
      return true;
    }
    return false;
  }, []);

  const releaseAutoScroll = useCallback(() => {
    const el = scrollRef.current;
    if (isTranscriptSelectionMode(modeRef.current)) return;
    if (smoothScrollTimer.current !== null) {
      clearTimeout(smoothScrollTimer.current);
      smoothScrollTimer.current = null;
    }
    // Cancel in-flight smooth scrolling without owner arbitration so a mid-gesture
    // release is never blocked by the gesture lock itself.
    if (el) cancelInFlightSmoothScroll(el);
    cancelPendingBottomScroll();
    tailFollowSuppressedRef.current = true;
    stick.current = false;
    setIsAtBottom(false);
    modeRef.current = "manual";
    if (scrollRef.current) scrollRef.current.dataset.scrollMode = "manual";
  }, [cancelPendingBottomScroll]);

  const onWheelIntent = useCallback((event: ReactWheelEvent<HTMLElement>) => {
    const el = scrollRef.current;
    // ctrlKey marks a pinch-zoom gesture synthesized as a wheel event (trackpads on
    // macOS/Chrome), not a scroll — treating it as scroll intent would release
    // tail-follow on a zoom that never actually moved scrollTop.
    if (!el || isTranscriptSelectionMode(modeRef.current) || !isScrollable(el) || event.ctrlKey || event.deltaY === 0 || Math.abs(event.deltaX) > Math.abs(event.deltaY)) return false;
    // Any vertical wheel on a scrollable transcript starts the gesture lock so
    // virtualizer remounts cannot rewrite scrollTop mid-inertia — including
    // wheel-down at the bottom (tail-follow stays, compensation still freezes).
    markUserGesture("wheel");
    if (event.deltaY < 0 || !isNearBottom(el)) {
      releaseAutoScroll();
      return true;
    }
    return false;
  }, [markUserGesture, releaseAutoScroll]);

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
    markUserGesture("touch");
    if (deltaY > 0 || !isNearBottom(el)) {
      releaseAutoScroll();
      return true;
    }
    return false;
  }, [markUserGesture, releaseAutoScroll]);

  const onKeyScrollIntent = useCallback((event: ReactKeyboardEvent<HTMLElement>) => {
    const el = scrollRef.current;
    // The transcript's scroll keys (Home/End/arrows/space/page keys) are also
    // ordinary text-editing keys. This listener runs on the capture phase, ahead
    // of a nested message-edit textarea's own key handling, so without this guard
    // moving the cursor while editing an earlier message would release tail-follow
    // on a completely unrelated stream, even though nothing was scrolled.
    if (!el || isTranscriptSelectionMode(modeRef.current) || !isScrollable(el) || isEditableTarget(event.target)) return false;
    if (SCROLL_BREAK_KEYS.has(event.key) || (CONDITIONAL_SCROLL_KEYS.has(event.key) && !isNearBottom(el))) {
      markUserGesture("keyboard");
      releaseAutoScroll();
      return true;
    }
    return false;
  }, [markUserGesture, releaseAutoScroll]);

  const onPointerDownIntent = useCallback((event: ReactPointerEvent<HTMLElement>) => {
    const el = scrollRef.current;
    if (event.button !== 1 || !el || !isScrollable(el) || isTranscriptSelectionMode(modeRef.current)) return false;
    releaseAutoScroll();
    markUserGesture("middle-button");
    return true;
  }, [markUserGesture, releaseAutoScroll]);

  const onNestedScrollIntent = useCallback((deltaY: number) => {
    const el = scrollRef.current;
    if (!el || isTranscriptSelectionMode(modeRef.current) || !isScrollable(el) || deltaY === 0) return false;
    markUserGesture("nested-scroll");
    if (deltaY < 0 || !isNearBottom(el)) {
      releaseAutoScroll();
      return true;
    }
    return false;
  }, [markUserGesture, releaseAutoScroll]);

  const onScroll = useCallback((event?: ReactUIEvent<HTMLElement>) => {
    const el = scrollRef.current;
    if (!el) return;
    const programmatic = consumeProgrammaticScroll(el.scrollTop) || isTranscriptSelectionMode(modeRef.current);
    if (programmatic) {
      updateBottomState(el, true);
      return;
    }
    if (event?.nativeEvent.isTrusted === false && !isUserGestureActive(gestureUntilRef.current)) {
      // Test harnesses and integration code sometimes dispatch a synthetic
      // scroll after positioning the viewport. It is not user ownership.
      updateBottomState(el);
      return;
    }
    // Native scrollbar drags and Windows middle-button auto-scroll can emit
    // scroll events without wheel samples. Treat every unowned scroll as user
    // activity; controller-owned writes are consumed above.
    markUserGesture(gestureSourceRef.current ?? "native-scroll");
    tailFollowSuppressedRef.current = !isAtPhysicalBottom(el);
    updateBottomState(el);
    if (tailFollowSuppressedRef.current) cancelPendingBottomScroll();
  }, [cancelPendingBottomScroll, consumeProgrammaticScroll, markUserGesture, updateBottomState]);

  const onScrollEnd = useCallback(() => {
    // WebViews can dispatch scrollend after a controller-owned write or after
    // the preceding native gesture was already settled. It is only a hint for
    // an existing user session; never manufacture a new idle cycle from it.
    if (gestureUntilRef.current === 0 || gestureSourceRef.current === null) return;
    if (!canScrollEndSettle(gestureLastActivityRef.current)) {
      scheduleGestureIdle();
      return;
    }
    finishUserGesture();
  }, [finishUserGesture, scheduleGestureIdle]);

  const finishProgrammaticScroll = useCallback(() => {
    programmaticScrollRef.current = null;
    const el = scrollRef.current;
    if (!el || isTranscriptSelectionMode(modeRef.current)) return;
    modeRef.current = "manual";
    updateBottomState(el);
  }, [updateBottomState]);

  /** Scroll smoothly to a specific element.  Used by the JumpBar. */
  const smoothScrollTo = useCallback((element: HTMLElement, offset = 12) => {
    const el = scrollRef.current;
    if (!el) return;
    stick.current = false;
    setIsAtBottom(false);
    modeRef.current = "programmatic";
    if (scrollRef.current) scrollRef.current.dataset.scrollMode = "programmatic";
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
    if (reduced) finishProgrammaticScroll();
    else {
      const generation = generationRef.current;
      smoothScrollTimer.current = window.setTimeout(() => {
        smoothScrollTimer.current = null;
        if (generation !== generationRef.current) return;
        finishProgrammaticScroll();
      }, DUR_FAST * 2 * 1000);
    }
  }, [finishProgrammaticScroll, writeOffset]);

  /** Force-scroll to the bottom — used when a new question is sent. */
  const scrollToBottom = useCallback((force = false, owner: TranscriptScrollOwner = "stream") => {
    const el = scrollRef.current;
    if (!el || isTranscriptSelectionMode(modeRef.current)) return;
    if (force) {
      tailFollowSuppressedRef.current = false;
      modeRef.current = "tail-follow";
      if (scrollRef.current) scrollRef.current.dataset.scrollMode = "tail-follow";
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
    if (!el || isTranscriptSelectionMode(modeRef.current)) return;
    const explicit = owner === "jump-bottom";
    if (!explicit && (modeRef.current !== "tail-follow" || !stick.current || tailFollowSuppressedRef.current)) return;
    if (resizeFrame.current !== null) {
      cancelAnimationFrame(resizeFrame.current);
      resizeFrame.current = null;
    }
    if (smoothScrollTimer.current !== null) {
      clearTimeout(smoothScrollTimer.current);
      smoothScrollTimer.current = null;
    }
    if (explicit) {
      tailFollowSuppressedRef.current = false;
      modeRef.current = "tail-follow";
      el.dataset.scrollMode = "tail-follow";
      stick.current = true;
    }
    if (writeOffset(owner, el.scrollHeight)) setIsAtBottom(true);
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
    // A new send is an explicit user action; clear any residual gesture lock so
    // jump-bottom is not deferred by the previous scroll session.
    clearGesture(false);
    scrollToBottom(true, "jump-bottom");
  }, [clearGesture, scrollToBottom]);

  /**
   * Refresh pin state on resize — call from a ResizeObserver on the container.
   */
  const repinIfWasPinned = useCallback(
    (containerHeightDelta: number, owner: TranscriptScrollOwner = "container-resize") => {
      const el = scrollRef.current;
      if (!el) return;
      if (tailFollowSuppressedRef.current) return;
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
        repinIfWasPinned(delta, owner);
      });
    },
    [repinIfWasPinned],
  );

  const resetGeneration = useCallback((_tabId?: string, _revealSignal?: number) => {
    generationRef.current += 1;
    cancelPendingBottomScroll();
    clearGesture(false);
    programmaticScrollRef.current = null;
    if (smoothScrollTimer.current !== null) {
      clearTimeout(smoothScrollTimer.current);
      smoothScrollTimer.current = null;
      const el = scrollRef.current;
      modeRef.current = "programmatic";
      if (el) cancelInFlightSmoothScroll(el);
    }
    modeRef.current = "tail-follow";
    tailFollowSuppressedRef.current = false;
    if (scrollRef.current) scrollRef.current.dataset.scrollMode = "tail-follow";
    stick.current = true;
    setIsAtBottom(true);
    return generationRef.current;
  }, [cancelPendingBottomScroll, clearGesture]);

  const canVirtualizerAdjust = useCallback(
    () => canVirtualizerAdjustScroll(modeRef.current, gestureUntilRef.current),
    [],
  );

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
    // Never fight an active trackpad gesture with anchor reconciliation.
    if (!canVirtualizerAdjustScroll(modeRef.current, gestureUntilRef.current)) return false;
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
    onScrollEnd,
    onWheelIntent,
    onPointerDownIntent,
    onNestedScrollIntent,
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
    /** Marks an external user scroll intent (e.g. nested-edge handoff). */
    markUserGesture,
    /** Fires once after the gesture hold expires (idle remeasure hook). */
    onGestureIdle,
    gestureUntilRef,
    gestureSourceRef,
    gestureLastActivityRef,
    finishProgrammaticScroll,
  };
}
