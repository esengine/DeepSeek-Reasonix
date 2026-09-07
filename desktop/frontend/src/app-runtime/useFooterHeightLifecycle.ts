import { useEffect, useRef, type RefObject } from "react";

export function useFooterHeightLifecycle(
  footerRef: RefObject<HTMLElement | null>,
  onHeight: (height: number) => void,
) {
  const lastHeight = useRef(0);
  useEffect(() => {
    const element = footerRef.current;
    if (!element || typeof ResizeObserver === "undefined") return;
    let frame = 0;
    const update = () => {
      if (frame) window.cancelAnimationFrame(frame);
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        const next = Math.round(element.getBoundingClientRect().height);
        if (Math.abs(lastHeight.current - next) < 2) return;
        lastHeight.current = next;
        onHeight(next);
      });
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(element);
    return () => {
      if (frame) window.cancelAnimationFrame(frame);
      observer.disconnect();
    };
  }, [footerRef, onHeight]);
}
