// scheduleIdleTask — requestIdleCallback with a setTimeout fallback for
// engines/tests without it. Used for non-urgent rendering work (deferred
// syntax highlighting, progressive markdown block mounting).

type IdleWindow = Window & {
  requestIdleCallback?: (callback: () => void, options?: { timeout: number }) => number;
  cancelIdleCallback?: (handle: number) => void;
};

export function scheduleIdleTask(callback: () => void, timeoutMs = 1_000): () => void {
  const idleWindow = window as IdleWindow;
  if (idleWindow.requestIdleCallback) {
    const handle = idleWindow.requestIdleCallback(callback, { timeout: timeoutMs });
    return () => idleWindow.cancelIdleCallback?.(handle);
  }
  const timer = window.setTimeout(callback, Math.min(timeoutMs, 200));
  return () => window.clearTimeout(timer);
}
