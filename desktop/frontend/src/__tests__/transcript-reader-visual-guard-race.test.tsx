// Run: tsx src/__tests__/transcript-reader-visual-guard-race.test.tsx
//
// Visual-guard races split out of transcript-reader-extent-race.test.tsx
// (800-line test-file ceiling). Under prefers-reduced-motion, Windows/WebView2
// lets the guard transform lag behind its same-frame write, and another guard
// owner can drop the shared attribute. The reader guard must derive the
// physical drift from the transform the browser actually applied, never
// compounding 681 → 1362 → 2043. Same JSDOM + fake rAF harness with a stubbed
// VirtuosoHandle as the extent race file.

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import type { VirtuosoHandle } from "react-virtuoso";
import { setTranscriptScrollDiagnosticSink, type TranscriptScrollWriteRecord } from "../lib/transcriptScrollProbe";
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

console.log("\ntranscript reader visual guard races");

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
rowElement.dataset.index = "0";
scrollElement.getBoundingClientRect = () => rectAt(0);
rowElement.getBoundingClientRect = () => rectAt(12);
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 725 });
let scrollExtent = 23_806;
Object.defineProperty(scrollElement, "scrollHeight", { configurable: true, get: () => scrollExtent });
Object.defineProperty(scrollElement, "scrollTop", { configurable: true, writable: true, value: 22_608 });

let scrollByCalls = 0;
let lastScrollByTop = 0;
const virtuosoHandle = {
  scrollBy: (options?: { top?: number }) => {
    scrollByCalls += 1;
    lastScrollByTop = options?.top ?? 0;
    scrollElement.scrollTop += lastScrollByTop;
  },
  scrollTo: (options?: { top?: number }) => {
    scrollElement.scrollTop = options?.top ?? scrollElement.scrollTop;
  },
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

const visualOffsetOf = () => Number.parseFloat(
  scrollElement.style.getPropertyValue("--transcript-reader-visual-offset"),
) || 0;
const itemList = dom.window.document.createElement("div");
itemList.dataset.testid = "virtuoso-item-list";
itemList.style.transform = "none";
scrollElement.append(itemList);

// A downward wheel gesture whose same-scrollTop estimate growth displaces the
// anchor row by 681px on screen. The row rect deliberately ignores the guard
// CSS variable: the transform has not been applied by the browser yet.
const startDisplacedTransaction = async () => {
  await act(async () => arbiter?.reset());
  scrollExtent = 23_806;
  scrollElement.scrollTop = 22_608;
  rowElement.getBoundingClientRect = () => rectAt(12);
  await act(async () => arbiter?.deliverScroll());
  await act(async () => arbiter?.releaseTailFollow());
  await act(async () => arbiter?.onWheelIntent({
    ctrlKey: false,
    deltaMode: 0,
    deltaX: 0,
    deltaY: 24,
    target: scrollElement,
  } as React.WheelEvent<HTMLElement>));
  scrollByCalls = 0;
  scrollWrites.length = 0;
  scrollExtent += 681;
  rowElement.getBoundingClientRect = () => rectAt(693 - (scrollElement.scrollTop - 22_608));
};

await startDisplacedTransaction();
await act(async () => arbiter?.deliverScroll());
check(Math.abs(visualOffsetOf() + 681) <= 1,
  `an unapplied guard is written once from the physical drift (${visualOffsetOf()}px)`);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.deliverScroll());
check(Math.abs(visualOffsetOf() + 681) <= 1,
  `repeated observations before the transform lands do not compound the guard (${visualOffsetOf()}px)`);
await flushFrames();
check(scrollByCalls === 1 && Math.abs(lastScrollByTop - 681) <= 1,
  `the correction targets the physical anchor, not a compounded guard (${lastScrollByTop}px)`);
check(scrollElement.dataset.transcriptReaderVisualGuard === undefined,
  "the unapplied guard releases after the anchor is physically restored");
check(
  scrollWrites.filter((write) => write.owner === "reader-stability" && write.kind === "scrollBy").length === 1,
  "the unapplied-guard correction stays inside the reader writer lane",
);

// The applied transform is the truth even when the remembered offset is gone:
// a mounted item list carrying the guard transform must still be subtracted.
const syncItemListTransform = () => {
  const applied = scrollElement.dataset.transcriptReaderVisualGuard === "true" ? visualOffsetOf() : 0;
  itemList.style.transform = applied === 0 ? "none" : `matrix(1, 0, 0, 1, 0, ${applied})`;
};
await startDisplacedTransaction();
rowElement.getBoundingClientRect = () => rectAt(693 - (scrollElement.scrollTop - 22_608) + (
  Number.parseFloat(itemList.style.transform.split(",")[5]) || 0
));
await act(async () => arbiter?.deliverScroll());
syncItemListTransform();
check(Math.abs(visualOffsetOf() + 681) <= 1,
  `an applied guard is written from the physical drift (${visualOffsetOf()}px)`);
await act(async () => arbiter?.deliverScroll());
syncItemListTransform();
check(Math.abs(visualOffsetOf() + 681) <= 1,
  `an applied guard stays put across observations (${visualOffsetOf()}px)`);
await flushFrames();
syncItemListTransform();
check(scrollByCalls === 1 && Math.abs(lastScrollByTop - 681) <= 1,
  `the correction subtracts the applied transform (${lastScrollByTop}px)`);
await flushFrames();
syncItemListTransform();
check(scrollElement.dataset.transcriptReaderVisualGuard === undefined,
  "the applied guard releases once the correction lands");
itemList.style.transform = "none";

// Field #9711 (d9cd713, Windows, all rows mounted): the reader scrolls up
// inside a long Markdown answer whose row starts above the viewport. The
// answer's block window prepends 7,252px of older blocks inside that row and
// compensates scrollTop by the same amount, so visible content does not move.
// The anchor row's top edge is now 7,252px higher relative to the viewport
// and scrollTop moved against the reader. Neither is a displacement of what
// the reader sees: the transaction must absorb the compensation instead of
// restoring the pre-prepend scrollTop and skipping the reader into the new
// blocks.
await act(async () => arbiter?.reset());
scrollExtent = 27_812;
scrollElement.scrollTop = 19_267;
// The long answer row starts 1,450px above the viewport and spans it.
const tallRowAt = (top: number) => ({ ...rectAt(top), bottom: top + 9_000, height: 9_000 });
rowElement.getBoundingClientRect = () => tallRowAt(-1_450 - (scrollElement.scrollTop - 19_267));
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: -63.49,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
scrollElement.scrollTop = 19_204;
await act(async () => arbiter?.deliverScroll());
scrollByCalls = 0;
scrollWrites.length = 0;
const anomalies: Array<Record<string, unknown>> = [];
setTranscriptScrollDiagnosticSink((type, fields) => {
  if (type === "scroll-anomaly") anomalies.push(fields);
});
// In-row prepend: extent grows above the visible blocks, the block window
// compensates scrollTop, the row's top edge moves up by the same amount.
scrollExtent += 7_252;
let compensated = false;
await act(async () => { compensated = Boolean(arbiter?.writeOffset("block-window-prepend", scrollElement.scrollTop + 7_252)); });
check(compensated && scrollElement.scrollTop === 19_204 + 7_252,
  `the block-window prepend compensation is written through the arbiter (${scrollElement.scrollTop})`);
await act(async () => arbiter?.deliverScroll());
check(anomalies.length === 0,
  `an in-row prepend with exact compensation is not a reader anomaly (${anomalies.length} recorded)`);
check(scrollElement.dataset.transcriptReaderVisualGuard === undefined,
  "an in-row prepend with exact compensation raises no visual guard");
for (let frame = 0; frame < 4; frame += 1) await flushFrames();
check(scrollByCalls === 0 && scrollWrites.filter((write) => write.owner === "reader-stability").length === 0,
  `the reader guard does not restore the pre-prepend scrollTop (${scrollByCalls} corrections)`);
check(scrollElement.scrollTop === 19_204 + 7_252,
  `the compensated scrollTop survives (${scrollElement.scrollTop})`);
// The next wheel step continues from the compensated position.
scrollElement.scrollTop -= 190;
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: -190.48,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
await act(async () => arbiter?.deliverScroll());
check(anomalies.length === 0, "continuing to scroll after the absorbed prepend stays anomaly-free");
// A genuine reverse jump after the absorbed prepend is still caught: the
// row moves up on screen by 700px without any scrollTop change.
rowElement.getBoundingClientRect = () => tallRowAt(-1_450 - 7_252 - (scrollElement.scrollTop - 19_267) - 700);
await act(async () => arbiter?.deliverScroll());
check(anomalies.length === 1 && Number(anomalies[0].reverseDisplacement) >= 96,
  `a real displacement after the absorbed prepend is still detected (${anomalies.length})`);
setTranscriptScrollDiagnosticSink(() => {});

await act(async () => root.unmount());
dom.window.close();

if (failed > 0) {
  console.error(`\n${failed} transcript reader visual guard race test(s) failed; ${passed} passed.`);
  process.exit(1);
}
console.log(`\n${passed} transcript reader visual guard race tests passed.`);
