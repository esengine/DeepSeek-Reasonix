import { useCallback, useEffect, useRef, useState } from "react";
import gsap from "gsap";
import { DUR_FAST, EASE_OUT, prefersReducedMotion } from "./gsapAnimations";

const BOTTOM_THRESHOLD_PX = 80;

function isNearBottom(el: HTMLElement): boolean {
  return el.scrollHeight - el.scrollTop - el.clientHeight < BOTTOM_THRESHOLD_PX;
}

/**
 * useScrollManager — GSAP-driven auto-scroll for the transcript container.
 *
 * - Auto-pins to the bottom when content is near the edge.
 * - Smooth scroll for jump-to-question navigation.
 * - Uses gsap.scrollTo for layout-safe scrolling (avoids layout thrashing).
 * - Batches ResizeObserver callbacks into a single GSAP tween.
 */
export function useScrollManager() {
  const scrollRef = useRef<HTMLDivElement>(null);
  const stick = useRef(true);
  const gsapCtx = useRef<gsap.Context | null>(null);
  const prevQuestionsLen = useRef(0);
  const resizeFrame = useRef<number | null>(null);
  const repinFrame = useRef<number | null>(null);
  const pendingRepinHeightDelta = useRef(0);
  const layoutScrollFrames = useRef<number[]>([]);
  const lastClientHeight = useRef<number | null>(null);
  const lastFooterHeight = useRef<number | null>(null);
  const [isAtBottom, setIsAtBottom] = useState(true);

  // Kill any lingering tweens on unmount.
  useEffect(() => {
    return () => {
      gsapCtx.current?.revert();
      if (resizeFrame.current !== null) cancelAnimationFrame(resizeFrame.current);
      if (repinFrame.current !== null) cancelAnimationFrame(repinFrame.current);
      for (const frame of layoutScrollFrames.current) cancelAnimationFrame(frame);
      layoutScrollFrames.current = [];
    };
  }, []);

  const updateBottomState = useCallback((el: HTMLElement) => {
    const atBottom = isNearBottom(el);
    stick.current = atBottom;
    setIsAtBottom(atBottom);
    return atBottom;
  }, []);

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
    if (resizeFrame.current !== null) {
      cancelAnimationFrame(resizeFrame.current);
      resizeFrame.current = null;
    }
    const rect = element.getBoundingClientRect();
    const containerRect = el.getBoundingClientRect();
    const top = el.scrollTop + rect.top - containerRect.top - offset;
    const reduced = prefersReducedMotion();
    gsap.to(el, {
      scrollTo: { y: Math.max(0, top) },
      duration: reduced ? 0.001 : DUR_FAST * 2,
      ease: EASE_OUT,
      onComplete: () => updateBottomState(el),
    });
  }, [updateBottomState]);

  /** Force-scroll to the bottom — used when a new question is sent. */
  const scrollToBottom = useCallback((force = false) => {
    const el = scrollRef.current;
    if (!el) return;
    if (force) {
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
      const reduced = prefersReducedMotion();
      gsap.to(el, {
        scrollTo: { y: "max" },
        duration: reduced ? 0.001 : DUR_FAST,
        ease: "none",
        overwrite: "auto",
        onComplete: () => {
          stick.current = true;
          setIsAtBottom(true);
        },
      });
    });
  }, []);

  const snapToBottom = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    if (resizeFrame.current !== null) {
      cancelAnimationFrame(resizeFrame.current);
      resizeFrame.current = null;
    }
    gsap.killTweensOf(el);
    stick.current = true;
    el.scrollTop = el.scrollHeight;
    setIsAtBottom(true);
  }, []);

  const scrollToBottomAfterLayout = useCallback((frames = 4) => {
    for (const frame of layoutScrollFrames.current) cancelAnimationFrame(frame);
    layoutScrollFrames.current = [];
    snapToBottom();
    let remaining = Math.max(0, frames);
    const tick = () => {
      if (remaining <= 0) return;
      const frame = requestAnimationFrame(() => {
        layoutScrollFrames.current = layoutScrollFrames.current.filter((id) => id !== frame);
        snapToBottom();
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
    scrollToBottom(true);
  }, [scrollToBottom]);

  /**
   * Refresh pin state on resize — call from a ResizeObserver on the container.
   */
  const repinIfWasPinned = useCallback(
    (containerHeightDelta: number) => {
      const el = scrollRef.current;
      if (!el) return;
      const bottomDistance = el.scrollHeight - el.scrollTop - el.clientHeight;
      if (!stick.current && bottomDistance + containerHeightDelta >= BOTTOM_THRESHOLD_PX) return;
      stick.current = true;
      setIsAtBottom(true);
      scrollToBottom();
    },
    [scrollToBottom],
  );

  const scheduleRepinIfWasPinned = useCallback(
    (containerHeightDelta: number) => {
      pendingRepinHeightDelta.current += containerHeightDelta;
      if (repinFrame.current !== null) return;
      repinFrame.current = requestAnimationFrame(() => {
        repinFrame.current = null;
        const delta = pendingRepinHeightDelta.current;
        pendingRepinHeightDelta.current = 0;
        repinIfWasPinned(delta);
      });
    },
    [repinIfWasPinned],
  );

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
  };
}
