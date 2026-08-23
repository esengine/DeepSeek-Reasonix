// Run: tsx src/__tests__/transcript-recovery-race.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import type { StateSnapshot, VirtuosoHandle } from "react-virtuoso";
import { useTranscriptScrollArbiter, type TranscriptRecoveryTerminal } from "../lib/useTranscriptScrollArbiter";
import { useTranscriptLayoutIntegrity } from "../lib/useTranscriptLayoutIntegrity";
import type { TranscriptScrollWriteRecord } from "../lib/transcriptScrollProbe";
import type { TranscriptRow } from "../lib/transcriptRows";
import type { Item } from "../lib/useController";

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

console.log("\ntranscript recovery races");

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

// Terminal-state capture: Transcript wires this into session diagnostics.
const terminals: TranscriptRecoveryTerminal[] = [];
const rowMeasurements: Array<{ rowKey: string; kind: TranscriptRow["kind"]; height: number; width: number }> = [];

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

const item: Item = { kind: "assistant", id: "a", text: "answer", reasoning: "", streaming: false };
const baseRows: TranscriptRow[] = [{ kind: "answer", key: "row-a", item }];
const readyRef = { current: true };
let scrollByCalls = 0;
let scrollToIndexCalls = 0;
let scrollToCalls = 0;
let scrollToBottomCalls = 0;
// Null disables snapshot capture; the snapshot sections opt in explicitly so
// the pre-snapshot scenarios keep their first-mount scrollToBottom behavior.
let stubSnapshot: StateSnapshot | null = null;
const virtuosoHandle = {
  scrollBy: () => { scrollByCalls += 1; },
  scrollToIndex: () => { scrollToIndexCalls += 1; },
  // Browser semantics: an offset write clamps against the current extent.
  scrollTo: (options?: { top?: number }) => {
    scrollToCalls += 1;
    const top = options?.top ?? 0;
    scrollElement.scrollTop = Math.max(0, Math.min(scrollExtent - scrollElement.clientHeight, top));
  },
  getState: (callback: (state: StateSnapshot) => void) => {
    if (stubSnapshot) callback(stubSnapshot);
  },
} as unknown as VirtuosoHandle;
let arbiter: ReturnType<typeof useTranscriptScrollArbiter> | undefined;
let integrity: ReturnType<typeof useTranscriptLayoutIntegrity> | undefined;

function Probe({ surfaceKey, rows = baseRows, layoutWidth = 800 }: { surfaceKey: string; rows?: TranscriptRow[]; layoutWidth?: number }) {
  const scroll = useTranscriptScrollArbiter({
    onRecoveryTerminal: (terminal) => { terminals.push(terminal); },
    onItemMeasured: (rowKey, kind, _layoutVariant, height, width) => { rowMeasurements.push({ rowKey, kind, height, width }); },
  });
  const layout = useTranscriptLayoutIntegrity({
    surfaceKey,
    rows,
    rowIndexByKey: new Map(rows.map((row, index) => [String(row.key), index])),
    scrollRef: scroll.scrollRef,
    pinnedRef: scroll.pinnedRef,
    readyRef,
    scrollToBottom: () => { scrollToBottomCalls += 1; },
    submitRecoveryRequest: scroll.submitRecoveryRequest,
    retryRecoveryRequest: scroll.retryRecoveryRequest,
    lastGoodAnchorRef: scroll.lastGoodAnchorRef,
    layoutTransientRef: scroll.layoutTransientRef,
    layoutWidth,
  });
  arbiter = scroll;
  integrity = layout;
  return null;
}

// Mirrors Transcript's surface-switch effect: the arbiter is reset, which
// cancels any in-flight recovery with reason "surface-switch".
async function switchSurface(surfaceKey: string, rows: TranscriptRow[] = baseRows) {
  await act(async () => root.render(<Probe surfaceKey={surfaceKey} rows={rows} />));
  await act(async () => { arbiter?.reset(); });
  await flushFrames();
}

// One scheduled blank check = one rAF pair. The watchdog only rebuilds after
// two consecutive idle blank sightings.
async function flushBlankCheck() {
  await act(async () => integrity?.scheduleBlankViewportCheck());
  await flushFrames();
  await flushFrames();
}

async function triggerWatchdogRebuild() {
  await flushBlankCheck();
  await flushBlankCheck();
}

const root = createRoot(dom.window.document.getElementById("root")!);
await act(async () => root.render(<Probe surfaceKey="surface-a" />));
await act(async () => {
  (arbiter!.virtuosoRef as { current: VirtuosoHandle | null }).current = virtuosoHandle;
});

// A first-mount bottom request may race the Virtuoso scroller ref. It must not
// strand the blank watchdog in a permanent layout-transient state.
await act(async () => arbiter?.scrollToBottom());
check(
  arbiter?.layoutTransientRef.current === false,
  "a pre-scroller tail request cannot strand layout-transient suppression",
);

await act(async () => {
  arbiter!.scrollerRef(scrollElement);
});

// itemSize is the measurement source of truth. data-known-size may still hold
// the estimate Virtuoso started from, so the cache callback must receive the
// returned DOM height instead.
rowElement.dataset.rowKind = "answer";
rowElement.dataset.transcriptLayoutVariant = "text-flow";
rowElement.dataset.knownSize = "291";
rowElement.getBoundingClientRect = () => ({ ...rectAt(200), height: 632, bottom: 832, width: 960, right: 960 });
rowMeasurements.length = 0;
arbiter?.itemSize(rowElement, "offsetHeight");
check(
  rowMeasurements.length === 1
    && rowMeasurements[0].rowKey === "row-a"
    && rowMeasurements[0].kind === "answer"
    && rowMeasurements[0].height === 632
    && rowMeasurements[0].width === 960,
  "itemSize publishes the real DOM height instead of data-known-size",
);
rowElement.getBoundingClientRect = () => rectAt(200);

// The native extent is authoritative even when Virtuoso reports a stale
// logical atBottom value after delayed row measurement.
scrollElement.scrollTop = 400;
await act(async () => arbiter?.atBottomStateChange(false));
check(arbiter?.isAtBottom === true, "physical bottom overrides a stale Virtuoso atBottom=false report");

// A nested code/tool scrollport owns the wheel until it reaches its edge.
// Capturing the event on Transcript must not release tail-follow early.
const nestedScroller = dom.window.document.createElement("div");
nestedScroller.style.overflowY = "auto";
Object.defineProperty(nestedScroller, "clientHeight", { configurable: true, value: 100 });
Object.defineProperty(nestedScroller, "scrollHeight", { configurable: true, value: 300 });
Object.defineProperty(nestedScroller, "scrollTop", { configurable: true, writable: true, value: 50 });
rowElement.appendChild(nestedScroller);
await act(async () => arbiter?.reset());
let nestedWheelAccepted = true;
await act(async () => {
  nestedWheelAccepted = arbiter?.onWheelIntent({
    ctrlKey: false,
    deltaX: 0,
    deltaY: -40,
    target: nestedScroller,
  } as React.WheelEvent<HTMLElement>) ?? true;
});
check(!nestedWheelAccepted && arbiter?.modeRef.current === "tail-follow", "a scrollable nested surface keeps wheel ownership");
nestedScroller.scrollTop = 0;
await act(async () => {
  nestedWheelAccepted = arbiter?.onWheelIntent({
    ctrlKey: false,
    deltaX: 0,
    deltaY: -40,
    target: nestedScroller,
  } as React.WheelEvent<HTMLElement>) ?? false;
});
check(nestedWheelAccepted && arbiter?.modeRef.current === "manual", "a nested edge hands wheel ownership to the transcript");
nestedScroller.remove();

// A queued confirmation belongs to the surface that requested it. Resetting
// before its frame runs must prevent the old request from writing the new one.
scrollToCalls = 0;
scrollElement.scrollTop = 0;
await act(async () => arbiter?.scrollToBottom());
check(scrollToCalls === 1, "bottom request performs its immediate native-extent write");
await act(async () => arbiter?.reset());
await flushFrames();
check(scrollToCalls === 1, "a reset invalidates the previous surface's queued tail confirmation");

// A jump-bottom transaction suppresses the blank watchdog while WebView2 and
// Virtuoso are still exchanging scroll/measurement frames. The diagnostic
// packages showed the old watchdog rebuilding inside this exact window.
const keyBeforeJumpBlank = integrity?.resetKey;
await act(async () => arbiter?.scrollToBottom());
await triggerWatchdogRebuild();
check(integrity?.resetKey === keyBeforeJumpBlank, "jump-bottom transients cannot trigger a blank size-tree rebuild");
await advanceClock(350);

// Tail-follow is a persistent mode, not a six-frame retry window. As long as
// growth keeps arriving (streaming), each height notification re-arms another
// coalesced convergence and the view keeps landing on the current bottom.
scrollToCalls = 0;
await act(async () => arbiter?.scrollToBottom());
for (let i = 0; i < 14; i += 1) {
  scrollExtent += 200;
  await advanceClock(40);
  await act(async () => arbiter?.followGrowingTail());
  await flushFrames();
}
for (let i = 0; i < 4; i += 1) await flushFrames();
check(scrollToCalls > 6, `tail convergence remains live beyond the former six-frame budget (${scrollToCalls} writes)`);
check(
  scrollElement.scrollTop === scrollExtent - scrollElement.clientHeight,
  "sustained growth still lands on the physical bottom after the burst ends",
);

// ── Reduced-motion flicker filter (#9028/#9089): layout churn alternates the
// extent between two values on consecutive frames. A tail displacement must
// survive a full frame before the writer acts, so pure oscillation earns zero
// writes; once the churn settles off-bottom, one write reconverges.
const churnBase = scrollExtent;
scrollToCalls = 0;
for (let i = 0; i < 8; i += 1) {
  scrollExtent = i % 2 === 0 ? churnBase + 700 : churnBase;
  await act(async () => arbiter?.followGrowingTail());
  await flushFrames();
}
check(scrollToCalls === 0, `alternating-extent churn earns zero tail writes (${scrollToCalls})`);
check(arbiter?.modeRef.current === "tail-follow", "churn does not revoke tail ownership");
scrollExtent = churnBase + 700;
await act(async () => arbiter?.followGrowingTail());
for (let i = 0; i < 4; i += 1) await flushFrames();
check(
  scrollElement.scrollTop === scrollExtent - scrollElement.clientHeight,
  "a settled post-churn displacement reconverges on the physical bottom",
);
check(scrollToCalls >= 1 && scrollToCalls <= 2, `post-churn convergence costs at most two writes (${scrollToCalls})`);

// Replay the returned Windows trace: several different extents become visible
// while one explicit jump owns the viewport. The writer may respond immediately
// and perform one final correction, but must not write once per intermediate
// height.
scrollToCalls = 0;
scrollExtent = 5_154;
scrollElement.scrollTop = 0;
await act(async () => arbiter?.scrollToBottom());
for (const extent of [3_467, 6_785, 7_728, 5_525, 4_869]) {
  scrollExtent = extent;
  scrollElement.scrollTop = Math.min(scrollElement.scrollTop, scrollExtent - scrollElement.clientHeight);
  await act(async () => arbiter?.followGrowingTail());
  await flushFrames();
}
await advanceClock(240);
for (let i = 0; i < 6; i += 1) await flushFrames();
check(scrollToCalls <= 2, `one jump-bottom transaction emits at most two effective writes (${scrollToCalls})`);
check(
  scrollElement.scrollTop === scrollExtent - scrollElement.clientHeight,
  `the bounded jump-bottom transaction still converges on the final native bottom (${scrollElement.scrollTop}/${scrollExtent - scrollElement.clientHeight})`,
);

scrollExtent = 500;
scrollElement.scrollTop = 400;
await act(async () => arbiter?.deliverScroll());

await act(async () => integrity?.scheduleBlankViewportCheck());
await switchSurface("surface-b");
check(integrity?.resetKey === "surface-b:0", "surface switch cancels the previous blank-viewport watchdog");

// ── Blank watchdog: two consecutive idle blank checks earn a rebuild (T8)
await act(async () => arbiter?.releaseTailFollow());
await flushBlankCheck();
check(integrity?.resetKey === "surface-b:0", "a single idle blank check does not rebuild (mount-lag guard)");
await flushBlankCheck();
check(integrity?.resetKey === "surface-b:1", "two consecutive idle blank checks schedule a controlled size-tree rebuild");
await act(async () => integrity?.handleItemsRendered(1));
terminals.length = 0;
await switchSurface("surface-c");
check(scrollByCalls === 0, "stale anchor correction cannot scroll the newly selected surface");
check(
  terminals.some((terminal) => terminal.outcome === "cancelled" && terminal.reason === "surface-switch"),
  "a surface switch cancels the in-flight recovery with an explicit terminal state",
);

// ── invalidateAnchors: user intent cancels an in-flight restore (#8657/#8688)
await act(async () => arbiter?.releaseTailFollow());
await triggerWatchdogRebuild();
check(integrity?.resetKey === "surface-c:2", "blank viewport rebuilds the size tree on the current surface");
scrollByCalls = 0;
scrollToIndexCalls = 0;
scrollToBottomCalls = 0;
await act(async () => integrity?.invalidateAnchors());
await act(async () => integrity?.handleItemsRendered(1));
await flushFrames();
check(scrollByCalls === 0, "invalidated anchor stops the restore correction loop");
check(scrollToIndexCalls === 0, "invalidated anchor never re-aims at the stale row");
check(scrollToBottomCalls === 1, "a reset without an anchor settles at the bottom");

// ── Blank-recovery generation: the same geometry may hard-reset only once.
// A real row-set or width change opens one new bounded recovery opportunity.
await advanceClock(2_100);
await triggerWatchdogRebuild();
check(integrity?.resetKey === "surface-c:2", "the same broken layout generation cannot enter a reset loop");
check(integrity?.safeMode === true, "a repeatedly blank generation enters one bounded measurement probe instead of another remount");
const safeModeResetKey = integrity?.resetKey;
rowElement.getBoundingClientRect = () => rectAt(0);
await flushBlankCheck();
check(integrity?.safeMode === false && integrity?.resetKey === safeModeResetKey,
  "a healthy measured viewport exits the bounded probe without remounting");
rowElement.getBoundingClientRect = () => rectAt(200);
await triggerWatchdogRebuild();
check(integrity?.safeMode === false && integrity?.resetKey === safeModeResetKey,
  "an exhausted generation cannot re-enter its measurement probe");
let recoveryRows = [...baseRows, { kind: "answer", key: "generation-1", item: { ...item, id: "generation-1" } } satisfies TranscriptRow];
await act(async () => root.render(<Probe surfaceKey="surface-c" rows={recoveryRows} />));
check(integrity?.safeMode === false, "a real layout generation change exits the automatic measurement fallback");
await triggerWatchdogRebuild();
check(integrity?.resetKey === "surface-c:3", "a changed row generation earns one new rebuild");
await act(async () => integrity?.handleItemsRendered(1));
// Let the in-flight restore converge: place the anchor row at its target
// offset so the correction loop settles within two stable frames (real DOMs
// converge after each scrollBy; the stubbed rects here do not move unless we
// move them, and the wall-clock budget would otherwise keep it alive).
rowElement.getBoundingClientRect = () => rectAt(0);
for (let i = 0; i < 10; i += 1) await flushFrames();
check(terminals.at(-1)?.outcome === "done", "a converged restore reports the done terminal state");
rowElement.getBoundingClientRect = () => rectAt(200);
scrollByCalls = 0;
await flushBlankCheck();
await flushBlankCheck();
check(integrity?.resetKey === "surface-c:3", "blank recovery within the cooldown window is ignored");
check(scrollByCalls === 0, "cooldown-blocked blank check performs no correction");
await advanceClock(2_100);
await triggerWatchdogRebuild();
check(integrity?.resetKey === "surface-c:3", "the revised generation also refuses a second hard reset");
recoveryRows = [...recoveryRows, { kind: "answer", key: "generation-2", item: { ...item, id: "generation-2" } } satisfies TranscriptRow];
await act(async () => root.render(<Probe surfaceKey="surface-c" rows={recoveryRows} />));
await triggerWatchdogRebuild();
check(integrity?.resetKey === "surface-c:4", "the next real layout generation can recover once");
await act(async () => integrity?.handleItemsRendered(1));
rowElement.getBoundingClientRect = () => rectAt(0);
for (let i = 0; i < 10; i += 1) await flushFrames();
rowElement.getBoundingClientRect = () => rectAt(200);

// ── The cooldown key carries no content revision: a patch storm inside the
// cooldown window cannot wear it down (T8)
let cooldownRows = baseRows;
for (let i = 0; i < 20; i += 1) {
  cooldownRows = [...cooldownRows, { kind: "answer", key: `cool-${i}`, item: { ...item, id: `cool-${i}` } }];
  await act(async () => root.render(<Probe surfaceKey="surface-c" rows={cooldownRows} />));
  if (i % 5 === 0) await flushBlankCheck();
}
await flushBlankCheck();
check(integrity?.resetKey === "surface-c:4", "a patch storm inside the cooldown window earns no rebuild");
await advanceClock(2_100);
await triggerWatchdogRebuild();
check(integrity?.resetKey === "surface-c:5", "the storm-worn blank rebuilds once the cooldown lapses");
await act(async () => integrity?.handleItemsRendered(1));
rowElement.getBoundingClientRect = () => rectAt(0);
for (let i = 0; i < 10; i += 1) await flushFrames();
rowElement.getBoundingClientRect = () => rectAt(200);

// ── Patch storm: content updates never remount and never write scroll (#8657)
// Simulates the ref-resolution patch burst of a long session: dozens of row
// updates landing while the user scrolls. The size tree must survive intact
// and the recovery path must stay silent the whole time.
await switchSurface("surface-d");
await act(async () => arbiter?.releaseTailFollow());
const keyBeforeStorm = integrity?.resetKey;
scrollWrites.length = 0;
scrollByCalls = 0;
scrollToIndexCalls = 0;
let stormRows = baseRows;
for (let i = 0; i < 50; i += 1) {
  stormRows = [...stormRows, { kind: "answer", key: `storm-${i}`, item: { ...item, id: `storm-${i}` } }];
  await act(async () => root.render(<Probe surfaceKey="surface-d" rows={stormRows} />));
  if (i % 7 === 0) await act(async () => integrity?.noteUserScrollIntent());
  if (i % 5 === 0) await flushFrames();
}
await flushFrames();
check(integrity?.resetKey === keyBeforeStorm, "a 50-patch content storm never remounts the size tree");
check(scrollByCalls === 0 && scrollToIndexCalls === 0, "the patch storm performs zero recovery scroll writes");
check(
  scrollWrites.every((write) => write.owner !== "recovery"),
  "the runtime probe records zero recovery-owned writes during the storm",
);
await advanceClock(350);
await flushBlankCheck();
check(integrity?.resetKey === keyBeforeStorm, "the first idle blank check after the storm does not rebuild yet");

// ── T5: a user scroll gesture mid-settling takes the recovery over
await advanceClock(2_100);
scrollByCalls = 0;
scrollToIndexCalls = 0;
scrollToBottomCalls = 0;
await flushBlankCheck();
check(integrity?.resetKey !== keyBeforeStorm, "watchdog rebuild still fires after the storm");
await act(async () => integrity?.handleItemsRendered(1));
await flushFrames();
check(scrollByCalls > 0 || scrollToIndexCalls > 0, "anchor restore is in flight after the watchdog rebuild");
// The user grabs the wheel mid-settling: the restore must cancel through the
// explicit user-takeover transition and adopt the user's position.
rowElement.getBoundingClientRect = () => rectAt(40);
terminals.length = 0;
await act(async () => integrity?.noteUserScrollIntent());
await act(async () => arbiter?.releaseTailFollow());
check(
  terminals.some((terminal) => terminal.outcome === "cancelled" && terminal.reason === "user-takeover"),
  "wheel intent mid-settling cancels recovery via user-takeover",
);
const lastGoodAfterTakeover = arbiter?.lastGoodAnchorRef.current;
check(
  lastGoodAfterTakeover?.mode === "manual" && lastGoodAfterTakeover.rowKey === "row-a" && lastGoodAfterTakeover.offset === 40,
  "user-takeover records the user's viewport anchor as lastGoodAnchor",
);
const frozenScrollBy = scrollByCalls;
const frozenScrollToIndex = scrollToIndexCalls;
await flushFrames();
await flushFrames();
await flushFrames();
check(scrollByCalls === frozenScrollBy && scrollToIndexCalls === frozenScrollToIndex, "no further recovery writes land after user-takeover");
await advanceClock(350);
rowElement.getBoundingClientRect = () => rectAt(200);

// ── Blank detection is gated while the user scrolls, armed again at idle
await switchSurface("surface-e");
await act(async () => arbiter?.releaseTailFollow());
const keySurfaceE = integrity?.resetKey;
await act(async () => integrity?.noteUserScrollIntent());
await flushBlankCheck();
check(integrity?.resetKey === keySurfaceE, "blank viewport during active user scrolling does not rebuild");
await advanceClock(350);
await flushFrames();
await flushFrames();
check(integrity?.resetKey === keySurfaceE, "the first idle blank check arms but does not rebuild");
await flushBlankCheck();
check(
  integrity?.resetKey !== keySurfaceE && integrity?.resetKey.startsWith("surface-e:"),
  "a blank confirmed by two consecutive idle checks earns a rebuild",
);

// ── Restore waits for a slow-mounting anchor row beyond the old 8-frame budget
await switchSurface("surface-f");
await act(async () => arbiter?.releaseTailFollow());
const keySurfaceF = integrity?.resetKey;
await triggerWatchdogRebuild();
check(integrity?.resetKey !== keySurfaceF, "rebuild armed for the slow-mount restore");
rowElement.remove();
scrollByCalls = 0;
scrollToIndexCalls = 0;
await act(async () => integrity?.handleItemsRendered(1));
for (let i = 0; i < 10; i += 1) await flushFrames();
check(scrollToIndexCalls > 8, "restore keeps re-aiming past the old 8-frame budget while the anchor row is unmounted");
check(scrollByCalls === 0, "no intermediate scrollBy lands while the anchor row is unmounted");
scrollElement.appendChild(rowElement);
rowElement.getBoundingClientRect = () => rectAt(50);
await flushFrames();
check(scrollByCalls > 0, "restore corrects once the anchor row mounts");
rowElement.getBoundingClientRect = () => rectAt(0);
await flushFrames();
await flushFrames();
await flushFrames();
const settledScrollBy = scrollByCalls;
const settledScrollToIndex = scrollToIndexCalls;
await flushFrames();
check(scrollByCalls === settledScrollBy && scrollToIndexCalls === settledScrollToIndex, "restore settles on the mounted anchor within the wall-clock budget");
check(terminals.at(-1)?.outcome === "done", "the settled slow-mount restore reports done");

// ── T8: the blank watchdog restores from lastGoodAnchor, not the nearest
// mounted row. The last recovery settled on row-a; the DOM now only mounts a
// stray row far below the viewport, so a nearest-row fallback would pick an
// unknown key and produce no restore location at all.
await advanceClock(2_100);
rowElement.remove();
const strayRow = dom.window.document.createElement("div");
strayRow.className = "transcript__row";
strayRow.dataset.rowKey = "row-stray";
strayRow.getBoundingClientRect = () => rectAt(600);
scrollElement.appendChild(strayRow);
await act(async () => root.render(<Probe surfaceKey="surface-f" layoutWidth={960} />));
await triggerWatchdogRebuild();
const watchdogLocation = integrity?.restoreLocation;
check(
  watchdogLocation !== undefined && typeof watchdogLocation === "object" && watchdogLocation.align === "start" && watchdogLocation.offset === 0,
  "blank watchdog anchors on lastGoodAnchor, not the nearest mounted row",
);
strayRow.remove();

// ── T4: budget expiry suspends the request (no intermediate landing), a
// bounded quiet-window retry keeps it from waiting forever for user input,
// and exhausted retries report terminal expired.
await switchSurface("surface-g");
scrollElement.appendChild(rowElement);
rowElement.getBoundingClientRect = () => rectAt(200);
await act(async () => arbiter?.releaseTailFollow());
await triggerWatchdogRebuild();
rowElement.remove();
scrollByCalls = 0;
scrollToIndexCalls = 0;
terminals.length = 0;
await act(async () => integrity?.handleItemsRendered(1));
await flushFrames();
check(scrollToIndexCalls === 1, "restore re-aims at the anchor row while it is unmounted");
await advanceClock(600);
await flushFrames();
await advanceClock(600);
await flushFrames();
check(scrollByCalls === 0, "zero intermediate scrollBy while the anchor row never mounts");
const frozenReaims = scrollToIndexCalls;
await flushFrames();
await flushFrames();
check(scrollToIndexCalls === frozenReaims, "budget expiry suspends the request instead of abandoning it mid-flight");
check(terminals.length === 0, "a suspended request reports no terminal state yet");
await advanceClock(350);
await flushFrames();
check(scrollToIndexCalls === frozenReaims + 1, "the quiet-window timer retries a suspended recovery without user input");
await advanceClock(1_100);
await flushFrames();
await advanceClock(350);
await flushFrames();
await advanceClock(1_100);
await flushFrames();
await advanceClock(350);
check(
  terminals.some((terminal) => terminal.outcome === "expired"),
  "after max retries the suspended request reports terminal expired",
);
check(scrollByCalls === 0, "the whole expired lifecycle emitted zero intermediate scrollBy");

// A real Transcript gesture first marks layout intent, then dispatches the
// arbiter event. The latter must cancel a suspended request and its automatic
// retry so scroll idle never steals the viewport back from the user.
await switchSurface("surface-h");
scrollElement.appendChild(rowElement);
rowElement.getBoundingClientRect = () => rectAt(200);
await act(async () => arbiter?.releaseTailFollow());
await triggerWatchdogRebuild();
rowElement.remove();
scrollToIndexCalls = 0;
terminals.length = 0;
await act(async () => integrity?.handleItemsRendered(1));
await flushFrames();
await advanceClock(1_100);
await flushFrames();
const reaimsBeforeTakeover = scrollToIndexCalls;
await act(async () => integrity?.noteUserScrollIntent());
await act(async () => arbiter?.releaseTailFollow());
check(
  terminals.some((terminal) => terminal.outcome === "cancelled" && terminal.reason === "user-takeover"),
  "the real Transcript user-intent order cancels a suspended recovery",
);
await advanceClock(350);
await flushFrames();
check(scrollToIndexCalls === reaimsBeforeTakeover, "a cancelled suspended recovery never retries after scroll idle");

// ── T10: entering selection mode mid-recovery cancels it; selection-edge
// scrolls are the only writes afterwards.
await switchSurface("surface-j");
scrollElement.appendChild(rowElement);
rowElement.getBoundingClientRect = () => rectAt(200);
await act(async () => arbiter?.releaseTailFollow());
await triggerWatchdogRebuild();
await act(async () => integrity?.handleItemsRendered(1));
scrollByCalls = 0;
scrollToIndexCalls = 0;
await flushFrames();
check(scrollByCalls > 0 || scrollToIndexCalls > 0, "recovery is in flight before selection begins");
terminals.length = 0;
await act(async () => arbiter?.setMode("selection", "cross-row-selection"));
check(
  terminals.some((terminal) => terminal.outcome === "cancelled" && terminal.reason === "user-takeover"),
  "entering selection mode mid-recovery cancels it via user-takeover",
);
scrollWrites.length = 0;
scrollByCalls = 0;
scrollToIndexCalls = 0;
let edgeWriteOk = false;
await act(async () => { edgeWriteOk = arbiter?.writeOffset("selection-edge-scroll", 120) ?? false; });
await flushFrames();
await flushFrames();
check(edgeWriteOk, "selection-edge scroll writes are accepted in selection mode");
check(scrollByCalls === 0 && scrollToIndexCalls === 0, "no recovery writes land after selection takes over");
check(
  scrollWrites.length > 0 && scrollWrites.every((write) => write.owner === "selection-edge-scroll"),
  "selection-edge scroll is the only writer afterwards",
);
let otherWriteOk = true;
await act(async () => { otherWriteOk = arbiter?.writeOffset("jump", 5) ?? true; });
check(!otherWriteOk, "non-selection writes stay rejected in selection mode");
scrollToIndexCalls = 0;
await act(async () => arbiter?.setMode("manual", "question-navigation"));
await act(async () => arbiter?.scrollToDataIndex(5));
check(scrollToIndexCalls === 1, "question navigation emits one indexed jump after its explicit selection cleanup");

// ── T6: surface switches reuse safe geometry through the LRU, never an old
// Virtuoso scrollTop. The blank watchdog also discards its broken tree.
stubSnapshot = {
  ranges: [{ startIndex: 0, endIndex: 0, size: 100 }, { startIndex: 1, endIndex: Infinity, size: 80 }],
  scrollTop: 420,
};
await switchSurface("surface-k");
await act(async () => arbiter?.releaseTailFollow());
await triggerWatchdogRebuild();
check(integrity?.restoreSnapshot === undefined, "watchdog rebuild discards the size tree it just declared broken");
await act(async () => integrity?.handleItemsRendered(1));
await flushFrames();

// Same-tab reveal (new surface, same rows) is still a view reset. It opens at
// the product-defined tail and does not restore an old reader position.
const scrollToBottomBeforeSnapshot = scrollToBottomCalls;
await switchSurface("surface-m");
check(integrity?.restoreSnapshot === undefined, "same-row surface remount does not restore an old scrollTop");
readyRef.current = false;
await act(async () => integrity?.handleItemsRendered(1));
await flushFrames();
check(scrollToBottomCalls === scrollToBottomBeforeSnapshot + 1, "same-row reveal follows normal tail positioning");

// ── T9: the incoming surface prepended older history since the capture;
// changed data/totalCount must discard the snapshot per Virtuoso's contract.
const prependedRows: TranscriptRow[] = [
  { kind: "answer", key: "older-1", item: { ...item, id: "older-1" } },
  { kind: "answer", key: "older-2", item: { ...item, id: "older-2" } },
  ...baseRows,
];
await switchSurface("surface-n", prependedRows);
check(integrity?.restoreSnapshot === undefined, "a prepended key sequence discards the captured snapshot");
readyRef.current = false;
await act(async () => integrity?.handleItemsRendered(1));
await flushFrames();
check(scrollToBottomCalls === scrollToBottomBeforeSnapshot + 2, "changed data falls back to normal first-mount positioning");

// Different session (disjoint keys): the snapshot is discarded and the
// first mount settles at the bottom as before.
const foreignRows: TranscriptRow[] = [{ kind: "answer", key: "row-elsewhere", item: { ...item, id: "elsewhere" } }];
await switchSurface("surface-l", foreignRows);
check(integrity?.restoreSnapshot === undefined, "a disjoint key sequence discards the snapshot");
readyRef.current = false;
await act(async () => integrity?.handleItemsRendered(1));
await flushFrames();
check(scrollToBottomCalls === scrollToBottomBeforeSnapshot + 3, "a disjoint snapshot-less first mount settles at the bottom");
stubSnapshot = null;

// A 10,000-row generation gets only one keyed reset and one bounded probe.
const longRows: TranscriptRow[] = Array.from({ length: 10_000 }, (_, index) => ({
  kind: "answer", key: `long-${index}`,
  item: { ...item, id: `long-${index}` },
}));
await switchSurface("surface-long", longRows);
await act(async () => arbiter?.releaseTailFollow());
await advanceClock(2_100);
const longResetBefore = integrity?.resetKey;
await triggerWatchdogRebuild();
const longResetAfter = integrity?.resetKey;
check(longResetAfter !== longResetBefore, "a 10,000-row generation spends its single hard-reset budget");
await act(async () => integrity?.invalidateAnchors());
await act(async () => integrity?.handleItemsRendered(1));
await triggerWatchdogRebuild();
check(integrity?.safeMode === true, "long-history recovery keeps the probe independent of total row count");
for (let frame = 0; frame < 3; frame += 1) await flushFrames();
check(integrity?.safeMode === false,
  "an unsuccessful long-history probe exits without another range or scroll event");
for (let cycle = 0; cycle < 3; cycle += 1) await triggerWatchdogRebuild();
check(integrity?.resetKey === longResetAfter && integrity?.safeMode === false,
  "repeated blank cycles cannot remount or re-enter the probe after the generation budget is exhausted");
const nextLongRows = [...longRows.slice(0, -1), { kind: "answer" as const, key: "long-next-generation",
  item: { ...item, id: "long-next-generation" } }];
await act(async () => root.render(<Probe surfaceKey="surface-long" rows={nextLongRows} />));
check(integrity?.safeMode === false, "a changed 10,000-row generation resets the probe state");
await advanceClock(2_100);
const nextGenerationResetBefore = integrity?.resetKey;
await triggerWatchdogRebuild();
check(integrity?.resetKey !== nextGenerationResetBefore, "a changed 10,000-row generation receives a fresh hard-reset budget");

await act(async () => root.unmount());
Date.now = originalDateNow;
dom.window.setTimeout = originalSetTimeout;
dom.window.clearTimeout = originalClearTimeout;
dom.window.close();

if (failed > 0) {
  console.error(`\n${failed} transcript recovery race test(s) failed; ${passed} passed.`);
  process.exit(1);
}
console.log(`\n${passed} transcript recovery race tests passed.`);
