import { useLayoutEffect, useRef } from "react";

/** Keeps a running reasoning pane at its own tail until the reader scrolls up. */
export function useReasoningScrollFollow(
  content: string,
  active: boolean,
) {
  const elementRef = useRef<HTMLDivElement>(null);
  const followRef = useRef(true);

  const onScroll = () => {
    const element = elementRef.current!;
    followRef.current = element.scrollHeight - element.scrollTop - element.clientHeight <= 8;
  };

  useLayoutEffect(() => {
    if (!active || !followRef.current) return;
    const element = elementRef.current;
    if (!element) return;
    // This is the reasoning pane's own nested scroller, not the transcript.
    element.scrollTop = element.scrollHeight;
  }, [active, content]);

  return [elementRef, onScroll] as const;
}
