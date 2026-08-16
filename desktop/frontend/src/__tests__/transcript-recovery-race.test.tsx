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

const rectAt = (top: number) => ({ top, bottom: top + 100, height: 100, left: 0, right: 800, width: 800, x: 0, y: top, toJSON: () => ({}) });

const scrollElement = dom.window.document.getElementById("scroll") as HTMLDivElement;
const rowElement = scrollElement.querySelector<HTMLElement>(".transcript__row")!;
scrollElement.getBoundingClientRect = () => rectAt(0);
rowElement.getBoundingClientRect = () => rectAt(200);
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 100 });
Object.defineProperty(scrollElement, "scrollHeight", { configurable: true, value: 500 });

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
  scrollTo: () => { scrollToCalls += 1; },
  getState: (callback: (state: StateSnapshot) => void) => {
    if (stubSnapshot) callback(stubSnapshot);
  },
} as unknown as VirtuosoHandle;
let arbiter: ReturnType<typeof useTranscriptScrollArbiter> | undefined;
let integrity: ReturnType<typeof useTranscriptLayoutIntegrity> | undefined;

function Probe({ surfaceKey, rows = baseRows }: { surfaceKey: string; rows?: TranscriptRow[] }) {
  const scroll = useTranscriptScrollArbiter({
    onRecoveryTerminal: (terminal) => { terminals.push(terminal); },
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
    captureStateSnapshot: scroll.captureStateSnapshot,
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
  arbiter!.scrollerRef(scrollElement);
});
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

// ── Blank-recovery cooldown: immediate re-blank is blocked, a persistent
// blank past the cooldown earns another rebuild
await advanceClock(2_100);
await triggerWatchdogRebuild();
check(integrity?.resetKey === "surface-c:3", "a later blank rebuilds the size tree again");
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
check(integrity?.resetKey === "surface-c:4", "a blank that persists past the cooldown earns another rebuild");
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

// ── T6: a snapshot captured before the keyed remount restores when the row
// keys still match, and is discarded when they do not.
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

// Same-tab reveal (new surface, same rows): the snapshot applies and the
// first-mount scrollToBottom is suppressed — it would yank the restored
// view straight back to the tail.
await switchSurface("surface-m");
check(integrity?.restoreSnapshot === stubSnapshot, "same-row surface remount offers the captured snapshot");
readyRef.current = false;
const scrollToBottomBeforeSnapshot = scrollToBottomCalls;
await act(async () => integrity?.handleItemsRendered(1));
await flushFrames();
check(scrollToBottomCalls === scrollToBottomBeforeSnapshot, "a snapshot-restored mount does not jump to the bottom");

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
check(scrollToBottomCalls === scrollToBottomBeforeSnapshot + 1, "changed data falls back to normal first-mount positioning");

// Different session (disjoint keys): the snapshot is discarded and the
// first mount settles at the bottom as before.
const foreignRows: TranscriptRow[] = [{ kind: "answer", key: "row-elsewhere", item: { ...item, id: "elsewhere" } }];
await switchSurface("surface-l", foreignRows);
check(integrity?.restoreSnapshot === undefined, "a disjoint key sequence discards the snapshot");
readyRef.current = false;
await act(async () => integrity?.handleItemsRendered(1));
await flushFrames();
check(scrollToBottomCalls === scrollToBottomBeforeSnapshot + 2, "a disjoint snapshot-less first mount settles at the bottom");
stubSnapshot = null;

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
