// Run: tsx src/__tests__/transcript-tail-settle-stagnation.test.ts

import assert from "node:assert/strict";
import { createTranscriptTailSettle } from "../lib/transcriptTailSettle";
import type { TranscriptScrollWriteRecord } from "../lib/transcriptScrollProbe";

console.log("\ntranscript tail settle stagnant resend bound");

(globalThis as Record<string, unknown>).window = {
  setTimeout: (handler: TimerHandler, timeout?: number) => setTimeout(handler as () => void, timeout),
  clearTimeout: (id: number | undefined) => clearTimeout(id),
};
const frameQueue: Array<FrameRequestCallback> = [];
(globalThis as Record<string, unknown>).requestAnimationFrame = (callback: FrameRequestCallback) => {
  frameQueue.push(callback);
  return frameQueue.length;
};
(globalThis as Record<string, unknown>).cancelAnimationFrame = () => {};

type EngineRejects = boolean;

function runFixture(engineRejects: EngineRejects, afterRejectedWrite?: (element: { scrollTop: number; scrollHeight: number; clientHeight: number }) => void) {
  const element = {
    scrollTop: 863,
    scrollHeight: 1_000,
    clientHeight: 100,
    dataset: {},
    getBoundingClientRect: () => ({ top: 0, bottom: 100 }),
    querySelector: () => null,
    querySelectorAll: () => [],
  };
  const writes: TranscriptScrollWriteRecord[] = [];
  const layoutTransient = { current: false };
  const settle = createTranscriptTailSettle({
    writer: {
      write: (request) => {
        writes.push({ ...(request as unknown as TranscriptScrollWriteRecord), owner: "tail-follow", kind: request.operation });
        // An engine clamp/consume that leaves the physical offset unchanged
        // reproduces the doomed-resend regime.
        if (!engineRejects) element.scrollTop = request.top ?? element.scrollTop;
        else afterRejectedWrite?.(element);
        return true;
      },
      lastOwner: () => "tail-follow",
    },
    scrollRef: { current: element as unknown as HTMLDivElement },
    modeRef: { current: "tail-follow" as const },
    generationRef: { current: 7 },
    layoutTransientRef: layoutTransient,
    requestResidualGeometry: () => {},
  });

  const pumpRevision = () => {
    settle.schedule(9_000 + writes.length, false, "geometry-changed");
    while (frameQueue.length > 0) {
      const batch = frameQueue.splice(0);
      for (const callback of batch) callback(0);
    }
    // Settle writes land synchronously on the first tick; residual
    // verification arms real timers that this fixture intentionally ignores.
  };
  return { element, writes, settle, pumpRevision };
}

{
  const { element, writes, settle, pumpRevision } = runFixture(true, (current) => {
    // React Virtuoso may replace the physical range synchronously while the
    // native absolute write is being processed. The next geometry signal can
    // return to the exact same rejected request state.
    current.scrollHeight += 200;
  });
  for (let index = 0; index < 4; index += 1) {
    element.scrollTop = 863;
    element.scrollHeight = 1_000;
    pumpRevision();
  }
  assert.equal(writes.length, 4, "a repeating post-write extent cycle still reaches the bounded handoff");
  assert.equal(writes[writes.length - 1]?.kind, "scrollToIndex", "the repeating extent cycle mounts logical LAST");
  settle.cancel();
  console.log("  PASS  post-write range replacement cannot reset the stagnant handoff budget");
}

{
  const { writes, settle, pumpRevision } = runFixture(true);
  for (let index = 0; index < 4; index += 1) pumpRevision();
  assert.ok(writes.length === 4,
    `identical rejected resends hand off after three absolute writes (got ${writes.length as number})`);
  assert.equal(writes[writes.length - 1]?.kind, "scrollToIndex", "the bounded fallback mounts logical LAST");
  settle.cancel();
  console.log("  PASS  identical rejected resends hand off once to the bounded LAST transaction");
}

{
  const { element, writes, settle, pumpRevision } = runFixture(true);
  for (let index = 0; index < 3; index += 1) pumpRevision();
  assert.ok(writes.length === 3);
  element.scrollHeight += 30;
  pumpRevision();
  const rearmCount: number = writes.length;
  assert.ok(rearmCount === 4, `re-arm spends exactly one new write (got ${rearmCount})`);
  assert.ok(writes[rearmCount - 1]?.top === element.scrollHeight - element.clientHeight,
    "the re-armed write targets the grown extent");
  console.log("  PASS  a real extent change re-arms corrections past the stagnation bound");

  for (let index = 0; index < 3; index += 1) pumpRevision();
  const resumeCount: number = writes.length;
  assert.ok(resumeCount === 7,
    `the bounded budget resumes and hands off after re-arm (got ${resumeCount})`);
  assert.equal(writes[writes.length - 1]?.kind, "scrollToIndex");
  settle.cancel();
  console.log("  PASS  the bounded fallback budget resumes counting after re-arm");
}

{
  const { element, writes, pumpRevision } = runFixture(true);
  pumpRevision();
  pumpRevision();
  pumpRevision();
  assert.ok(writes.length === 3);
  element.scrollTop = 900;
  pumpRevision();
  assert.ok(writes.length === 3,
    "corrections a user gesture already satisfied spend no extra write");
  console.log("  PASS  corrections a user gesture already satisfied spend no extra write");
}

{
  const { writes, pumpRevision } = runFixture(false);
  for (let index = 0; index < 8; index += 1) pumpRevision();
  assert.ok(writes.length <= 2,
    `an honoring engine converges without repeat resends (got ${writes.length})`);
  console.log("  PASS  an honoring engine converges without repeat resends");
}

{
  const { element, writes, pumpRevision } = runFixture(false);
  // Reproduce WebKit retaining the old native top during a virtual-range
  // contraction. The resulting negative bottom distance must not be accepted
  // as a settled tail for even one rendering opportunity.
  element.scrollTop = 1_000;
  pumpRevision();
  assert.equal(writes.length, 1, "an out-of-range tail receives one writer-owned clamp");
  assert.equal(writes[0]?.top, 900, "the pre-paint clamp targets the real native bottom");
  assert.equal(element.scrollTop, 900, "the invalid native offset is corrected synchronously");
  console.log("  PASS  a contracted native range is clamped before its invalid tail can paint");
}

{
  const { element, writes, pumpRevision } = runFixture(true);
  const offscreenRow = {
    dataset: { rowKey: "stale-row", itemIndex: "998" },
    getBoundingClientRect: () => ({ top: 180, bottom: 220, height: 40 }),
  };
  element.dataset = { transcriptRowCount: "1", transcriptFirstItemIndex: "999" };
  (element as unknown as { querySelectorAll: () => unknown[] }).querySelectorAll = () => [offscreenRow];
  pumpRevision();
  assert.equal(writes.length, 1, "a pre-paint unmounted tail spends one bounded writer transaction");
  assert.equal(writes[0]?.kind, "scrollToIndex", "a pre-paint unmounted tail remounts logical LAST");
  console.log("  PASS  an unmounted tail range is remounted before it becomes a painted baseline");
}

console.log("transcript tail settle stagnation tests passed");
