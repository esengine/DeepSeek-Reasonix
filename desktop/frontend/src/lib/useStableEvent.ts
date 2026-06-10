import { useCallback, useLayoutEffect, useRef } from "react";

// Stable function identity with fresh implementation. Useful for DOM listeners,
// rAF callbacks, and memoized children that should not rerender just because an
// inline handler closed over newer state.
type AnyFn = (...args: any[]) => any;

export function useStableEvent<T extends AnyFn>(fn: T): T {
  const ref = useRef(fn);
  useLayoutEffect(() => {
    ref.current = fn;
  });
  return useCallback(((...args: Parameters<T>) => ref.current(...args)) as T, []);
}
