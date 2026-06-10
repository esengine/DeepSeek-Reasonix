import { useCallback, useEffect, useRef } from "react";
import { useStableEvent } from "./useStableEvent";

type AnyVoidFn = (...args: any[]) => void;

export function useRafThrottle<T extends AnyVoidFn>(fn: T): T {
  const latest = useStableEvent(fn);
  const frameRef = useRef<number | null>(null);
  const argsRef = useRef<Parameters<T> | null>(null);

  useEffect(() => {
    return () => {
      if (frameRef.current !== null) {
        window.cancelAnimationFrame(frameRef.current);
        frameRef.current = null;
      }
    };
  }, []);

  return useCallback(((...args: Parameters<T>) => {
    argsRef.current = args;
    if (frameRef.current !== null) return;
    frameRef.current = window.requestAnimationFrame(() => {
      frameRef.current = null;
      const nextArgs = argsRef.current;
      argsRef.current = null;
      if (nextArgs) latest(...nextArgs);
    });
  }) as T, [latest]);
}
