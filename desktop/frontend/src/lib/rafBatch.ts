// Coalesces text/reasoning stream deltas into one flush per animation frame.
// Non-text events must drain() first so causal ordering is preserved.
//
// A best-effort timer backs up rAF when frame callbacks pause but the JS task
// queue still runs, as can happen in a throttled or occluded WebView. It is not
// a main-thread watchdog: browser timer throttling and long tasks can delay
// both callbacks. Whichever callback runs first flushes and cancels the other,
// preserving one-flush-per-frame behavior while frames are being produced.

type Flush<T> = (batch: T[]) => void;

interface BatchHandle<T> {
  push: (item: T) => void;
  drain: () => void;
  size: () => number;
}

// Request the fallback after 200ms. Timers are minimum-delay only, so a
// throttled WebView or blocked task queue can deliver it later.
const STALL_TIMEOUT_MS = 200;

export function createRafBatch<T>(flush: Flush<T>): BatchHandle<T> {
  let buffer: T[] = [];
  let scheduled: number | null = null; // rAF id; 1 = microtask fallback (no rAF)
  let stallTimer: ReturnType<typeof setTimeout> | null = null;

  const clearScheduled = () => {
    if (scheduled !== null && scheduled !== 1 && typeof cancelAnimationFrame !== "undefined") {
      cancelAnimationFrame(scheduled);
    }
    scheduled = null;
  };

  const clearStallTimer = () => {
    if (stallTimer !== null) {
      clearTimeout(stallTimer);
      stallTimer = null;
    }
  };

  const run = () => {
    clearScheduled();
    clearStallTimer();
    // Snapshot + clear before flushing so a re-entrant push() lands next frame.
    const out = buffer;
    buffer = [];
    if (out.length > 0) flush(out);
  };

  const arm = () => {
    if (scheduled === null && typeof requestAnimationFrame !== "undefined") {
      scheduled = requestAnimationFrame(run);
    } else if (scheduled === null) {
      // No rAF (SSR / JSDOM) — fall back to a microtask.
      scheduled = 1;
      Promise.resolve().then(run);
    }
    if (stallTimer === null && typeof setTimeout !== "undefined") {
      stallTimer = setTimeout(run, STALL_TIMEOUT_MS);
    }
  };

  const handle: BatchHandle<T> = {
    push(item: T) {
      buffer.push(item);
      if (scheduled === null) arm();
    },
    drain() {
      clearScheduled();
      clearStallTimer();
      run();
    },
    size() {
      return buffer.length;
    },
  };
  return handle;
}
