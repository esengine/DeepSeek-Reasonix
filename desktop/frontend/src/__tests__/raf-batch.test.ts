// Run: tsx src/__tests__/raf-batch.test.ts
// (sandbox note: `node --import tsx src/__tests__/raf-batch.test.ts`)
//
// Plain Node has no requestAnimationFrame, so the module's microtask fallback
// is exercised first; then a mocked rAF/cancelAnimationFrame pair is installed
// on globalThis to cover the frame-scheduled path deterministically.

import { createRafBatch } from "../lib/rafBatch";

let passed = 0;
let failed = 0;

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

const tick = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

console.log("\nraf batch");

// --- microtask fallback path (no rAF in plain node) ---
{
  const flushes: number[][] = [];
  const batch = createRafBatch<number>((out) => flushes.push([...out]));

  eq(batch.size(), 0, "starts with an empty buffer");
  batch.push(1);
  batch.push(2);
  batch.push(3);
  eq(batch.size(), 3, "push accumulates items before the flush");
  await tick();
  eq(flushes.length, 1, "fallback flushes exactly once per batch");
  eq(flushes[0].join(","), "1,2,3", "fallback flush delivers all buffered items in order");
  eq(batch.size(), 0, "buffer is drained after the flush");
  await tick();
  eq(flushes.length, 1, "no second flush without new pushes");
}

// re-entrant push during the flush lands in the next flush
{
  const flushes: string[][] = [];
  let batch: ReturnType<typeof createRafBatch<string>>;
  batch = createRafBatch<string>((out) => {
    flushes.push([...out]);
    if (flushes.length === 1) batch.push("re-entrant");
  });
  batch.push("a");
  await tick();
  eq(flushes.length, 2, "re-entrant push triggers a second fallback flush");
  eq(flushes[0].join(","), "a", "first flush excludes the re-entrant item");
  eq(flushes[1].join(","), "re-entrant", "re-entrant item flushes in its own batch");
  await tick();
  eq(flushes.length, 2, "no further flushes after the re-entrant batch");
}

// drain: synchronous flush, empty-buffer no-op, stale microtask cannot double-flush
{
  const flushes: number[][] = [];
  const batch = createRafBatch<number>((out) => flushes.push([...out]));

  batch.push(1);
  batch.push(2);
  batch.drain();
  eq(flushes.length, 1, "drain flushes synchronously");
  eq(flushes[0].join(","), "1,2", "drain flushes buffered items");
  eq(batch.size(), 0, "drain empties the buffer");
  batch.drain();
  eq(flushes.length, 1, "drain on an empty buffer does not flush");
  batch.push(3);
  batch.drain();
  eq(flushes.length, 2, "drain flushes a newly pushed item immediately");
  await tick();
  eq(flushes.length, 2, "stale microtask from the pre-drain push does not double-flush");
}

// --- mocked requestAnimationFrame path ---
{
  type FrameCallback = (time: number) => void;
  const frames: FrameCallback[] = [];
  let nextId = 1;
  let rafCalls = 0;
  let cancelCalls = 0;
  globalThis.requestAnimationFrame = ((cb: FrameCallback) => {
    rafCalls += 1;
    const id = nextId++;
    frames.push(cb);
    return id;
  }) as typeof requestAnimationFrame;
  globalThis.cancelAnimationFrame = ((id: number) => {
    cancelCalls += 1;
    void id;
  }) as typeof cancelAnimationFrame;

  const flushes: number[][] = [];
  const batch = createRafBatch<number>((out) => flushes.push([...out]));

  batch.push(1);
  eq(rafCalls, 1, "first push schedules one rAF");
  batch.push(2);
  batch.push(3);
  eq(rafCalls, 1, "later pushes coalesce into the already-scheduled rAF");
  eq(batch.size(), 3, "buffer holds items until the frame fires");
  frames.shift()!(16);
  eq(flushes.length, 1, "rAF callback flushes once");
  eq(flushes[0].join(","), "1,2,3", "rAF flush delivers all buffered items in order");
  eq(batch.size(), 0, "buffer is empty after the rAF flush");
  frames.shift()?.(16);
  eq(flushes.length, 1, "firing an extra frame does not flush an empty buffer");

  // re-entrant push during an rAF flush schedules the next frame
  const reflushes: string[][] = [];
  let rebatch: ReturnType<typeof createRafBatch<string>>;
  rebatch = createRafBatch<string>((out) => {
    reflushes.push([...out]);
    if (reflushes.length === 1) rebatch.push("re");
  });
  rebatch.push("x");
  eq(rafCalls, 2, "second batch schedules its own rAF");
  frames.shift()!(16);
  eq(reflushes.length, 1, "first rAF flush fires");
  eq(reflushes[0].join(","), "x", "first flush excludes the re-entrant item");
  eq(rafCalls, 3, "re-entrant push schedules a fresh rAF for the next frame");
  frames.shift()!(16);
  eq(reflushes.length, 2, "second rAF flush fires");
  eq(reflushes[1].join(","), "re", "re-entrant item flushes on the next frame");

  // drain cancels a pending rAF and flushes synchronously
  const drainFlushes: number[][] = [];
  const drainBatch = createRafBatch<number>((out) => drainFlushes.push([...out]));
  drainBatch.push(7);
  eq(rafCalls, 4, "push before drain schedules an rAF");
  drainBatch.drain();
  eq(cancelCalls, 1, "drain cancels the pending rAF");
  eq(drainFlushes.length, 1, "drain flushes synchronously even in rAF mode");
  eq(drainFlushes[0].join(","), "7", "drain delivers the buffered item");
  frames.shift()?.(16);
  eq(drainFlushes.length, 1, "firing the canceled frame does not double-flush");
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
