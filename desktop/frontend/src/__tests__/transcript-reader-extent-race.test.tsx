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
rowElement.getBoundingClientRect = () => rectAt(20);
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 725 });
scrollElement.getBoundingClientRect = () => ({
  ...rectAt(0),
  bottom: scrollElement.clientHeight,
  height: scrollElement.clientHeight,
});
let scrollExtent = 15_829;
Object.defineProperty(scrollElement, "scrollHeight", { configurable: true, get: () => scrollExtent });
Object.defineProperty(scrollElement, "scrollTop", { configurable: true, writable: true, value: 14_567.47 });

let scrollByCalls = 0;
let lastScrollByTop = 0;
let virtuosoScrollToCalls = 0;
dom.window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => {
  scrollWrites.push(write);
  if (write.owner === "reader-stability") {
    scrollByCalls += 1;
    lastScrollByTop = (write.top ?? 0) - write.scrollTop;
  }
};
const virtuosoHandle = {
  // Match Virtuoso's synchronous native-scroller write so a following rAF
  // observes the accepted correction instead of replaying a test-only stale
  // scrollTop value.
  scrollBy: ({ top }: { top: number }) => { scrollElement.scrollTop += top; },
  scrollTo: ({ top }: { top: number }) => { virtuosoScrollToCalls += 1; scrollElement.scrollTop = top; },
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

const wheel = (deltaY: number) => act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));

// Composer wrap shrinks the in-flow viewport. A bottom-adjacent viewport stays
// tail-owned without a synchronous write; the coalesced revision observes it.
await act(async () => arbiter?.reset());
scrollExtent = 500;
scrollElement.scrollTop = 400;
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 100 });
await act(async () => arbiter?.followGrowingTail());
await flushFrames();
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 80 });
await act(async () => arbiter?.followGrowingTail());
check(scrollElement.scrollTop === 400, "footer-driven viewport shrink performs no synchronous tail write");
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
await wheel(133.33);
scrollExtent = 13_344;
scrollElement.scrollTop = 12_618.67;
rowElement.getBoundingClientRect = () => rectAt(1_836);
await act(async () => arbiter?.deliverScroll());
check(arbiter?.modeRef.current === "reader-gesture",
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
await wheel(133.33);
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

// WKWebView can replace the entire Virtuoso mount window in the scroll event
// that reports a large reverse jump. Correct that event synchronously: by the
// next animation frame the old logical anchor is already unmounted and the
// user has seen the bad range for one paint.
await act(async () => arbiter?.reset());
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 596 });
scrollExtent = 23_349;
scrollElement.scrollTop = 22_753;
rowElement.getBoundingClientRect = () => rectAt(-11);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
await wheel(24);
scrollWrites.length = 0;
scrollByCalls = 0;
scrollExtent = 23_269;
scrollElement.scrollTop = 21_986;
rowElement.remove();
await act(async () => arbiter?.deliverScroll());
check(scrollByCalls === 1 && lastScrollByTop === 687,
  `an unmounted-anchor reverse jump is corrected in its scroll event (${lastScrollByTop}px)`);
check(scrollWrites.length === 1 && scrollWrites[0].owner === "reader-stability",
  "the pre-paint correction still passes through the single writer");
scrollElement.append(rowElement);

// A Virtuoso range commit can move the old visible rows without emitting a
// native scroll event. The list MutationObserver calls this pre-paint reader
// observation before replacing the transaction's logical anchor.
await act(async () => arbiter?.reset());
scrollExtent = 22_834;
scrollElement.scrollTop = 15_438;
rowElement.getBoundingClientRect = () => rectAt(-16);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
await wheel(24);
scrollWrites.length = 0;
scrollByCalls = 0;
scrollExtent = 23_114;
rowElement.getBoundingClientRect = () => rectAt(516);
await act(async () => arbiter?.observeReaderExtent());
check(scrollByCalls === 1 && lastScrollByTop === 532,
  `a DOM-range-only visual reverse is corrected before paint (${lastScrollByTop}px)`);
check(scrollWrites.length === 1 && scrollWrites[0].owner === "reader-stability",
  "the range-mutation correction still passes through the single writer");

// The first correction drops its stale anchor. A native host can acknowledge
// that write and coalesce the next +24px wheel into the same scroll event, so
// the reported offset passes the exact target. Capture the corrected leading
// row at that event before the following visual-only range swap.
scrollElement.scrollTop += 24;
rowElement.getBoundingClientRect = () => rectAt(-40);
await act(async () => arbiter?.observeReaderExtent());
scrollWrites.length = 0;
scrollByCalls = 0;
scrollExtent += 318;
rowElement.getBoundingClientRect = () => rectAt(530);
await act(async () => arbiter?.observeReaderExtent());
check(scrollByCalls === 1 && lastScrollByTop === 546,
  `an acknowledged correction re-anchors the next coalesced range swap (${lastScrollByTop}px)`);
check(scrollWrites.length === 1 && scrollWrites[0].owner === "reader-stability",
  "the re-anchored range correction keeps the single-writer contract");

// A following native wheel can arrive after the replacement range mounts but
// before its mutation observer runs. Keep the still-mounted prior anchor so
// that the new range's first row cannot bless its own visual reversal.
await act(async () => arbiter?.reset());
scrollExtent = 21_716;
scrollElement.scrollTop = 4_678;
rowElement.getBoundingClientRect = () => rectAt(-1);
const oldSecondRow = dom.window.document.createElement("div");
oldSecondRow.className = "transcript__row";
oldSecondRow.dataset.rowKey = "old-second-row";
oldSecondRow.getBoundingClientRect = () => rectAt(36);
scrollElement.append(oldSecondRow);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
await wheel(24);
const incomingRow = dom.window.document.createElement("div");
incomingRow.className = "transcript__row";
incomingRow.dataset.rowKey = "incoming-row";
incomingRow.getBoundingClientRect = () => rectAt(-25);
const incomingSecondRow = dom.window.document.createElement("div");
incomingSecondRow.className = "transcript__row";
incomingSecondRow.dataset.rowKey = "incoming-second-row";
incomingSecondRow.getBoundingClientRect = () => rectAt(7);
scrollElement.prepend(incomingRow);
incomingRow.after(incomingSecondRow);
oldSecondRow.remove();
scrollElement.scrollTop = 4_702;
rowElement.getBoundingClientRect = () => rectAt(559);
scrollWrites.length = 0;
scrollByCalls = 0;
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 24,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
await act(async () => arbiter?.observeReaderExtent());
check(scrollByCalls === 1 && lastScrollByTop === 560,
  `a wheel cannot replace the mounted pre-swap anchor before observation (${lastScrollByTop}px)`);
check(scrollWrites.length === 1 && scrollWrites[0].owner === "reader-stability",
  "the interleaved wheel/range correction keeps the single-writer contract");
incomingRow.remove();
incomingSecondRow.remove();

// WKWebView can retire the leading row before the hook's next sample while a
// boundary row from the last painted frame remains barely visible. Protect
// that common row too; the single leading-anchor lookup alone has no DOM node
// left with which to prove the 577px visual reversal returned by native CI.
await act(async () => arbiter?.reset());
scrollExtent = 20_804;
scrollElement.scrollTop = 3_937;
rowElement.getBoundingClientRect = () => rectAt(-32);
const paintedBoundaryRow = dom.window.document.createElement("div");
paintedBoundaryRow.className = "transcript__row";
paintedBoundaryRow.dataset.rowKey = "row-115";
paintedBoundaryRow.getBoundingClientRect = () => rectAt(0);
scrollElement.append(paintedBoundaryRow);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 24,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
rowElement.remove();
const olderRangeRow = dom.window.document.createElement("div");
olderRangeRow.className = "transcript__row";
olderRangeRow.dataset.rowKey = "row-101";
olderRangeRow.getBoundingClientRect = () => rectAt(0);
scrollElement.prepend(olderRangeRow);
scrollElement.scrollTop = 3_961;
paintedBoundaryRow.getBoundingClientRect = () => rectAt(577);
scrollWrites.length = 0;
scrollByCalls = 0;
await act(async () => arbiter?.observeReaderExtent());
check(scrollByCalls === 1 && lastScrollByTop === 577,
  `the last common painted row blocks the native 577px range reversal (${lastScrollByTop}px)`);
check(scrollWrites.length === 1 && scrollWrites[0].owner === "reader-stability",
  "the common-row pre-paint correction keeps the single-writer contract");
olderRangeRow.remove();
paintedBoundaryRow.remove();
scrollElement.append(rowElement);

// WKWebView may publish an intermediate range with no rows in common, then
// promote it before a later native task applies the final translated range.
// Keep one prior painted baseline: the final range can restore exactly one
// older boundary row and expose a reversal that the intermediate map cannot
// identify.
await act(async () => arbiter?.reset());
scrollExtent = 20_957;
scrollElement.scrollTop = 4_974;
rowElement.dataset.rowKey = "row-142";
rowElement.getBoundingClientRect = () => rectAt(-6);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 24,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
const intermediateRow = dom.window.document.createElement("div");
intermediateRow.className = "transcript__row";
intermediateRow.dataset.rowKey = "row-128";
intermediateRow.getBoundingClientRect = () => rectAt(-13);
rowElement.replaceWith(intermediateRow);
scrollElement.scrollTop = 4_998;
await act(async () => arbiter?.observeReaderExtent());
await flushFrames();
await new Promise((resolve) => dom.window.setTimeout(resolve, 0));
await act(async () => arbiter?.observeReaderExtent());
await flushFrames();
await new Promise((resolve) => dom.window.setTimeout(resolve, 0));
const finalBoundaryRow = dom.window.document.createElement("div");
finalBoundaryRow.className = "transcript__row";
finalBoundaryRow.dataset.rowKey = "row-142";
finalBoundaryRow.getBoundingClientRect = () => rectAt(591);
scrollElement.append(finalBoundaryRow);
scrollWrites.length = 0;
scrollByCalls = 0;
await act(async () => arbiter?.observeReaderExtent());
check(scrollByCalls === 1 && lastScrollByTop === 597,
  `a promoted no-common range cannot erase the prior painted boundary row (${lastScrollByTop}px)`);
check(scrollWrites.length === 1 && scrollWrites[0].owner === "reader-stability",
  "duplicate promotions retain the prior range and keep the single-writer contract");
intermediateRow.remove();
finalBoundaryRow.replaceWith(rowElement);
rowElement.dataset.rowKey = "row-a";

// A long gesture can outlive its original leading row. Commit each accepted
// forward frame as the next guard anchor so a later visual-only range swap is
// compared with the row the user actually saw immediately before the swap.
await act(async () => arbiter?.reset());
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 596 });
scrollExtent = 24_906;
scrollElement.scrollTop = 17_451;
rowElement.getBoundingClientRect = () => rectAt(-18);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 24,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
const lastPaintedRow = dom.window.document.createElement("div");
lastPaintedRow.className = "transcript__row";
lastPaintedRow.dataset.rowKey = "row-463";
lastPaintedRow.getBoundingClientRect = () => rectAt(-18);
scrollElement.scrollTop = 17_475;
rowElement.getBoundingClientRect = () => rectAt(-42);
await act(async () => arbiter?.observeReaderExtent());
rowElement.replaceWith(lastPaintedRow);
await act(async () => arbiter?.observeReaderExtent());
scrollWrites.length = 0;
scrollByCalls = 0;
scrollExtent = 25_187;
lastPaintedRow.getBoundingClientRect = () => rectAt(515);
await act(async () => arbiter?.observeReaderExtent());
check(scrollByCalls === 1 && lastScrollByTop === 533,
  `the last painted row protects a later visual-only range swap (${lastScrollByTop}px)`);
check(scrollWrites.length === 1 && scrollWrites[0].owner === "reader-stability",
  "the continuous painted-anchor correction keeps the single-writer contract");
lastPaintedRow.replaceWith(rowElement);

// Native host input can advance the scroller without surfacing a React wheel
// event. Reconcile the guard direction from consecutive native scroll
// deliveries so the setup wheel's opposite direction cannot leave the real
// forward traversal unprotected.
const originalUserAgent = dom.window.navigator.userAgent;
Object.defineProperty(dom.window.navigator, "userAgent", { configurable: true, value: "AppleWebKit/605.1.15" });
// The first host-native delivery can both replace the synthetic setup
// direction and commit an older Virtuoso range. Preserve the prior painted
// boundary across that handoff instead of constructing the new guard from the
// already-reversed DOM.
await act(async () => arbiter?.reset());
scrollExtent = 24_489;
scrollElement.scrollTop = 18_920;
rowElement.dataset.rowKey = "row-505";
rowElement.getBoundingClientRect = () => rectAt(-20);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
await wheel(-1);
let handoffCandidate = rowElement;
for (const rowKey of ["row-392", "row-404", "row-418"]) {
  const replacement = dom.window.document.createElement("div");
  replacement.className = "transcript__row";
  replacement.dataset.rowKey = rowKey;
  replacement.getBoundingClientRect = () => rectAt(-12);
  handoffCandidate.replaceWith(replacement);
  handoffCandidate = replacement;
  await act(async () => arbiter?.observeReaderExtent());
}
await flushFrames();
await new Promise((resolve) => dom.window.setTimeout(resolve, 0));
handoffCandidate.replaceWith(rowElement);
scrollWrites.length = 0;
scrollByCalls = 0;
scrollExtent += 33;
scrollElement.scrollTop += 340;
rowElement.getBoundingClientRect = () => rectAt(589);
await act(async () => arbiter?.deliverScroll());
check(scrollByCalls === 1 && lastScrollByTop === 609,
  `direction handoff retains the three-generation painted row (${lastScrollByTop}px)`);
check(scrollWrites.length === 1 && scrollWrites[0].owner === "reader-stability",
  "the coalesced direction/range correction remains single-owned");

await act(async () => arbiter?.reset());
scrollExtent = 24_515;
scrollElement.scrollTop = 23_160;
rowElement.dataset.rowKey = "row-a";
rowElement.getBoundingClientRect = () => rectAt(-25);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
await wheel(-1);
scrollElement.scrollTop += 320;
rowElement.getBoundingClientRect = () => rectAt(-49);
await act(async () => arbiter?.deliverScroll());
scrollWrites.length = 0;
scrollByCalls = 0;
scrollExtent += 170;
scrollElement.scrollTop += 24;
rowElement.getBoundingClientRect = () => rectAt(121);
await act(async () => arbiter?.deliverScroll());
check(scrollByCalls === 1 && lastScrollByTop === 170,
  `a coalesced native delivery replaces an opposite setup direction before a late range swap (${lastScrollByTop}px)`);
check(scrollWrites.length === 1 && scrollWrites[0].owner === "reader-stability",
  "native direction reconciliation keeps the late range correction single-owned");
const nativeCorrectionTarget = scrollElement.scrollTop;
scrollElement.scrollTop = nativeCorrectionTarget - 43;
rowElement.getBoundingClientRect = () => rectAt(-49);
await act(async () => arbiter?.deliverScroll());
scrollElement.scrollTop = nativeCorrectionTarget;
await act(async () => arbiter?.deliverScroll());
check(scrollWrites.length === 1,
  "a delayed native correction acknowledgement is not recycled as opposite user input");
scrollElement.scrollTop += 24;
rowElement.getBoundingClientRect = () => rectAt(-73);
await act(async () => arbiter?.deliverScroll());
check(scrollWrites.length === 1,
  "forward input after the acknowledgement resumes without a correction feedback loop");
const nativeOriginalScrollTo = scrollElement.scrollTo;
scrollElement.scrollTo = () => {};
scrollExtent += 170;
scrollElement.scrollTop += 24;
rowElement.getBoundingClientRect = () => rectAt(97);
await act(async () => arbiter?.deliverScroll());
const stalledForwardTarget = scrollWrites.at(-1)?.top ?? 0;
await act(async () => arbiter?.observeReaderExtent());
check(scrollWrites.length === 2,
  "an unacknowledged forward native correction remains single-owned");
scrollElement.scrollTop = stalledForwardTarget + scrollElement.clientHeight + 24;
rowElement.getBoundingClientRect = () => rectAt(30);
await act(async () => arbiter?.deliverScroll());
check(scrollWrites.length === 2,
  "progress beyond a stalled forward correction discards its non-adjacent painted baseline");
await flushFrames();
await new Promise((resolve) => dom.window.setTimeout(resolve, 0));
scrollExtent += 170;
scrollElement.scrollTop += 24;
rowElement.getBoundingClientRect = () => rectAt(200);
await act(async () => arbiter?.deliverScroll());
check(scrollWrites.length === 3,
  `progress beyond a stalled forward correction releases the next range correction (${scrollWrites.length})`);
scrollElement.scrollTo = nativeOriginalScrollTo;

// WKWebView can paint an accepted native-scroll range, then replace it before
// the deferred post-paint baseline timer runs. The native delivery itself is
// the last reliable observation of that range; retain its barely visible
// boundary row so the next older range cannot flash downward.
await act(async () => arbiter?.reset());
scrollExtent = 24_765;
scrollElement.scrollTop = 18_768;
rowElement.dataset.rowKey = "row-495";
rowElement.getBoundingClientRect = () => rectAt(-99);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 24,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
scrollElement.scrollTop += 24;
await act(async () => arbiter?.deliverScroll());
scrollWrites.length = 0;
scrollByCalls = 0;
scrollExtent = 24_506;
rowElement.getBoundingClientRect = () => rectAt(573);
await act(async () => arbiter?.observeReaderExtent());
check(scrollByCalls === 1 && lastScrollByTop === 672,
  `an accepted native delivery retains its painted boundary before replacement (${lastScrollByTop}px)`);
check(scrollWrites.length === 1 && scrollWrites[0].owner === "reader-stability",
  "the native-delivery baseline correction keeps the single-writer contract");
rowElement.dataset.rowKey = "row-a";
Object.defineProperty(dom.window.navigator, "userAgent", { configurable: true, value: originalUserAgent });

// A correction acknowledgement can share a native delivery with the next
// older range swap. Validate the retained correction anchor before accepting
// that delivery, otherwise its wrong leading row becomes the new baseline.
await act(async () => arbiter?.reset());
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 596 });
scrollExtent = 20_411;
scrollElement.scrollTop = 1_728;
rowElement.getBoundingClientRect = () => rectAt(-29);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 24,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
scrollWrites.length = 0;
scrollByCalls = 0;
rowElement.getBoundingClientRect = () => rectAt(567);
await act(async () => arbiter?.observeReaderExtent());
check(scrollByCalls === 1 && lastScrollByTop === 596,
  `the first replacement range receives its reader correction (${lastScrollByTop}px)`);
scrollWrites.length = 0;
scrollByCalls = 0;
scrollElement.scrollTop += 72;
await act(async () => arbiter?.observeReaderExtent());
check(scrollByCalls === 1 && lastScrollByTop === 596,
  `a coalesced acknowledgement cannot bless the next older range (${lastScrollByTop}px)`);
check(scrollWrites.length === 1 && scrollWrites[0].owner === "reader-stability",
  "the acknowledgement-range race remains a single owned correction");

// Native WebViews can deliver the Virtuoso range replacement well after the
// 180ms reader-intent idle boundary. Keep the accepted logical row alive
// across a multi-second compositor delay instead of accepting the late range
// as a new position. The fake clock makes this race deterministic without
// sleeping.
await act(async () => arbiter?.reset());
scrollExtent = 24_592;
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 725 });
scrollElement.scrollTop = 19_228;
rowElement.getBoundingClientRect = () => rectAt(-28);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
const originalDateNow = Date.now;
let fakeNow = 10_000;
Date.now = () => fakeNow;
try {
  await act(async () => arbiter?.onWheelIntent({
    ctrlKey: false,
    deltaMode: 0,
    deltaX: 0,
    deltaY: 24,
    target: scrollElement,
  } as React.WheelEvent<HTMLElement>));
  scrollWrites.length = 0;
  scrollByCalls = 0;
  fakeNow += 1_600;
  await flushFrames();
  fakeNow += 60;
  scrollExtent = 24_656;
  scrollElement.scrollTop = 19_368;
  rowElement.getBoundingClientRect = () => rectAt(433);
  await act(async () => arbiter?.observeReaderExtent());
  check(scrollByCalls === 1 && lastScrollByTop > 460,
    `the delayed range replacement restores the accepted logical row (${lastScrollByTop}px)`);
  check(scrollWrites.length === 1 && scrollWrites[0].owner === "reader-stability",
    "the delayed native correction still passes through the single writer");
} finally {
  Date.now = originalDateNow;
}

// WKWebView can briefly outrun Virtuoso's mounted range. The blank native
// coordinate must not survive a paint. Hold the last occupied logical
// position immediately, and do not reissue the async native correction while
// its first write is pending.
await act(async () => arbiter?.reset());
scrollExtent = 20_416;
scrollElement.scrollTop = 2_413;
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
scrollWrites.length = 0;
scrollByCalls = 0;
scrollElement.scrollTop = 3_655;
rowElement.getBoundingClientRect = () => rectAt(900);
await act(async () => arbiter?.deliverScroll());
const replacementRow = dom.window.document.createElement("div");
replacementRow.className = "transcript__row";
replacementRow.dataset.rowKey = "row-b";
replacementRow.getBoundingClientRect = () => rectAt(20);
rowElement.replaceWith(replacementRow);
scrollElement.scrollTop = 2_756;
const originalScrollTo = scrollElement.scrollTo;
scrollElement.scrollTo = () => {};
try {
  await act(async () => arbiter?.deliverScroll());
  await act(async () => arbiter?.observeReaderExtent());
  check(scrollByCalls === 1 && lastScrollByTop === -1_242,
    `the blank frame is synchronously held at the last occupied logical position once (${lastScrollByTop}px)`);
  check(scrollWrites.length === 1 && scrollWrites[0].owner === "reader-stability",
    "an unacknowledged WebKit correction is not emitted again");
} finally {
  scrollElement.scrollTo = originalScrollTo;
  replacementRow.replaceWith(rowElement);
}

// Hosted WKWebView can replace every occupied row while pulling native
// scrollTop backwards. A pixel-only correction cannot remount that logical
// range, so the same writer transaction must synchronize Virtuoso too.
await act(async () => arbiter?.reset());
scrollExtent = 24_514;
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 596 });
scrollElement.scrollTop = 23_874;
rowElement.dataset.rowKey = "row-612";
rowElement.getBoundingClientRect = () => rectAt(-21);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 24,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
scrollWrites.length = 0;
scrollByCalls = 0;
const syncCallsBeforeReplacement = virtuosoScrollToCalls;
replacementRow.dataset.rowKey = "row-574";
replacementRow.getBoundingClientRect = () => rectAt(-9);
rowElement.replaceWith(replacementRow);
scrollElement.scrollTop = 22_558;
await act(async () => arbiter?.observeReaderExtent());
check(virtuosoScrollToCalls === syncCallsBeforeReplacement + 1,
  "a no-common occupied range correction synchronizes Virtuoso and native pixels");
replacementRow.replaceWith(rowElement);
rowElement.dataset.rowKey = "row-a";

// Near-bottom input uses the same reader transaction as every other logical
// position. A synthetic >96px reverse displacement must be rejected instead
// of slipping through the old near-bottom exception.
await act(async () => arbiter?.reset());
scrollExtent = 2_000;
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 725 });
scrollElement.scrollTop = 1_275;
rowElement.getBoundingClientRect = () => rectAt(20);
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
await act(async () => arbiter?.observeReaderExtent());
// Model the browser applying the first correction before the hook's rAF
// acknowledgement; otherwise this static jsdom rect still describes the
// pre-write range and intentionally looks like a second native swap.
rowElement.getBoundingClientRect = () => rectAt(-100);
await flushFrames();
check(scrollByCalls === 1 && scrollWrites.length === 1 && scrollWrites[0].owner === "reader-stability",
  "near-bottom reader transaction rejects the same >96px reverse jump");

// A logical tail can temporarily expose a small native footer remainder while
// its final measurement lands. Downward input must consume that native range
// without releasing tail ownership and restarting LAST on every tick.
await act(async () => arbiter?.reset());
scrollExtent = 2_000;
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 725 });
scrollElement.scrollTop = 1_206;
await act(async () => arbiter?.deliverScroll());
let preventedTailWheel = false;
const acceptedTailWheel = await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 120,
  target: scrollElement,
  preventDefault: () => { preventedTailWheel = true; },
} as React.WheelEvent<HTMLElement>));
check(acceptedTailWheel === true && arbiter?.modeRef.current === "tail-follow" && !preventedTailWheel,
  "downward input consumes a measured native tail remainder without restarting LAST");

scrollElement.scrollTop = 1_206;
arbiter!.layoutTransientRef.current = true;
const acceptedTransientTailWheel = await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false,
  deltaMode: 0,
  deltaX: 0,
  deltaY: 120,
  target: scrollElement,
} as React.WheelEvent<HTMLElement>));
check(acceptedTransientTailWheel === true && arbiter?.modeRef.current === "tail-follow",
  "downward input cannot restart LAST while its tail transaction is transient");
arbiter!.layoutTransientRef.current = false;

await act(async () => root.unmount());
dom.window.close();

if (failed > 0) {
  console.error(`\n${failed} transcript reader extent race test(s) failed; ${passed} passed.`);
  process.exit(1);
}
console.log(`\n${passed} transcript reader extent race tests passed.`);
