import { useCallback, useRef, useSyncExternalStore } from "react";
import type { TranscriptKernel } from "./transcriptKernel";
import type { TranscriptWindowDirection } from "./transcriptWindowRange";

type NativeViewportSnapshot = {
  scrollTop: number;
  clientHeight: number;
  scrollHeight: number;
  direction: TranscriptWindowDirection;
};

export function useNativeViewportSnapshot(element: HTMLElement | null, kernel: Pick<TranscriptKernel, "generation">): NativeViewportSnapshot {
  const cachedRef = useRef<NativeViewportSnapshot>({ scrollTop: 0, clientHeight: 0, scrollHeight: 0, direction: null });
  const getSnapshot = useCallback(() => {
    const scrollTop = element?.scrollTop ?? 0;
    const clientHeight = element?.clientHeight ?? 0;
    const scrollHeight = element?.scrollHeight ?? 0;
    const cached = cachedRef.current;
    if (Object.is(cached.scrollTop, scrollTop) && Object.is(cached.clientHeight, clientHeight) && Object.is(cached.scrollHeight, scrollHeight)) return cached;
    const direction = scrollTop > cached.scrollTop ? "forward" : scrollTop < cached.scrollTop ? "backward" : cached.direction;
    cachedRef.current = { scrollTop, clientHeight, scrollHeight, direction };
    return cachedRef.current;
  }, [element]);
  const generation = kernel.generation;
  const subscribe = useCallback((notify: () => void) => {
    if (!element) return () => {};
    let active = true;
    const handleChange = () => { if (active && generation === kernel.generation) notify(); };
    element.addEventListener("scroll", handleChange, { passive: true });
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(handleChange);
    observer?.observe(element);
    return () => {
      active = false;
      element.removeEventListener("scroll", handleChange);
      observer?.disconnect();
    };
  }, [element, generation, kernel]);
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}

