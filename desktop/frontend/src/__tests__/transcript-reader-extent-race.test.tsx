// Run: tsx src/__tests__/transcript-reader-extent-race.test.tsx

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
rowElement.dataset.index = "0";
const reboundCoverageRow = dom.window.document.createElement("div");
reboundCoverageRow.className = "transcript__row";
reboundCoverageRow.dataset.rowKey = "row-ready";
scrollElement.getBoundingClientRect = () => rectAt(0);
rowElement.getBoundingClientRect = () => rectAt(20);
reboundCoverageRow.getBoundingClientRect = () => rectAt(20);
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 725 });
let scrollExtent = 15_829;
Object.defineProperty(scrollElement, "scrollHeight", { configurable: true, get: () => scrollExtent });
Object.defineProperty(scrollElement, "scrollTop", { configurable: true, writable: true, value: 14_567.47 });

let scrollByCalls = 0;
let lastScrollByTop = 0;
let mountCoverageOnScrollBy = false;
const virtuosoHandle = {
  scrollBy: (options?: { top?: number }) => {
    scrollByCalls += 1;
    lastScrollByTop = options?.top ?? 0;
    scrollElement.scrollTop += lastScrollByTop;
    if (mountCoverageOnScrollBy) rowElement.getBoundingClientRect = () => rectAt(20);
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
rowElement.getBoundingClientRect = () => rectAt(1_836 - (scrollElement.scrollTop - 12_618.67) + (Number.parseFloat(
  scrollElement.style.getPropertyValue("--transcript-reader-visual-offset"),
) || 0));
await act(async () => arbiter?.deliverScroll());
check(arbiter?.modeRef.current === "manual",
  "a transient physical-bottom clamp cannot claim tail ownership");
check(scrollElement.dataset.transcriptReaderVisualGuard === "true",
  "the transient clamp visually holds the mounted history window");
await act(async () => arbiter?.followGrowingTail());
await flushFrames();
check(scrollByCalls === 0, "the transaction waits while the native extent remains collapsed");
scrollExtent = 15_829;
scrollElement.append(reboundCoverageRow);
await act(async () => arbiter?.followGrowingTail());
await flushFrames();
check(scrollByCalls === 0,
  "the rebound waits for mounted coverage and one unchanged-height interval");
await flushFrames();
check(scrollByCalls === 1 && Math.abs(lastScrollByTop - 1_816) <= 1,
  `the rebound restores the logical anchor exactly once (${lastScrollByTop}px)`);
check(scrollWrites.length === 1 && scrollWrites[0].owner === "reader-stability",
  "the correction is owned by reader stability rather than recovery or tail-follow");
check(scrollElement.dataset.transcriptReaderVisualGuard === undefined,
  "the visual hold clears in the rebound correction frame");
check(arbiter?.modeRef.current === "manual", "the correction preserves manual reader ownership");
reboundCoverageRow.remove();

// A long same-direction transaction spans many streaming revisions. Its
// accepted extent must advance with growth so a later collapse that remains
// above the mount-time height is still visible to the guard.
const growthRaceRealNow = Date.now;
let growthRaceNow = growthRaceRealNow();
Date.now = () => growthRaceNow;
await act(async () => arbiter?.reset());
scrollExtent = 20_000;
scrollElement.scrollTop = 1_000;
rowElement.getBoundingClientRect = () => rectAt(20);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 120,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
scrollExtent = 26_000;
scrollElement.scrollTop = 5_000;
await act(async () => arbiter?.deliverScroll());
scrollByCalls = 0;
scrollWrites.length = 0;
scrollExtent = 24_500;
scrollElement.scrollTop = 3_300;
rowElement.getBoundingClientRect = () => rectAt(1_720 + (Number.parseFloat(
  scrollElement.style.getPropertyValue("--transcript-reader-visual-offset"),
) || 0));
await act(async () => arbiter?.deliverScroll());
for (let frame = 0; frame < 4; frame += 1) await flushFrames();
check(scrollByCalls === 0, "a persistent collapsed range waits for the reader idle deadline");
growthRaceNow += 181;
await flushFrames();
check(scrollByCalls === 1, "streaming growth advances the accepted extent before a later collapse");
check(
  scrollWrites.filter((write) => write.owner === "reader-stability" && write.kind === "scrollBy").length === 1,
  "the post-growth collapse receives one reader-owned anchor correction",
);
Date.now = growthRaceRealNow;

// WKWebView can replace estimates above the viewport without moving native
// scrollTop. The logical rows still jump backwards on screen, so the reader
// transaction must guard and correct the row displacement itself.
await act(async () => arbiter?.reset());
scrollExtent = 23_806;
scrollElement.scrollTop = 22_608;
rowElement.getBoundingClientRect = () => rectAt(12 + (Number.parseFloat(
  scrollElement.style.getPropertyValue("--transcript-reader-visual-offset"),
) || 0));
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
rowElement.getBoundingClientRect = () => rectAt(693 + (Number.parseFloat(
  scrollElement.style.getPropertyValue("--transcript-reader-visual-offset"),
) || 0));
await act(async () => arbiter?.deliverScroll());
check(scrollElement.dataset.transcriptReaderVisualGuard === "true",
  "same-scrollTop estimate growth visually holds the logical reader anchor");
await flushFrames();
check(scrollByCalls === 1 && Math.abs(lastScrollByTop - 681) <= 1,
  `same-scrollTop estimate growth restores the logical anchor once (${lastScrollByTop}px)`);
rowElement.getBoundingClientRect = () => rectAt(12 + (Number.parseFloat(
  scrollElement.style.getPropertyValue("--transcript-reader-visual-offset"),
) || 0));
await flushFrames();
check(scrollElement.dataset.transcriptReaderVisualGuard === undefined,
  "the completed screen-anchor correction releases its visual guard");

// A long native gesture can encounter another range replacement after using
// its one permitted writer correction. Keep that later displacement visually
// guarded until native movement self-restores it; never replay the old write.
rowElement.getBoundingClientRect = () => rectAt(463 + (Number.parseFloat(
  scrollElement.style.getPropertyValue("--transcript-reader-visual-offset"),
) || 0));
await act(async () => arbiter?.deliverScroll());
await flushFrames();
check(scrollElement.dataset.transcriptReaderVisualGuard === "true",
  "a second same-transaction displacement cannot clear the new visual guard with an old write");
check(scrollByCalls === 1,
  "a second same-transaction displacement does not exceed the one-correction writer budget");
scrollElement.scrollTop += 451;
rowElement.getBoundingClientRect = () => rectAt(12 + (Number.parseFloat(
  scrollElement.style.getPropertyValue("--transcript-reader-visual-offset"),
) || 0));
await act(async () => arbiter?.deliverScroll());
check(scrollElement.dataset.transcriptReaderVisualGuard === undefined,
  "same-direction native movement releases the later visual guard after self-restoring the anchor");
check(
  scrollWrites.filter((write) => write.owner === "reader-stability" && write.kind === "scrollBy").length === 1,
  "same-scrollTop anchor displacement stays inside the reader writer lane",
);

// A corrupted extent can momentarily collapse all the way to one viewport.
// That sample is not evidence that the transcript became non-scrollable: the
// active reader guard must keep manual ownership until geometry rebounds.
await act(async () => arbiter?.reset());
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 596 });
scrollExtent = 4_600;
scrollElement.scrollTop = 2_200;
rowElement.getBoundingClientRect = () => rectAt(20);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 24,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
scrollExtent = 596;
scrollElement.scrollTop = 0;
rowElement.getBoundingClientRect = () => rectAt(2_220);
await act(async () => arbiter?.deliverScroll());
check(arbiter?.modeRef.current === "manual",
  "a viewport-sized transient extent cannot manufacture tail ownership");
check(scrollElement.dataset.transcriptReaderVisualGuard === "true",
  "a viewport-sized transient extent keeps the visual reader guard");
scrollExtent = 4_600;
await act(async () => arbiter?.setMode("selection", "end-viewport-collapse-test"));
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 725 });

// A rebound scroll delivery can arrive before the next animation frame. If
// the native extent has already exposed a blank viewport, spend the same
// single correction budget synchronously so the next paint has mounted rows.
await act(async () => arbiter?.reset());
scrollExtent = 5_000;
scrollElement.scrollTop = 2_000;
rowElement.getBoundingClientRect = () => rectAt(20);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 10,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
scrollExtent = 4_000;
scrollElement.scrollTop = 1_000;
rowElement.getBoundingClientRect = () => rectAt(1_000);
await act(async () => arbiter?.deliverScroll());
scrollExtent = 5_000;
scrollByCalls = 0;
mountCoverageOnScrollBy = true;
await act(async () => arbiter?.deliverScroll());
mountCoverageOnScrollBy = false;
check(scrollByCalls === 1 && rowElement.getBoundingClientRect().top === 20,
  "a blank rebound delivery corrects before the next paint");
await flushFrames();
check(scrollByCalls === 1, "the prepaint rebound still spends one correction");

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
scrollElement.append(reboundCoverageRow);
await act(async () => arbiter?.deliverScroll());
scrollExtent = 5_000;
scrollByCalls = 0;
lastScrollByTop = 0;
await flushFrames();
await flushFrames();
check(scrollByCalls === 1 && lastScrollByTop === 1_020,
  `consecutive touch segments use incremental geometry (${lastScrollByTop}px)`);
reboundCoverageRow.remove();
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

// A question jump owns the whole masked paging/landing transaction, not only
// the final indexed write. Entering it must invalidate a queued reader
// correction, and a generic scrollend from the indexed placement must not
// release ownership before the matching paint terminal.
await act(async () => arbiter?.reset());
scrollExtent = 5_000;
scrollElement.scrollTop = 2_000;
rowElement.getBoundingClientRect = () => rectAt(20);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 133.33,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
scrollExtent = 4_000;
scrollElement.scrollTop = 1_000;
rowElement.getBoundingClientRect = () => rectAt(1_020);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.beginQuestionJump(77));
scrollWrites.length = 0;
scrollByCalls = 0;
scrollExtent = 5_000;
await act(async () => arbiter?.followGrowingTail());
await flushFrames();
check(scrollWrites.length === 0 && scrollByCalls === 0,
  "question-jump ownership cancels queued reader and tail writers");
await act(async () => arbiter?.finishProgrammaticScroll());
check(String(arbiter?.modeRef.current) === "restoring",
  "generic scrollend cannot release a masked question jump");
await act(async () => arbiter?.finishQuestionJump(76));
check(String(arbiter?.modeRef.current) === "restoring",
  "a stale question-jump terminal cannot release the current transaction");
await act(async () => arbiter?.finishQuestionJump(77));
check(String(arbiter?.modeRef.current) === "manual",
  "the matching paint terminal releases question-jump ownership");

// A downward wheel at the physical bottom still creates a reader transaction,
// but a non-collapsed movement never earns an anchor correction.
await act(async () => arbiter?.reset());
scrollExtent = 2_000;
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 725 });
scrollElement.scrollTop = 1_275;
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
scrollWrites.length = 0;
scrollByCalls = 0;
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 120,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
scrollElement.scrollTop = 1_100;
await act(async () => arbiter?.followGrowingTail());
await flushFrames();
check(scrollByCalls === 0 && scrollWrites.length === 0,
  "near-bottom downward wheel does not invent a reverse correction");

// Reaching the native tail once does not authorize reader-stability to chase
// later geometry revisions. Manual ownership remains entirely observational.
await act(async () => arbiter?.reset());
scrollExtent = 5_000;
scrollElement.scrollTop = 4_275;
rowElement.dataset.transcriptLastRow = "true";
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.setMode("manual", "test-reader-observation"));
scrollWrites.length = 0;
const realDateNow = Date.now;
let fakeNow = realDateNow();
Date.now = () => fakeNow;
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 120,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
await act(async () => arbiter?.deliverScroll());
scrollExtent = 5_100;
await act(async () => arbiter?.followGrowingTail("data-change"));
fakeNow += 181;
for (let frame = 0; frame < 4; frame += 1) await flushFrames();
check(
  scrollWrites.filter((write) => write.kind === "pinTail" && write.owner === "reader-stability").length === 0,
  "reader stability never writes toward the tail in manual mode",
);
scrollExtent = 5_200;
await act(async () => arbiter?.followGrowingTail("row-measure"));
for (let frame = 0; frame < 4; frame += 1) await flushFrames();
check(
  scrollWrites.filter((write) => write.kind === "pinTail" && write.owner === "reader-stability").length === 0,
  "later geometry revisions cannot re-arm reader tail pinning",
);
check(arbiter?.modeRef.current === "manual", "a post-settle growth revision exits to stable manual ownership");
check(arbiter?.readerTransactionActive === true,
  "stable manual reading keeps an observational mount corridor across a short native-input gap");
fakeNow += 1_001;
await flushFrames();
await flushFrames();
check(scrollElement.dataset.transcriptReaderIntent === "false",
  "the reader writer transaction ends after the bounded quiet window");
check(arbiter?.readerTransactionActive === true,
  "manual ownership retains the layout-only mount corridor after the writer transaction ends");
await act(async () => arbiter?.setMode("selection", "test-manual-layout-lease-release"));
check(arbiter?.readerTransactionActive === false,
  "an explicit owner releases the idle manual layout lease");

// A same-direction wheel arriving after the 180ms idle boundary must start a
// new ownership epoch while inheriting the prior transaction's high-water.
// Otherwise the smaller replacement range becomes a fresh baseline and can
// manufacture a false tail handoff.
const readerStarts: Array<Record<string, unknown>> = [];
setTranscriptScrollDiagnosticSink((type, fields) => {
  if (type === "reader-transaction" && fields.result === "started") readerStarts.push(fields);
});
await act(async () => arbiter?.reset());
scrollExtent = 26_000;
scrollElement.scrollTop = 20_000;
rowElement.getBoundingClientRect = () => rectAt(20);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.setMode("manual", "test-passive-reader-high-water"));
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 120,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
fakeNow += 181;
for (let frame = 0; frame < 4; frame += 1) await flushFrames();
check(arbiter?.readerTransactionActive === true,
  "the first transaction keeps its measured high-water through bounded settling");
scrollExtent = 24_000;
scrollElement.scrollTop = 23_275;
rowElement.dataset.transcriptLastRow = "true";
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 120,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
await act(async () => arbiter?.deliverScroll());
check(
  readerStarts.length >= 2 && readerStarts.at(-2)?.ownershipEpoch !== readerStarts.at(-1)?.ownershipEpoch,
  "input after 180ms starts a new reader ownership epoch",
);
fakeNow += 181;
for (let frame = 0; frame < 4; frame += 1) await flushFrames();
check(String(arbiter?.modeRef.current) === "manual",
  "a new same-direction epoch cannot claim tail from an inherited collapsed extent");
check(arbiter?.readerTransactionActive === true,
  "a new reader epoch reuses the manual layout lease without contracting the mount corridor");
delete rowElement.dataset.transcriptLastRow;
Date.now = realDateNow;
setTranscriptScrollDiagnosticSink(() => {});

// A real reader-to-tail handoff keeps the enlarged mount window after writer
// ownership changes. WKWebView otherwise contracts the overscan in that
// commit, replaces the measured tail with estimates, and paints a reverse
// jump. The next explicit owner must still release the layout-only guard.
await act(async () => arbiter?.reset());
scrollExtent = 5_000;
scrollElement.scrollTop = 4_275;
rowElement.dataset.transcriptLastRow = "true";
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.setMode("manual", "test-tail-handoff-layout-safe"));
let handoffNow = realDateNow();
Date.now = () => handoffNow;
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 120,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
handoffNow += 181;
for (let frame = 0; frame < 4; frame += 1) await flushFrames();
check(String(arbiter?.modeRef.current) === "tail-follow", "two stable reader frames hand ownership to the physical tail");
check(arbiter?.readerTransactionActive === true, "tail handoff retains only the layout-safe mount window");
await act(async () => arbiter?.setMode("manual", "test-tail-handoff-layout-safe-release"));
check(arbiter?.readerTransactionActive === false, "the next explicit owner releases the handoff mount window");
Date.now = realDateNow;
delete rowElement.dataset.transcriptLastRow;

// A stable real shrink must not veto the tail handoff forever. Once the
// smaller extent has held for two painted samples and the reader produces
// direction-consistent native displacement inside it, the transaction accepts
// the shrunken extent as its new baseline and the physical tail of that range
// becomes claimable again (#9513 storm replay).
const extentAcceptances: Array<Record<string, unknown>> = [];
setTranscriptScrollDiagnosticSink((type, fields) => {
  if (type === "reader-transaction" && fields.result === "extent-accepted") extentAcceptances.push(fields);
});
await act(async () => arbiter?.reset());
scrollExtent = 26_000;
scrollElement.scrollTop = 20_000;
rowElement.getBoundingClientRect = () => rectAt(20);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.setMode("manual", "test-stable-shrink-acceptance"));
let shrinkNow = realDateNow();
Date.now = () => shrinkNow;
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 640,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
scrollExtent = 24_000;
scrollElement.scrollTop = 20_640;
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 640,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
await act(async () => arbiter?.deliverScroll());
// The shrunken extent must prove stable across two painted samples before any
// acceptance; movement delivered while it is still transient keeps waiting.
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 640,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
scrollElement.scrollTop = 21_280;
await act(async () => arbiter?.deliverScroll());
check(extentAcceptances.length === 1,
  "direction-consistent displacement inside a stabilized shrink accepts the new extent");
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 640,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
scrollElement.scrollTop = 23_275;
rowElement.dataset.transcriptLastRow = "true";
await act(async () => arbiter?.deliverScroll());
shrinkNow += 181;
for (let frame = 0; frame < 6; frame += 1) await flushFrames();
check(String(arbiter?.modeRef.current) === "tail-follow",
  "the accepted shrunken extent hands ownership to its physical tail");
Date.now = realDateNow;
delete rowElement.dataset.transcriptLastRow;

// The mirror contract: a collapse-fabricated bottom reached only by the
// browser's own clamp earns no directional displacement proof, so the
// high-water is never rebased and the handoff stays closed even after the
// shrink stabilizes — in-place wheeling changes nothing.
await act(async () => arbiter?.reset());
scrollExtent = 26_000;
scrollElement.scrollTop = 25_275;
rowElement.getBoundingClientRect = () => rectAt(20);
rowElement.dataset.transcriptLastRow = "true";
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.setMode("manual", "test-fake-bottom-no-handoff"));
extentAcceptances.length = 0;
let parkedNow = realDateNow();
Date.now = () => parkedNow;
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 640,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
// The extent really shrinks and the browser clamps the parked reader onto the
// fabricated bottom; that clamp is reverse motion, never reader displacement.
scrollExtent = 24_000;
scrollElement.scrollTop = 23_275;
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 640,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
await act(async () => arbiter?.deliverScroll());
parkedNow += 181;
for (let frame = 0; frame < 6; frame += 1) await flushFrames();
check(extentAcceptances.length === 0,
  "in-place wheeling at a collapse-fabricated bottom never accepts the shrunken extent");
check(String(arbiter?.modeRef.current) === "manual",
  "in-place wheeling at a collapse-fabricated bottom cannot claim the tail");
Date.now = realDateNow;
delete rowElement.dataset.transcriptLastRow;
setTranscriptScrollDiagnosticSink(() => {});

// A large extent contraction is transient for its first painted sample and is
// accepted only after two consecutive stable frames.
const geometryEvents: Array<Record<string, unknown>> = [];
setTranscriptScrollDiagnosticSink((type, fields) => {
  if (type === "geometry-revision") geometryEvents.push(fields);
});
await act(async () => arbiter?.reset());
scrollExtent = 5_000;
scrollElement.scrollTop = 4_275;
await act(async () => arbiter?.followGrowingTail("data-change"));
await flushFrames();
scrollExtent = 3_000;
await act(async () => arbiter?.followGrowingTail("row-measure"));
await flushFrames();
check(geometryEvents.some((event) => event.transient === true), "a 2k extent collapse is treated as transient on its first frame");
await flushFrames();
await flushFrames();
check(geometryEvents.some((event) => event.result === "stable" && event.transient === false), "a permanent extent shrink is accepted after two stable frames");
setTranscriptScrollDiagnosticSink(() => {});

// A replacement native-thumb gesture owns a new pointer transaction. A late
// pointerup from the displaced gesture must not release or demote it.
await act(async () => arbiter?.reset());
scrollExtent = 5_000;
Object.defineProperties(scrollElement, {
  offsetWidth: { configurable: true, value: 800 },
  clientWidth: { configurable: true, value: 780 },
  clientLeft: { configurable: true, value: 0 },
});
scrollElement.getBoundingClientRect = () => rectAt(0);
scrollElement.scrollTop = 1_000;
const thumbPointer = (pointerId: number) => ({
  button: 0,
  clientX: 795,
  nativeEvent: { button: 0, clientX: 795, pointerId },
}) as unknown as React.PointerEvent<HTMLElement>;
await act(async () => {
  arbiter?.onPointerDownIntent(thumbPointer(1));
  arbiter?.onPointerDownIntent(thumbPointer(2));
});
const staleRelease = new dom.window.Event("pointerup", { bubbles: true });
Object.defineProperty(staleRelease, "pointerId", { value: 1 });
await act(async () => dom.window.dispatchEvent(staleRelease));
check(scrollElement.dataset.nativeScrollbarDrag === "true",
  "a stale pointerup cannot release the replacement native thumb");
check(String(arbiter?.modeRef.current) === "native-thumb",
  "the replacement thumb retains explicit scroll ownership");

// A stationary release at the physical bottom is insufficient: the same
// pointer transaction must have observed forward native progress.
scrollElement.scrollTop = 4_275;
await act(async () => arbiter?.deliverScroll(scrollElement));
const activeRelease = new dom.window.Event("pointerup", { bubbles: true });
Object.defineProperty(activeRelease, "pointerId", { value: 2 });
await act(async () => dom.window.dispatchEvent(activeRelease));
check(String(arbiter?.modeRef.current) === "tail-follow",
  "the active thumb may commit tail ownership after forward native progress");

await act(async () => arbiter?.onPointerDownIntent(thumbPointer(3)));
const stationaryRelease = new dom.window.Event("pointerup", { bubbles: true });
Object.defineProperty(stationaryRelease, "pointerId", { value: 3 });
await act(async () => dom.window.dispatchEvent(stationaryRelease));
check(String(arbiter?.modeRef.current) === "manual",
  "a stationary thumb at the bottom cannot claim tail ownership");

await act(async () => arbiter?.onPointerDownIntent(thumbPointer(4)));
await act(async () => arbiter?.reset());
check(scrollElement.dataset.nativeScrollbarDrag === undefined,
  "a generation reset clears the native-thumb DOM transaction");
check(String(arbiter?.modeRef.current) === "tail-follow",
  "a generation reset cannot retain native-thumb ownership");

await act(async () => root.unmount());
dom.window.close();

if (failed > 0) {
  console.error(`\n${failed} transcript reader extent race test(s) failed; ${passed} passed.`);
  process.exit(1);
}
console.log(`\n${passed} transcript reader extent race tests passed.`);
