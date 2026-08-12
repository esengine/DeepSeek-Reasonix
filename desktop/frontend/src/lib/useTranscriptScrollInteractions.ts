import { useCallback, useEffect, type KeyboardEvent, type PointerEvent, type RefObject, type TouchEvent, type WheelEvent } from "react";
import { attachNestedScrollHandoff } from "./nestedScrollHandoff";

export function useTranscriptScrollInteractions({
  scrollRef,
  cancelStreamingScroll,
  onWheelIntent,
  onTouchMoveIntent,
  onKeyScrollIntent,
  onPointerDownIntent,
  onNestedScrollIntent,
  onScrollEnd,
  onSelectionPointerDown,
}: {
  scrollRef: RefObject<HTMLDivElement | null>;
  cancelStreamingScroll: () => void;
  onWheelIntent: (event: WheelEvent<HTMLElement>) => boolean;
  onTouchMoveIntent: (event: TouchEvent<HTMLElement>) => boolean;
  onKeyScrollIntent: (event: KeyboardEvent<HTMLElement>) => boolean;
  onPointerDownIntent: (event: PointerEvent<HTMLElement>) => boolean;
  onNestedScrollIntent: (deltaY: number) => boolean;
  onScrollEnd: () => void;
  onSelectionPointerDown: (event: PointerEvent<HTMLElement>) => void;
}) {
  useEffect(() => {
    const element = scrollRef.current;
    if (!element) return;
    const onNestedIntent = (deltaY: number) => {
      if (onNestedScrollIntent(deltaY)) cancelStreamingScroll();
    };
    const handoff = attachNestedScrollHandoff({ parent: element, onParentScrollIntent: onNestedIntent });
    element.addEventListener("scrollend", onScrollEnd);
    return () => {
      handoff.detach();
      element.removeEventListener("scrollend", onScrollEnd);
    };
  }, [cancelStreamingScroll, onNestedScrollIntent, onScrollEnd, scrollRef]);

  const onWheelCapture = useCallback((event: WheelEvent<HTMLElement>) => {
    if (onWheelIntent(event)) cancelStreamingScroll();
  }, [cancelStreamingScroll, onWheelIntent]);

  const onTouchMoveCapture = useCallback((event: TouchEvent<HTMLElement>) => {
    if (onTouchMoveIntent(event)) cancelStreamingScroll();
  }, [cancelStreamingScroll, onTouchMoveIntent]);

  const onKeyDownCapture = useCallback((event: KeyboardEvent<HTMLElement>) => {
    if (onKeyScrollIntent(event)) cancelStreamingScroll();
  }, [cancelStreamingScroll, onKeyScrollIntent]);

  const onPointerDownCapture = useCallback((event: PointerEvent<HTMLElement>) => {
    if (onPointerDownIntent(event)) cancelStreamingScroll();
    onSelectionPointerDown(event);
  }, [cancelStreamingScroll, onPointerDownIntent, onSelectionPointerDown]);

  return { onWheelCapture, onTouchMoveCapture, onKeyDownCapture, onPointerDownCapture };
}
