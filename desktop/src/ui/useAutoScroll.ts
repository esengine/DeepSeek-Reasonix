import { useCallback, useEffect, useRef, useState } from "react";

const PIN_THRESHOLD = 80; // px from bottom to consider "pinned"

/**
 * Auto-scroll to bottom when content grows, as long as the user is pinned to the bottom.
 * When the user scrolls up, auto-scroll pauses. A "jump to bottom" button appears.
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

  const updateJumpButton = useCallback(() => {
    const el = containerRef.current;
    if (!el) return;
    const elScrollHeight = el.scrollHeight;
    const elClientHeight = el.clientHeight;
    const isPinned =
      el.scrollTop + elClientHeight >= elScrollHeight - PIN_THRESHOLD;
    isPinnedRef.current = isPinned;
    setShowJumpButton(
      !isPinned && elScrollHeight > elClientHeight + PIN_THRESHOLD,
    );
  }, [containerRef]);

  // Scroll to bottom
  const scrollToBottom = useCallback(
    (smooth = true) => {
      const el = containerRef.current;
      if (!el) return;
      el.scrollTo({
        top: el.scrollHeight,
        behavior: smooth ? "smooth" : "instant",
      });
      isPinnedRef.current = true;
      setShowJumpButton(false);
    },
    [containerRef],
  );

  // Listen for scroll events on the container
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    el.addEventListener("scroll", updateJumpButton, { passive: true });
    return () => el.removeEventListener("scroll", updateJumpButton);
  }, [containerRef, updateJumpButton]);

  // When busy→idle (turn completes), always scroll to final answer
  useEffect(() => {
    if (wasBusyRef.current && !busy) {
      scrollToBottom(true);
    }
    wasBusyRef.current = busy;
  }, [busy, scrollToBottom]);

  // Watch content size changes (streaming text, tool results, new messages)
  // and auto-scroll if user is pinned to bottom.
  // Uses requestAnimationFrame to batch rapid resize events during streaming.
  useEffect(() => {
    const content = contentRef.current;
    if (!content) return;

    const ro = new ResizeObserver(() => {
      // Cancel any pending rAF to batch rapid resize events
      if (rafIdRef.current) {
        cancelAnimationFrame(rafIdRef.current);
      }
      rafIdRef.current = requestAnimationFrame(() => {
        rafIdRef.current = 0;
        const el = containerRef.current;
        if (!el) return;

        if (isPinnedRef.current) {
          el.scrollTo({ top: el.scrollHeight, behavior: "instant" });
          // After scrolling, re-check if we're still at bottom
          const stillPinned =
            el.scrollTop + el.clientHeight >=
            el.scrollHeight - PIN_THRESHOLD;
          if (!stillPinned) {
            isPinnedRef.current = false;
            setShowJumpButton(
              el.scrollHeight > el.clientHeight + PIN_THRESHOLD,
            );
          }
        } else {
          setShowJumpButton(
            el.scrollHeight > el.clientHeight + PIN_THRESHOLD,
          );
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
  }, [containerRef, contentRef]);

  // Initial scroll to bottom when hook mounts (e.g., session loaded)
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const id = setTimeout(() => {
      el.scrollTo({ top: el.scrollHeight, behavior: "instant" });
      isPinnedRef.current = true;
      setShowJumpButton(false);
    }, 50);
    return () => clearTimeout(id);
  }, [containerRef]);

  return { showJumpButton, scrollToBottom };
}
