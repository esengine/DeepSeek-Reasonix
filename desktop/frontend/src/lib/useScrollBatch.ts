import { useEffect, useRef } from "react";

// useScrollBatch coalesces rapid scroll-to-bottom requests into a single
// requestAnimationFrame callback. During streaming, text deltas arrive at
// 30-60fps; without coalescing, each delta triggers a scrollTop assignment
// that forces a synchronous layout recalculation. By batching, we collapse
// N deltas per frame into one scroll update, reducing layout thrash.
//
// Usage: call scheduleScroll() on every content change; the ref callback
// runs once per frame at most, right before paint.
export function useScrollBatch(
  containerRef: React.RefObject<HTMLDivElement | null>,
  enabled: boolean,
) {
  const rafRef = useRef(0);

  // Cancel any pending rAF on unmount.
  useEffect(() => {
    return () => {
      if (rafRef.current) cancelAnimationFrame(rafRef.current);
    };
  }, []);

  return () => {
    if (!enabled) return;
    if (rafRef.current) return; // already scheduled this frame
    rafRef.current = requestAnimationFrame(() => {
      rafRef.current = 0;
      const el = containerRef.current;
      if (el) el.scrollTop = el.scrollHeight;
    });
  };
}
