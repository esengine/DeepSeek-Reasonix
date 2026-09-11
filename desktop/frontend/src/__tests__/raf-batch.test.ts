// Run: tsx src/__tests__/raf-batch.test.ts
//
// rafBatch must flush coalesced deltas once per animation frame in the visible
// path and request a best-effort timer flush when rAF stops while the JS task
// queue still runs. The fake scheduler verifies the requested 200ms ordering;
// browser throttling or a blocked main thread may deliver either callback later.

import { createRafBatch } from "../lib/rafBatch";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}
`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}
`);
    failed += 1;
  }
}

type RafCb = () => void;

// Fake scheduler: rAF callbacks fire only on frame(), timers only on
// advance(), so a stalled-rAF scenario is fully controllable.
class Scheduler {
  rafQueue = new Map<number, RafCb>();
  timers = new Map<number, { at: number; cb: RafCb }>();
  now = 0;
  nextId = 1;

  install() {
    const s = this;
    (globalThis as Record<string, unknown>).requestAnimationFrame = (cb: RafCb) => {
      const id = s.nextId++;
      s.rafQueue.set(id, cb);
      return id;
    };
    (globalThis as Record<string, unknown>).cancelAnimationFrame = (id: number) => {
      s.rafQueue.delete(id);
    };
    (globalThis as Record<string, unknown>).setTimeout = (cb: RafCb, ms?: number) => {
      const id = s.nextId++;
      s.timers.set(id, { at: s.now + (ms ?? 0), cb });
      return id;
    };
    (globalThis as Record<string, unknown>).clearTimeout = (id: number) => {
      s.timers.delete(id);
    };
  }

  uninstall() {
    const g = globalThis as Record<string, unknown>;
    delete g.requestAnimationFrame;
    delete g.cancelAnimationFrame;
    delete g.setTimeout;
    delete g.clearTimeout;
  }

  uninstallRafOnly() {
    const g = globalThis as Record<string, unknown>;
    delete g.requestAnimationFrame;
    delete g.cancelAnimationFrame;
  }

  frame() {
    const pending = [...this.rafQueue.keys()];
    for (const id of pending) {
      const cb = this.rafQueue.get(id);
      this.rafQueue.delete(id);
      cb?.();
    }
  }

  advance(ms: number) {
    const target = this.now + ms;
    for (;;) {
      const due = [...this.timers.entries()]
        .filter(([, t]) => t.at <= target)
        .sort((a, b) => a[1].at - b[1].at);
      if (due.length === 0) break;
      const [id, t] = due[0];
      this.timers.delete(id);
      this.now = t.at;
      t.cb();
    }
    this.now = target;
  }
}

const sched = new Scheduler();
sched.install();

// --- one flush per animation frame in the visible path ---
{
  const flushed: string[][] = [];
  const batch = createRafBatch<string>((out) => flushed.push(out));
  batch.push("a");
  batch.push("b");
  eq(flushed.length, 0, "deltas wait for a frame (rAF still scheduled)");
  sched.frame();
  eq(flushed.length, 1, "one frame produces exactly one flush");
  eq(JSON.stringify(flushed[0]), JSON.stringify(["a", "b"]), "same-frame deltas coalesce into one batch");
  batch.push("c");
  sched.frame();
  eq(flushed.length, 2, "next frame flushes the next batch");
  eq(JSON.stringify(flushed[1]), JSON.stringify(["c"]), "subsequent push lands in its own batch");
  // The stall timer must have been cancelled by the rAF flushes.
  sched.advance(1000);
  eq(flushed.length, 2, "advancing time does not double-flush after rAF flushes");
}

// --- rAF stalls: the stall timer flushes after STALL_TIMEOUT_MS ---
{
  const flushed: string[][] = [];
  const batch = createRafBatch<string>((out) => flushed.push(out));
  batch.push("thinking");
  batch.push("chunk");
  eq(flushed.length, 0, "nothing flushes while rAF is stalled");
  sched.advance(199);
  eq(flushed.length, 0, "still nothing before the stall timeout");
  sched.advance(1);
  eq(flushed.length, 1, "stall timer flushes the accumulated deltas");
  eq(JSON.stringify(flushed[0]), JSON.stringify(["thinking", "chunk"]), "stall flush delivers everything buffered");
  sched.advance(1000);
  eq(flushed.length, 1, "stall flush is a one-shot; no repeated flushes");
}

// --- drain() flushes immediately and cancels the pending stall timer ---
{
  const flushed: string[][] = [];
  const batch = createRafBatch<string>((out) => flushed.push(out));
  batch.push("x");
  batch.drain();
  eq(flushed.length, 1, "drain flushes immediately");
  eq(JSON.stringify(flushed[0]), JSON.stringify(["x"]), "drain delivers the buffered delta");
  eq(batch.size(), 0, "buffer is empty after drain");
  sched.advance(1000);
  eq(flushed.length, 1, "drain cancels rAF and the stall timer");
}

// --- re-entrant push() during flush lands in the next batch ---
{
  const flushed: string[][] = [];
  let nextRef: { push: (v: string) => void } | undefined;
  const batch = createRafBatch<string>((out) => {
    flushed.push(out);
    if (out[0] === "first" && nextRef) nextRef.push("reentrant");
  });
  nextRef = batch;
  batch.push("first");
  sched.frame();
  eq(flushed.length, 1, "first flush ran");
  sched.frame();
  eq(flushed.length, 2, "re-entrant push was flushed on the next frame");
  eq(JSON.stringify(flushed[1]), JSON.stringify(["reentrant"]), "re-entrant delta coalesces into its own batch");
}

// --- microtask fallback still works without rAF (SSR / JSDOM) ---
{
  sched.uninstallRafOnly(); // keep fake timers; only rAF is absent
  const flushed: string[][] = [];
  const batch = createRafBatch<string>((out) => flushed.push(out));
  batch.push("y");
  await Promise.resolve();
  await Promise.resolve();
  eq(flushed.length, 1, "microtask fallback flushes without rAF");
  sched.install();
}

process.stdout.write(`
${passed} passed, ${failed} failed
`);
if (failed > 0) process.exit(1);
