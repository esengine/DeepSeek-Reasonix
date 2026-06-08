// Coalesces text/reasoning stream deltas into one flush per animation frame.
// Non-text events must drain() first so causal ordering is preserved.

type Flush<T> = (batch: T[]) => void;

interface BatchHandle<T> {
  push: (item: T) => void;
  drain: () => void;
  size: () => number;
}

export function createRafBatch<T>(flush: Flush<T>): BatchHandle<T> {
  let buffer: T[] = [];
  let scheduled: number | null = null;

  const run = () => {
    scheduled = null;
    // Snapshot + clear before flushing so a re-entrant push() lands next frame.
    const out = buffer;
    buffer = [];
    if (out.length > 0) flush(out);
  };

  const handle: BatchHandle<T> = {
    push(item: T) {
      buffer.push(item);
      if (scheduled === null && typeof requestAnimationFrame !== "undefined") {
        scheduled = requestAnimationFrame(run);
      } else if (scheduled === null) {
        // No rAF (SSR / JSDOM) — fall back to a microtask.
        scheduled = 1;
        Promise.resolve().then(run);
      }
    },
    drain() {
      if (scheduled !== null) {
        if (typeof cancelAnimationFrame !== "undefined" && scheduled !== 1) {
          cancelAnimationFrame(scheduled);
        }
        scheduled = null;
      }
      run();
    },
    size() {
      return buffer.length;
    },
  };
  return handle;
}
