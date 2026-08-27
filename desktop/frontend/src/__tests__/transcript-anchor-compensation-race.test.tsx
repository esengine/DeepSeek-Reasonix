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
let tailClampLag = 0;
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
    scrollElement.scrollTop = Math.max(0, Math.min(scrollExtent - scrollElement.clientHeight - tailClampLag, top));
    tailClampLag = 0;
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

// A downward wheel has no valid destination once a settled tail-follow
// surface is already at its native maximum. Consuming that boundary default
// keeps WebKit from resolving it against a stale virtual range.
scrollElement.scrollTop = 400;
let preventedBottomWheel = false;
let acceptedBottomWheel = true;
await act(async () => {
  acceptedBottomWheel = arbiter?.onWheelIntent({
    ctrlKey: false,
    deltaX: 0,
    deltaY: 40,
    target: scrollElement,
    preventDefault: () => { preventedBottomWheel = true; },
  } as unknown as React.WheelEvent<HTMLElement>) ?? false;
});
check(acceptedBottomWheel === false && preventedBottomWheel, "stable physical tail consumes an over-boundary downward wheel");
check(arbiter?.modeRef.current === "tail-follow", "over-boundary wheel keeps tail-follow ownership");

// ── Bottom-hold re-entry (#8709/#9099): auto re-entry into tail-follow
// requires the bottom to be HELD — two consecutive at-bottom deliveries inside
// one reader-intent window with no upward gesture in between. A single
// touch-down claims the gesture but stays reader-owned.
scrollElement.scrollTop = 400;
rowElement.getBoundingClientRect = () => rectAt(-99);
await act(async () => arbiter?.releaseTailFollow());
await wheelDown();
check(arbiter?.modeRef.current === "reader-gesture", "a single downward touch-down stays reader-owned");
await flushFrames();
check(arbiter?.modeRef.current === "tail-follow", "a held bottom (next-frame delivery) re-enters tail-follow");
scrollWrites.length = 0;
scrollExtent = 1_100;
rowElement.getBoundingClientRect = () => rectAt(99);
await act(async () => arbiter?.observeReaderExtent());
check(scrollWrites.some((write) => write.owner === "reader-stability"), "tail-follow handoff retains one active-frame reader correction");
scrollWrites.length = 0;
await advanceClock(200);
rowElement.getBoundingClientRect = () => rectAt(-99);
await act(async () => arbiter?.observeReaderExtent());
check(!scrollWrites.some((write) => write.owner === "reader-stability"), "tail-follow handoff correction expires with the active reader frame");
scrollExtent = 500;
scrollElement.scrollTop = 400;
rowElement.getBoundingClientRect = () => rectAt(200);

// An upward gesture between at-bottom deliveries breaks the hold streak; the
// next downward gesture restarts it from zero.
await act(async () => arbiter?.releaseTailFollow());
await wheelDown();
check(arbiter?.modeRef.current === "reader-gesture", "one at-bottom delivery starts the hold without re-entering");
await wheel(-40);
scrollElement.scrollTop = 300;
await act(async () => arbiter?.deliverScroll());
scrollElement.scrollTop = 400;
await wheelDown();
check(arbiter?.modeRef.current === "reader-gesture", "an upward gesture between at-bottom deliveries breaks the hold");
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

// WebView2 may coalesce the entire final wheel segment: the native scroller is
// physically at the bottom, but no delivery observed it before the idle probe.
// Keep the expanded reader range for one frame so the hold completes before a
// manual-mode Virtuoso contraction can expose the old rows at a new offset.
await act(async () => arbiter?.releaseTailFollow());
scrollElement.scrollTop = 300;
await wheelDown();
scrollElement.scrollTop = 400;
await advanceClock(200);
check(arbiter?.modeRef.current === "reader-gesture", "a first idle bottom sample retains reader ownership through paint");
await flushFrames();
check(arbiter?.modeRef.current === "tail-follow", "the next painted bottom sample commits tail ownership before buffer release");

await act(async () => arbiter?.releaseTailFollow());
await wheelDown();
check(arbiter?.modeRef.current === "reader-gesture", "a fresh intent window rebuilds the bottom hold from zero");
await wheelDown();
check(arbiter?.modeRef.current === "tail-follow", "the fresh window re-enters tail-follow after its second delivery");

// A thumb gesture that reaches the native bottom claims the tail only after
// release has sampled two stable frames. A later real measurement is an
// explicit geometry revision rather than a scroll-delivery feedback write.
await act(async () => arbiter?.reset());
scrollElement.scrollTop = 0;
await act(async () => arbiter?.onPointerDownIntent({
  button: 0,
  nativeEvent: { button: 0, clientX: 795 },
} as React.PointerEvent<HTMLElement>));
scrollElement.scrollTop = 400;
await act(async () => arbiter?.deliverScroll());
await act(async () => window.dispatchEvent(new dom.window.Event("pointerup")));
await flushFrames();
await flushFrames();
check(arbiter?.modeRef.current === "tail-follow", "native thumb release after a held physical bottom resumes tail-follow");

// GTK can consume every pointermove and coalesce the away-and-back scroll
// range. The capture-phase pointerup still proves the native thumb travelled;
// combine that with the final physical-bottom geometry before claiming tail.
await act(async () => arbiter?.reset());
scrollElement.scrollTop = 400;
await act(async () => arbiter?.onPointerDownIntent({
  button: 0,
  clientY: 500,
  nativeEvent: { button: 0, clientX: 795, clientY: 500 },
} as React.PointerEvent<HTMLElement>));
await act(async () => window.dispatchEvent(new dom.window.MouseEvent("pointerup", { clientY: 452 })));
await flushFrames();
await flushFrames();
check(arbiter?.modeRef.current === "tail-follow", "release pointer travel proves a coalesced away-and-back native thumb");

// Some native themes preserve pointermove at the window boundary but report
// pointerup back at the original thumb coordinate. Retain that intermediate
// travel so an away-and-back bottom release cannot lose its explicit proof.
await act(async () => arbiter?.reset());
scrollElement.scrollTop = 400;
await act(async () => arbiter?.onPointerDownIntent({
  button: 0,
  clientY: 500,
  nativeEvent: { button: 0, clientX: 795, clientY: 500 },
} as React.PointerEvent<HTMLElement>));
await act(async () => window.dispatchEvent(new dom.window.MouseEvent("pointermove", { clientY: 452 })));
await act(async () => window.dispatchEvent(new dom.window.MouseEvent("pointerup", { clientY: 500 })));
await flushFrames();
await flushFrames();
check(arbiter?.modeRef.current === "tail-follow", "captured pointer travel survives an original-coordinate native release");

// Chromium's native gutter can report an original-coordinate PointerEvent,
// followed by the real release coordinate on the compatibility MouseEvent.
// Defer mouse-pointer termination so that coordinate remains movement proof.
await act(async () => arbiter?.reset());
scrollElement.scrollTop = 400;
await act(async () => arbiter?.onPointerDownIntent({
  button: 0,
  clientY: 500,
  nativeEvent: { button: 0, clientX: 795, clientY: 500 },
} as React.PointerEvent<HTMLElement>));
const nativePointerUp = new dom.window.MouseEvent("pointerup", { clientY: 500 });
Object.defineProperty(nativePointerUp, "pointerType", { value: "mouse" });
await act(async () => window.dispatchEvent(nativePointerUp));
check(scrollElement.dataset.nativeScrollbarDrag === "true", "mouse pointerup waits for the compatibility release coordinate");
await advanceClock(16);
check(scrollElement.dataset.nativeScrollbarDrag === "true", "a later-task GTK mouseup wins over the bounded fallback");
await act(async () => window.dispatchEvent(new dom.window.MouseEvent("mouseup", { clientY: 452 })));
await flushFrames();
await flushFrames();
check(arbiter?.modeRef.current === "tail-follow", "compatibility mouseup proves native thumb travel");

// A host that omits compatibility mouseup must not strand native-thumb mode.
await act(async () => arbiter?.reset());
scrollElement.scrollTop = 400;
await act(async () => arbiter?.onPointerDownIntent({ button: 0, clientY: 500, nativeEvent: { button: 0, clientX: 795, clientY: 500 } } as React.PointerEvent<HTMLElement>));
const cancelledMousePointer = new dom.window.MouseEvent("pointercancel", { clientY: 500 });
Object.defineProperty(cancelledMousePointer, "pointerType", { value: "mouse" });
await act(async () => window.dispatchEvent(cancelledMousePointer));
await advanceClock(64);
check(scrollElement.dataset.nativeScrollbarDrag === undefined, "missing compatibility mouseup falls back without stranding native-thumb mode");

// A real virtual range can reach the native bottom before its LAST row mounts.
// Keep a bounded release probe alive until that row becomes observable.
await act(async () => arbiter?.reset());
scrollElement.dataset.transcriptRowCount = "2";
scrollElement.dataset.transcriptFirstItemIndex = "0";
rowElement.dataset.itemIndex = "0";
scrollElement.scrollTop = 0;
await act(async () => arbiter?.onPointerDownIntent({
  button: 0,
  nativeEvent: { button: 0, clientX: 795 },
} as React.PointerEvent<HTMLElement>));
scrollElement.scrollTop = 400;
await act(async () => arbiter?.deliverScroll());
await act(async () => window.dispatchEvent(new dom.window.Event("pointerup")));
await flushFrames();
check(arbiter?.modeRef.current === "reader-gesture", "bottom release waits while the virtual tail is unmounted");
await advanceClock(250);
check(arbiter?.modeRef.current === "reader-gesture", "native bottom proof outlives the ordinary reader idle window");
rowElement.dataset.itemIndex = "1";
await flushFrames();
await flushFrames();
check(arbiter?.modeRef.current === "tail-follow", "bounded release sampling claims the newly mounted virtual tail");
delete scrollElement.dataset.transcriptRowCount;
delete scrollElement.dataset.transcriptFirstItemIndex;
delete rowElement.dataset.itemIndex;

scrollExtent = 900;
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.noteGeometryChange("row-measure"));
await advanceClock(80);
for (let i = 0; i < 6; i += 1) await flushFrames();
check(scrollElement.scrollTop === 800, "post-release remeasurement reconverges the claimed native bottom");
scrollExtent = 500;

// A WebView can clamp the first stable row-measure correction against the
// previous Virtuoso range even though scrollHeight exposes the new bottom.
// The same logical writer transaction confirms the restored native range on
// the following frame; the stable extent cannot start a residual loop.
await act(async () => arbiter?.reset());
scrollElement.scrollTop = 400;
scrollWrites.length = 0;
tailClampLag = 6;
scrollExtent = 700;
await act(async () => arbiter?.noteGeometryChange("row-measure"));
check(scrollElement.scrollTop === 400, "row measurement does not write before geometry is stable");
await advanceClock(80);
for (let i = 0; i < 6 && scrollElement.scrollTop === 400; i += 1) await flushFrames();
check(scrollElement.scrollTop === 594,
  `the fixture reproduces a stable-write clamp against the stale native range (${scrollElement.scrollTop})`);
await flushFrames();
check(scrollElement.scrollTop === 600, "the bounded native confirmation commits the restored tail range");
await flushFrames();
await flushFrames();
await advanceClock(160);
await advanceClock(80);
for (let i = 0; i < 6; i += 1) await flushFrames();
check(scrollElement.scrollTop === 600, "the stable native tail remains converged after residual verification");
for (let i = 0; i < 3; i += 1) await flushFrames();
const residualWrites = scrollWrites.filter((write) => write.owner === "tail-follow");
check(residualWrites.length === 1, `stable-height repair remains one logical writer transaction (${residualWrites.length})`);
check(
  new Set(residualWrites.map((write) => write.geometryRevision)).size === residualWrites.length,
  "the native confirmation does not fabricate a second geometry revision",
);
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

// A multi-viewport range replacement must not use an absolute scrollTop that
// can commit ahead of Virtuoso's mounted window. Preserve the sampled row at
// its prior viewport offset with one indexed transaction.
await act(async () => arbiter?.reset());
rowElement.dataset.itemIndex = "15";
scrollElement.scrollTop = 100;
scrollExtent = 1_200;
rowElement.getBoundingClientRect = () => rectAt(150 - scrollElement.scrollTop);
await act(async () => arbiter?.setMode("manual", "large-anchor-compensation"));
await act(async () => arbiter?.deliverScroll());
scrollWrites.length = 0;
rowElement.getBoundingClientRect = () => rectAt(-150 - scrollElement.scrollTop);
await act(async () => arbiter?.followGrowingTail());
for (let i = 0; i < 3; i += 1) await flushFrames();
const indexedCompensation = scrollWrites.filter((write) => write.owner === "anchor-compensation");
check(
  indexedCompensation.length === 1
    && indexedCompensation[0]?.kind === "scrollToIndex"
    && indexedCompensation[0]?.index === 15
    && indexedCompensation[0]?.offset === -50,
  "large manual anchor drift commits one viewport-preserving indexed transaction",
);
delete rowElement.dataset.itemIndex;

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

// A host scroll delivery can be accepted without WebKit input classification.
// Preserve that delivered range before the next replacement removes its
// leading row; the shared clipped boundary still proves the visual reversal.
await act(async () => arbiter?.reset());
scrollExtent = 500; scrollElement.scrollTop = 200;
rowElement.getBoundingClientRect = () => rectAt(-20);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
await wheel(24);
const nativeLeading = dom.window.document.createElement("div");
nativeLeading.className = "transcript__row"; nativeLeading.dataset.rowKey = "native-leading";
nativeLeading.getBoundingClientRect = () => rectAt(-95);
const nativeBoundary = dom.window.document.createElement("div");
nativeBoundary.className = "transcript__row"; nativeBoundary.dataset.rowKey = "native-boundary";
nativeBoundary.getBoundingClientRect = () => rectAt(-80);
rowElement.replaceWith(nativeLeading); scrollElement.append(nativeBoundary);
scrollElement.scrollTop += 24;
await act(async () => arbiter?.deliverScroll());
scrollWrites.length = 0;
nativeLeading.remove(); nativeBoundary.getBoundingClientRect = () => rectAt(18);
await act(async () => arbiter?.observeReaderExtent());
const nativeRangeWrites = scrollWrites.filter((write) => write.owner === "reader-stability");
check(nativeRangeWrites.length === 1
  && (nativeRangeWrites[0]!.top ?? 0) - (nativeRangeWrites[0]!.scrollTop ?? 0) === 98,
  "every accepted native delivery retains its boundary before replacement");
nativeBoundary.replaceWith(rowElement); rowElement.dataset.rowKey = "row-a";

// WebView2 can report a small opposite scrollTop delta in the same delivery
// that a measured range grows above the viewport. The shared painted rows are
// the stronger signal: keep the armed downward direction and repair the
// layout-owned reverse slide instead of reclassifying it as upward input.
{
  const originalUserAgent = dom.window.navigator.userAgent;
  const originalClientDescriptor = Object.getOwnPropertyDescriptor(scrollElement, "clientHeight")!;
  const originalScrollRect = scrollElement.getBoundingClientRect;
  const painted = Array.from({ length: 9 }, (_, index) => {
    const row = dom.window.document.createElement("div");
    row.className = "transcript__row";
    row.dataset.rowKey = `coalesced-${index}`;
    let top = 20 + index * 55;
    row.getBoundingClientRect = () => rectAt(top);
    scrollElement.append(row);
    return { row, move: (delta: number) => { top += delta; } };
  });
  Object.defineProperty(dom.window.navigator, "userAgent", {
    configurable: true,
    value: "Mozilla/5.0 AppleWebKit/537.36 Chrome/128 Safari/537.36 Edg/128",
  });
  Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 600 });
  scrollElement.getBoundingClientRect = () => ({ ...rectAt(0), height: 600, bottom: 600 });
  try {
    await act(async () => arbiter?.reset());
    scrollExtent = 24_305;
    scrollElement.scrollTop = 23_658;
    await act(async () => arbiter?.setMode("manual", "coalesced-layout-setup"));
    await act(async () => arbiter?.deliverScroll());
    await act(async () => arbiter?.releaseTailFollow(false, 40));
    scrollWrites.length = 0;

    scrollElement.scrollTop = 23_616;
    scrollExtent = 24_591;
    painted.forEach(({ move }) => move(328));
    await act(async () => arbiter?.deliverScroll());

    const coalescedWrites = scrollWrites.filter((write) => write.owner === "reader-stability");
    check(
      coalescedWrites.length === 1
        && (coalescedWrites[0]?.top ?? 0) - (coalescedWrites[0]?.scrollTop ?? 0) >= 300,
      "coalesced opposite scrollTop and extent growth preserves the armed reader direction",
    );

    // The passive five-second layout lease must not preserve that direction
    // after the 180ms input transaction ends. A later opposite native
    // delivery is a fresh gesture even when its Virtuoso range changes in the
    // same frame (the WKWebView smoke begins this way).
    await act(async () => arbiter?.reset());
    scrollExtent = 6_000;
    scrollElement.scrollTop = 2_000;
    painted.forEach(({ move }) => move(-328));
    await act(async () => arbiter?.setMode("manual", "expired-direction-setup"));
    await act(async () => arbiter?.deliverScroll());
    await act(async () => arbiter?.releaseTailFollow(false, -40));
    await advanceClock(200);
    scrollWrites.length = 0;

    scrollElement.scrollTop = 2_040;
    scrollExtent = 6_280;
    painted.forEach(({ move }) => move(-300));
    await act(async () => arbiter?.deliverScroll());

    check(
      scrollWrites.every((write) => write.owner !== "reader-stability"),
      "an expired opposite direction cannot turn fresh native input into a reader repair",
    );
  } finally {
    painted.forEach(({ row }) => row.remove());
    Object.defineProperty(dom.window.navigator, "userAgent", { configurable: true, value: originalUserAgent });
    Object.defineProperty(scrollElement, "clientHeight", originalClientDescriptor);
    scrollElement.getBoundingClientRect = originalScrollRect;
  }
}

// WebView2 can defer an accepted native correction. Changing range estimates
// during that gap must not alternate targets before the first acknowledgement.
await act(async () => arbiter?.reset());
scrollExtent = 500; scrollElement.scrollTop = 172;
rowElement.getBoundingClientRect = () => rectAt(-29);
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.releaseTailFollow());
await wheel(24); scrollWrites.length = 0;
const deferredNativeScrollTo = scrollElement.scrollTo;
scrollElement.scrollTo = () => {};
try {
  rowElement.getBoundingClientRect = () => rectAt(67);
  await act(async () => arbiter?.observeReaderExtent());
  rowElement.getBoundingClientRect = () => rectAt(90); scrollExtent += 60;
  await act(async () => arbiter?.observeReaderExtent());
  rowElement.getBoundingClientRect = () => rectAt(75); scrollExtent -= 20;
  await act(async () => arbiter?.observeReaderExtent());
  check(scrollWrites.filter((write) => write.owner === "reader-stability").length === 1,
    "an unacknowledged native correction suppresses alternating targets");
} finally {
  scrollElement.scrollTo = deferredNativeScrollTo;
}


// ── A frozen-offset measured-extent slide re-pins the reading position ─────
// When every shared row slides together across one observed frame while
// scrollTop stays frozen, that slide is pure above-window extent drift: one
// bounded reader-stability write must restore the painted positions without
// any direction-gated reverse classification.
{
  const originalClientDescriptor = Object.getOwnPropertyDescriptor(scrollElement, "clientHeight")!;
  Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 600 });
  const originalScrollRect = scrollElement.getBoundingClientRect;
  scrollElement.getBoundingClientRect = () => ({ ...rectAt(0), height: 600, bottom: 600 });
  try {
    await act(async () => arbiter?.reset());
    scrollExtent = 6_000;
    scrollElement.scrollTop = 2_000;
    scrollWrites.length = 0;
    rowElement.dataset.itemIndex = "11";
    let pinnedRowTop = 420;
    rowElement.getBoundingClientRect = () => rectAt(pinnedRowTop);
    await act(async () => arbiter?.releaseTailFollow());
    await wheelDown();
    check(arbiter?.modeRef.current === "reader-gesture", "the pin fixture starts from a live reader gesture");
    await flushFrames();
    await advanceClock(2);
    await flushFrames();

    pinnedRowTop -= 120;
    scrollExtent -= 120;
    await act(async () => arbiter?.observeReaderExtent());
    const pinWrites = scrollWrites.filter((write) => write.owner === "reader-stability");
    check(pinWrites.length === 1
      && pinWrites[0]?.kind === "scrollTo"
      && pinWrites[0]?.top === 1_880,
      "one frozen-offset extent slide synchronizes Virtuoso and native pixels without an indexed remount");
    const followupBefore = scrollWrites.filter((write) => write.owner === "reader-stability").length;
    await act(async () => arbiter?.observeReaderExtent());
    check(
      scrollWrites.filter((write) => write.owner === "reader-stability").length <= followupBefore + 1,
      "a settled pin does not replay against its acknowledged target",
    );

    await act(async () => arbiter?.reset());
    scrollExtent = 6_000;
    scrollElement.scrollTop = 2_000;
    pinnedRowTop = 100;
    scrollWrites.length = 0;
    await act(async () => arbiter?.releaseTailFollow());
    await wheel(-400);
    await flushFrames();
    pinnedRowTop += 400;
    await act(async () => arbiter?.observeReaderExtent());
    check(scrollWrites.every((write) => write.owner !== "reader-stability"),
      "a stable-extent range slide remains user-owned instead of cancelling upward input");
  } finally {
    scrollElement.getBoundingClientRect = originalScrollRect;
    Object.defineProperty(scrollElement, "clientHeight", originalClientDescriptor);
  }
}

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
