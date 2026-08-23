// Run: tsx src/__tests__/transcript-reader-extent-race.test.tsx

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

console.log("\ntranscript reader extent races");

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

async function flushFrames() {
  const pending = [...frames.values()];
  frames.clear();
  await act(async () => pending.forEach((callback) => callback(performance.now())));
}

const scrollWrites: TranscriptScrollWriteRecord[] = [];
dom.window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => { scrollWrites.push(write); };

const rectAt = (top: number) => ({
  top,
  bottom: top + 100,
  height: 100,
  left: 0,
  right: 800,
  width: 800,
  x: 0,
  y: top,
  toJSON: () => ({}),
});
const scrollElement = dom.window.document.getElementById("scroll") as HTMLDivElement;
const rowElement = scrollElement.querySelector<HTMLElement>(".transcript__row")!;
scrollElement.getBoundingClientRect = () => rectAt(0);
rowElement.getBoundingClientRect = () => rectAt(20);
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 725 });
let scrollExtent = 15_829;
Object.defineProperty(scrollElement, "scrollHeight", { configurable: true, get: () => scrollExtent });
Object.defineProperty(scrollElement, "scrollTop", { configurable: true, writable: true, value: 14_567.47 });

let scrollByCalls = 0;
let lastScrollByTop = 0;
const virtuosoHandle = {
  scrollBy: (options?: { top?: number }) => {
    scrollByCalls += 1;
    lastScrollByTop = options?.top ?? 0;
    scrollElement.scrollTop += lastScrollByTop;
  },
  scrollTo: () => {},
  scrollToIndex: () => {},
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

// Composer wrap shrinks the in-flow viewport. Tail-follow must pin the native
// tail synchronously so jump-bottom cannot flash before the coalesced frame.
await act(async () => arbiter?.reset());
scrollExtent = 500;
scrollElement.scrollTop = 400;
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 100 });
await act(async () => arbiter?.followGrowingTail());
await flushFrames();
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 80 });
await act(async () => arbiter?.followGrowingTail());
check(scrollElement.scrollTop === 420, "footer-driven viewport shrink pins the native tail before rAF");
await act(async () => arbiter?.deliverScroll());
check(arbiter?.isAtBottom === true, "tail-follow keeps isAtBottom through a composer-wrap gap");
await flushFrames();
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 725 });
scrollExtent = 15_829;
scrollElement.scrollTop = 14_567.47;

// Returned Windows geometry: the native extent collapses after a downward
// wheel and rebounds while scrollTop remains clamped 1,949px too high.
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
scrollWrites.length = 0;
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 133.33,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
scrollExtent = 13_344;
scrollElement.scrollTop = 12_618.67;
rowElement.getBoundingClientRect = () => rectAt(1_836);
await act(async () => arbiter?.deliverScroll());
check(arbiter?.modeRef.current === "manual",
  "a transient physical-bottom clamp cannot claim tail ownership");
await act(async () => arbiter?.finishProgrammaticScroll());
await act(async () => arbiter?.followGrowingTail());
await flushFrames();
check(scrollByCalls === 0, "the transaction waits while the native extent remains collapsed");
scrollExtent = 15_829;
await act(async () => arbiter?.followGrowingTail());
await flushFrames();
check(scrollByCalls === 1 && lastScrollByTop > 1_900,
  `the rebound restores the logical anchor exactly once (${lastScrollByTop}px)`);
check(scrollWrites.length === 1 && scrollWrites[0].owner === "reader-stability",
  "the correction is owned by reader stability rather than recovery or tail-follow");
check(arbiter?.modeRef.current === "manual", "the correction preserves manual reader ownership");

// Touch movement is incremental: the second touchmove protects only its own
// segment rather than replaying the distance from the original touchstart.
await act(async () => arbiter?.reset());
scrollExtent = 5_000;
scrollElement.scrollTop = 2_000;
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
await act(async () => arbiter?.onTouchStartIntent({
  touches: [{ clientY: 100 }],
} as unknown as React.TouchEvent<HTMLElement>));
await act(async () => arbiter?.onTouchMoveIntent({
  touches: [{ clientY: 90 }],
} as unknown as React.TouchEvent<HTMLElement>));
scrollElement.scrollTop = 2_010;
await act(async () => arbiter?.onTouchMoveIntent({
  touches: [{ clientY: 80 }],
} as unknown as React.TouchEvent<HTMLElement>));
scrollExtent = 4_000;
scrollElement.scrollTop = 1_000;
rowElement.remove();
await act(async () => arbiter?.deliverScroll());
scrollExtent = 5_000;
scrollByCalls = 0;
lastScrollByTop = 0;
await flushFrames();
check(scrollByCalls === 1 && lastScrollByTop === 1_020,
  `consecutive touch segments use incremental geometry (${lastScrollByTop}px)`);
scrollElement.append(rowElement);

// Ordinary sub-viewport measurement jitter stays browser-owned, and a higher
// priority selection cancels the still-pending transaction.
await act(async () => arbiter?.reset());
scrollExtent = 5_000;
scrollElement.scrollTop = 2_000;
rowElement.getBoundingClientRect = () => rectAt(20);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
scrollWrites.length = 0;
scrollByCalls = 0;
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 133.33,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
scrollElement.scrollTop = 1_960;
rowElement.getBoundingClientRect = () => rectAt(60);
await act(async () => arbiter?.followGrowingTail());
await flushFrames();
check(scrollByCalls === 0 && scrollWrites.length === 0,
  "sub-viewport reverse jitter never earns a correction");
await act(async () => arbiter?.setMode("selection", "test-reader-stability-preemption"));
scrollElement.scrollTop = 1_000;
rowElement.getBoundingClientRect = () => rectAt(1_060);
await act(async () => arbiter?.followGrowingTail());
await flushFrames();
check(scrollByCalls === 0 && scrollWrites.length === 0,
  "selection ownership cancels a pending reader transaction");

await act(async () => root.unmount());
dom.window.close();

if (failed > 0) {
  console.error(`\n${failed} transcript reader extent race test(s) failed; ${passed} passed.`);
  process.exit(1);
}
console.log(`\n${passed} transcript reader extent race tests passed.`);
