import { useCallback, useEffect, useRef } from "react";
import gsap from "gsap";
import { DUR_FAST, EASE_OUT, prefersReducedMotion } from "./gsapAnimations";

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
  const lastClientHeight = useRef<number | null>(null);
  const lastFooterHeight = useRef<number | null>(null);

  // Kill any lingering tweens on unmount.
  useEffect(() => {
    return () => {
      gsapCtx.current?.revert();
      if (resizeFrame.current !== null) cancelAnimationFrame(resizeFrame.current);
    };
  }, []);

  const onScroll = useCallback(() => {
    const el = scrollRef.current;
    if (el) {
      stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    }
  }, []);

  /** Scroll smoothly to a specific element.  Used by the JumpBar. */
  const smoothScrollTo = useCallback((element: HTMLElement, offset = 12) => {
    const el = scrollRef.current;
    if (!el) return;
    stick.current = false;
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
    });
  }, []);

  /** Force-scroll to the bottom — used when a new question is sent. */
  const scrollToBottom = useCallback((force = false) => {
    const el = scrollRef.current;
    if (!el) return;
    if (force) {
      stick.current = true;
    }
    if (!stick.current) return;
    if (resizeFrame.current !== null) {
      cancelAnimationFrame(resizeFrame.current);
      resizeFrame.current = null;
    }
    resizeFrame.current = requestAnimationFrame(() => {
      resizeFrame.current = null;
      if (!stick.current) return;
      const reduced = prefersReducedMotion();
      gsap.to(el, {
        scrollTo: { y: "max" },
        duration: reduced ? 0.001 : DUR_FAST,
        ease: "none",
        overwrite: "auto",
      });
    });
  }, []);

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
      if (!stick.current && bottomDistance + containerHeightDelta >= 80) return;
      stick.current = true;
      scrollToBottom();
    },
    [scrollToBottom],
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
    smoothScrollTo,
    scrollToBottom,
    onNewQuestion,
    repinIfWasPinned,
    trackQuestions,
    resizeFrame,
    lastClientHeight,
    lastFooterHeight,
  };
}
