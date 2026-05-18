import { useCallback, useEffect, useRef, useState } from "react";

const PIN_THRESHOLD = 80; // px from bottom to consider "pinned"

/**
 * Auto-scroll to bottom while content grows; un-pin only on real user input.
 *
 * The "user scrolled up" signal comes from wheel / touchmove / keydown,
 * NOT from scroll events. Scroll events fire for both user gestures and
 * our own scrollTo, and a smooth scrollTo can keep dispatching scroll
 * events for 200-500 ms — long enough to misread "smooth scroll
 * mid-flight" as "user scrolled up" and freeze the view (issue #1103).
 */
export function useAutoScroll(
  containerRef: React.RefObject<HTMLDivElement | null>,
  contentRef: React.RefObject<HTMLDivElement | null>,
  busy: boolean,
) {
  const [showJumpButton, setShowJumpButton] = useState(false);
  const isPinnedRef = useRef(true);
  const wasBusyRef = useRef(busy);
  const rafIdRef = useRef<number>(0);

  const isAtBottom = useCallback(() => {
    const el = containerRef.current;
    if (!el) return true;
    return el.scrollTop + el.clientHeight >= el.scrollHeight - PIN_THRESHOLD;
  }, [containerRef]);

  const refreshJumpButton = useCallback(() => {
    const el = containerRef.current;
    if (!el) return;
    setShowJumpButton(
      !isPinnedRef.current && el.scrollHeight > el.clientHeight + PIN_THRESHOLD,
    );
  }, [containerRef]);

  const scrollToBottom = useCallback(
    (smooth = true) => {
      const el = containerRef.current;
      if (!el) return;
      isPinnedRef.current = true;
      setShowJumpButton(false);
      el.scrollTo({
        top: el.scrollHeight,
        behavior: smooth ? "smooth" : "instant",
      });
    },
    [containerRef],
  );

  // User-intent detection: only these gestures un-pin. Scroll events are
  // intentionally NOT listened to — they can't tell user gestures from our
  // own scrollTo.
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;

    // rAF lets the gesture's scroll delta land before we measure.
    let pendingFrame = 0;
    const onUserGesture = () => {
      if (pendingFrame) cancelAnimationFrame(pendingFrame);
      pendingFrame = requestAnimationFrame(() => {
        pendingFrame = 0;
        isPinnedRef.current = isAtBottom();
        refreshJumpButton();
      });
    };

    el.addEventListener("wheel", onUserGesture, { passive: true });
    el.addEventListener("touchmove", onUserGesture, { passive: true });
    el.addEventListener("keydown", onUserGesture);
    // pointerdown on the scrollbar gutter starts a drag-scroll. The
    // drag itself fires no wheel/touch, but pointerdown's followup
    // scroll arrives within a frame; one rAF measure catches it.
    el.addEventListener("pointerdown", onUserGesture);

    return () => {
      if (pendingFrame) cancelAnimationFrame(pendingFrame);
      el.removeEventListener("wheel", onUserGesture);
      el.removeEventListener("touchmove", onUserGesture);
      el.removeEventListener("keydown", onUserGesture);
      el.removeEventListener("pointerdown", onUserGesture);
    };
  }, [containerRef, isAtBottom, refreshJumpButton]);

  // Both busy edges re-pin: turn start = user just sent and expects to
  // see the reply; turn end = settle on the final answer (issue #1182).
  useEffect(() => {
    if (wasBusyRef.current !== busy) {
      scrollToBottom(true);
    }
    wasBusyRef.current = busy;
  }, [busy, scrollToBottom]);

  // Watch content size changes (streaming text, tool results, new
  // messages) and follow the bottom while pinned.
  useEffect(() => {
    const content = contentRef.current;
    if (!content) return;

    const ro = new ResizeObserver(() => {
      if (rafIdRef.current) cancelAnimationFrame(rafIdRef.current);
      rafIdRef.current = requestAnimationFrame(() => {
        rafIdRef.current = 0;
        const el = containerRef.current;
        if (!el) return;
        if (isPinnedRef.current) {
          el.scrollTo({ top: el.scrollHeight, behavior: "instant" });
        } else {
          refreshJumpButton();
        }
      });
    });

    ro.observe(content);
    return () => {
      ro.disconnect();
      if (rafIdRef.current) {
        cancelAnimationFrame(rafIdRef.current);
        rafIdRef.current = 0;
      }
    };
  }, [containerRef, contentRef, refreshJumpButton]);

  // Initial scroll to bottom when hook mounts (e.g., session loaded).
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const id = setTimeout(() => {
      isPinnedRef.current = true;
      setShowJumpButton(false);
      el.scrollTo({ top: el.scrollHeight, behavior: "instant" });
    }, 50);
    return () => clearTimeout(id);
  }, [containerRef]);

  return { showJumpButton, scrollToBottom };
}
