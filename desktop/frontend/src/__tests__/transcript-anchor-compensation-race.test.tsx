// Run: tsx src/__tests__/transcript-anchor-compensation-race.test.tsx
//
// W1 scroll-policy races, split out of transcript-recovery-race.test.tsx
// (800-line test-file ceiling): the bottom-hold tail-follow re-entry policy
// (#8709/#9099) and the steady-state manual-mode anchor compensation
// (#8438/#8488/#8897). Same JSDOM + fake rAF/clock harness with a stubbed
// VirtuosoHandle as the recovery race file.

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import type { VirtuosoHandle } from "react-virtuoso";
import type { TranscriptScrollWriteRecord } from "../lib/transcriptScrollProbe";
import { useTranscriptScrollArbiter } from "../lib/useTranscriptScrollArbiter";

let passed = 0;
let failed = 0;

function check(condition: unknown, label: string) {
  if (condition) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\ntranscript bottom-hold and anchor compensation races");

const dom = new JSDOM('<!doctype html><html><body><div id="root"></div><div id="scroll"><div class="transcript__row" data-row-key="row-a"></div></div></body></html>', {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Element = dom.window.Element;
globalThis.Node = dom.window.Node;

let nextFrame = 1;
const frames = new Map<number, FrameRequestCallback>();
const requestFrame = (callback: FrameRequestCallback) => {
  const id = nextFrame;
  nextFrame += 1;
  frames.set(id, callback);
  return id;
};
const cancelFrame = (id: number) => void frames.delete(id);
globalThis.requestAnimationFrame = requestFrame;
globalThis.cancelAnimationFrame = cancelFrame;
dom.window.requestAnimationFrame = requestFrame;
dom.window.cancelAnimationFrame = cancelFrame;

let clockNow = 10_000;
let nextTimer = 1;
const timers = new Map<number, { dueAt: number; run: () => void }>();
const originalDateNow = Date.now;
const originalSetTimeout = dom.window.setTimeout;
const originalClearTimeout = dom.window.clearTimeout;
Date.now = () => clockNow;
dom.window.setTimeout = ((handler: TimerHandler, timeout = 0, ...args: unknown[]) => {
  const id = nextTimer;
  nextTimer += 1;
  const run = typeof handler === "function"
    ? () => handler(...args)
    : () => { throw new Error("string timer handlers are unsupported in this test"); };
  timers.set(id, { dueAt: clockNow + Math.max(0, timeout), run });
  return id;
}) as typeof dom.window.setTimeout;
dom.window.clearTimeout = ((id: number | undefined) => {
  if (id !== undefined) timers.delete(id);
}) as typeof dom.window.clearTimeout;

async function advanceClock(milliseconds: number) {
  await act(async () => {
    const target = clockNow + milliseconds;
    while (true) {
      const next = [...timers.entries()]
        .filter(([, timer]) => timer.dueAt <= target)
        .sort(([leftID, left], [rightID, right]) => left.dueAt - right.dueAt || leftID - rightID)[0];
      if (!next) break;
      const [id, timer] = next;
      timers.delete(id);
      clockNow = timer.dueAt;
      timer.run();
    }
    clockNow = target;
  });
}

async function flushFrames() {
  const pending = [...frames.entries()];
  frames.clear();
  await act(async () => pending.forEach(([, callback]) => callback(performance.now())));
}

// Runtime capture of every imperative scroll write (Phase 0 probe).
const scrollWrites: TranscriptScrollWriteRecord[] = [];
dom.window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => { scrollWrites.push(write); };

const rectAt = (top: number) => ({ top, bottom: top + 100, height: 100, left: 0, right: 800, width: 800, x: 0, y: top, toJSON: () => ({}) });

const scrollElement = dom.window.document.getElementById("scroll") as HTMLDivElement;
const rowElement = scrollElement.querySelector<HTMLElement>(".transcript__row")!;
scrollElement.getBoundingClientRect = () => rectAt(0);
rowElement.getBoundingClientRect = () => rectAt(200);
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 100 });
let scrollExtent = 500;
Object.defineProperty(scrollElement, "scrollHeight", { configurable: true, get: () => scrollExtent });
Object.defineProperty(scrollElement, "scrollTop", { configurable: true, writable: true, value: 0 });
Object.defineProperty(scrollElement, "offsetWidth", { configurable: true, value: 800 });
Object.defineProperty(scrollElement, "clientWidth", { configurable: true, value: 780 });
Object.defineProperty(scrollElement, "clientLeft", { configurable: true, value: 0 });

const virtuosoHandle = {
  scrollBy: () => {},
  scrollToIndex: () => {},
  // Browser semantics: an offset write clamps against the current extent.
  scrollTo: (options?: { top?: number }) => {
    const top = options?.top ?? 0;
    scrollElement.scrollTop = Math.max(0, Math.min(scrollExtent - scrollElement.clientHeight, top));
  },
  getState: () => {},
} as unknown as VirtuosoHandle;
let arbiter: ReturnType<typeof useTranscriptScrollArbiter> | undefined;

function Probe() {
  arbiter = useTranscriptScrollArbiter();
  return null;
}

const root = createRoot(dom.window.document.getElementById("root")!);
await act(async () => root.render(<Probe />));
await act(async () => {
  (arbiter!.virtuosoRef as { current: VirtuosoHandle | null }).current = virtuosoHandle;
  arbiter!.scrollerRef(scrollElement);
});

const wheel = async (deltaY: number) => act(async () => {
  arbiter?.onWheelIntent({
    ctrlKey: false,
    deltaX: 0,
    deltaY,
    target: scrollElement,
  } as React.WheelEvent<HTMLElement>);
});
const wheelDown = () => wheel(40);

// ── Bottom-hold re-entry (#8709/#9099): auto re-entry into tail-follow
// requires the bottom to be HELD — two consecutive at-bottom deliveries inside
// one reader-intent window with no upward gesture in between. A single
// touch-down claims the gesture but stays manual.
scrollElement.scrollTop = 400;
await act(async () => arbiter?.releaseTailFollow());
await wheelDown();
check(arbiter?.modeRef.current === "manual", "a single downward touch-down stays manual");
await wheelDown();
check(arbiter?.modeRef.current === "tail-follow", "a held bottom (second delivery) re-enters tail-follow");

// An upward gesture between at-bottom deliveries breaks the hold streak; the
// next downward gesture restarts it from zero.
await act(async () => arbiter?.releaseTailFollow());
await wheelDown();
check(arbiter?.modeRef.current === "manual", "one at-bottom delivery starts the hold without re-entering");
await wheel(-40);
scrollElement.scrollTop = 300;
await act(async () => arbiter?.deliverScroll());
scrollElement.scrollTop = 400;
await wheelDown();
check(arbiter?.modeRef.current === "manual", "an upward gesture between at-bottom deliveries breaks the hold");
await wheelDown();
check(arbiter?.modeRef.current === "tail-follow", "two consecutive held deliveries after the reset re-enter tail-follow");

// The 180ms idle close performs one final native delivery so a large wheel or
// touch gesture that clamps at the physical bottom can complete the hold even
// when the browser emits no second scroll event. The completed transition
// still resets the streak before a fresh reader-intent window begins.
await act(async () => arbiter?.releaseTailFollow());
await wheelDown();
await advanceClock(200);
check(arbiter?.modeRef.current === "tail-follow", "idle close re-samples a held physical bottom before ending intent");
await act(async () => arbiter?.releaseTailFollow());
await wheelDown();
check(arbiter?.modeRef.current === "manual", "a fresh intent window rebuilds the bottom hold from zero");
await wheelDown();
check(arbiter?.modeRef.current === "tail-follow", "the fresh window re-enters tail-follow after its second delivery");

// A thumb gesture that reaches the frozen native bottom claims the tail when
// the drag's own deliveries already held the bottom before release resumes
// real row measurements and changes the extent.
await act(async () => arbiter?.reset());
scrollElement.scrollTop = 0;
await act(async () => arbiter?.onPointerDownIntent({
  button: 0,
  nativeEvent: { button: 0, clientX: 795 },
} as React.PointerEvent<HTMLElement>));
scrollElement.scrollTop = 400;
await act(async () => arbiter?.deliverScroll());
await act(async () => window.dispatchEvent(new dom.window.Event("pointerup")));
check(arbiter?.modeRef.current === "tail-follow", "native thumb release after a held physical bottom resumes tail-follow");
scrollExtent = 900;
await act(async () => arbiter?.deliverScroll());
await flushFrames();
await flushFrames();
await flushFrames();
check(scrollElement.scrollTop === 800, "post-release remeasurement reconverges the claimed native bottom");
scrollExtent = 500;

// ── Manual-mode viewport anchor compensation (#8438/#8488/#8897): an
// above-viewport height change (fold auto-collapse, history patch) must not
// push the reading position. The drift is measured against the anchor sampled
// on the last delivered scroll and corrected through exactly one arbiter-owned
// offset write; growth below the viewport and ownership changes earn none.
await act(async () => arbiter?.reset());
scrollElement.appendChild(rowElement);
// Real-geometry stub: the row's client position tracks scrollTop like a
// browser's would (document top 150).
rowElement.getBoundingClientRect = () => rectAt(150 - scrollElement.scrollTop);
scrollElement.scrollTop = 100;
await act(async () => arbiter?.setMode("manual", "test-anchor-compensation"));
await act(async () => arbiter?.deliverScroll());
scrollWrites.length = 0;
// A fold above the viewport expands: extent and the row both move +200.
scrollExtent = 700;
rowElement.getBoundingClientRect = () => rectAt(350 - scrollElement.scrollTop);
await act(async () => arbiter?.followGrowingTail());
await flushFrames(); // followGrowingTail frame: LAYOUT_HEIGHT_CHANGED + compensation scheduled
await flushFrames(); // compensation measures drift and writes once
await flushFrames(); // stable frame 1
await flushFrames(); // stable frame 2: done
const compensationWrites = scrollWrites.filter((write) => write.owner === "anchor-compensation");
check(compensationWrites.length === 1, `above-viewport growth in manual mode emits exactly one anchor-compensation write (${compensationWrites.length})`);
check(compensationWrites[0]?.top === 300 && scrollElement.scrollTop === 300, "the compensation restores the anchor row's viewport offset");
check(arbiter?.modeRef.current === "manual", "anchor compensation preserves manual reading ownership");
check(rowElement.getBoundingClientRect().top === 50, "the anchor row is physically back at its pre-change offset");

// Growth below the viewport (streaming tail) leaves the anchor row put:
// zero measured drift, zero writes.
await act(async () => arbiter?.deliverScroll());
scrollWrites.length = 0;
scrollExtent = 900;
await act(async () => arbiter?.followGrowingTail());
for (let i = 0; i < 4; i += 1) await flushFrames();
check(
  scrollWrites.filter((write) => write.owner === "anchor-compensation").length === 0,
  "below-viewport growth earns no compensation write",
);
check(scrollElement.scrollTop === 300, "below-viewport growth leaves the reading position untouched");

// A collapse above the viewport (CONTENT_SHRINK path) compensates upward.
scrollWrites.length = 0;
scrollExtent = 600;
rowElement.getBoundingClientRect = () => rectAt(150 - scrollElement.scrollTop);
await act(async () => arbiter?.followGrowingTail());
for (let i = 0; i < 4; i += 1) await flushFrames();
const shrinkWrites = scrollWrites.filter((write) => write.owner === "anchor-compensation");
check(shrinkWrites.length === 1 && shrinkWrites[0]?.top === 100, "an above-viewport collapse compensates upward exactly once");
check(scrollElement.scrollTop === 100, "the upward compensation restores the anchor offset");

// A user gesture mid-compensation cancels the loop: the reader owns the
// viewport from there on.
await act(async () => arbiter?.deliverScroll());
scrollWrites.length = 0;
scrollExtent = 800;
rowElement.getBoundingClientRect = () => rectAt(350 - scrollElement.scrollTop);
await act(async () => arbiter?.followGrowingTail());
await flushFrames(); // schedules the compensation
await act(async () => arbiter?.releaseTailFollow());
for (let i = 0; i < 4; i += 1) await flushFrames();
check(
  scrollWrites.filter((write) => write.owner === "anchor-compensation").length === 0,
  "user scroll intent cancels a pending anchor compensation",
);

await act(async () => root.unmount());
Date.now = originalDateNow;
dom.window.setTimeout = originalSetTimeout;
dom.window.clearTimeout = originalClearTimeout;
dom.window.close();

if (failed > 0) {
  console.error(`\n${failed} transcript anchor compensation race test(s) failed; ${passed} passed.`);
  process.exit(1);
}
console.log(`\n${passed} transcript anchor compensation race tests passed.`);
