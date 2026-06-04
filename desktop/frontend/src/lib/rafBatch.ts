// rafBatch coalesces repeated calls into one execution per animation frame. The
// desktop agent stream pushes a text/reasoning delta per Webview event; at 200
// tok/s that's a state update every ~5 ms, so a 16 ms rAF window can absorb 3–4
// deltas into a single React render. Non-text events (tool dispatch, usage,
// notice, turn boundaries) must NOT be batched — they have ordering with the
// text deltas (a "tool_dispatch" should appear after the text that announced
// it), and the controller flushes the buffer first to keep the causal order
// correct.
//
// We don't use a single "apply all buffered events" reducer action because the
// controller's reducer is pure and deterministic; instead we accept a flusher
// function that receives the accumulated list and emits a single dispatch. The
// flush happens synchronously at the start of the rAF callback, then the batch
// is cleared, so the next frame's deltas land in a fresh window.

type Flush<T> = (batch: T[]) => void;

interface BatchHandle<T> {
  push: (item: T) => void;
  drain: () => void;
  size: () => number;
}

export function createRafBatch<T>(flush: Flush<T>): BatchHandle<T> {
  // The pending buffer and the scheduled rAF id are kept in a closure rather than
  // refs so a single createRafBatch() can be shared by many callers without
  // each owning their own React-bound state. In the desktop frontend the
  // controller creates one batch and the event listener uses push() freely.
  let buffer: T[] = [];
  let scheduled: number | null = null;

  const run = () => {
    scheduled = null;
    // Snapshot then clear BEFORE invoking the flusher, so any re-entrant push()
    // (e.g. a useController dispatch that triggers another event) lands in the
    // next frame rather than the current one.
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
        // No rAF in this environment (e.g. SSR or a test using JSDOM without
        // the polyfill) — flush on a microtask so the controller still sees a
        // single dispatch per tick.
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
