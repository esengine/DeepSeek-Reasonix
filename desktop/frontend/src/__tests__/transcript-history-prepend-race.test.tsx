// Run: tsx src/__tests__/transcript-history-prepend-race.test.tsx

import React, { act, useLayoutEffect } from "react";
import { createRoot } from "react-dom/client";
import type { ListItem, VirtuosoHandle } from "react-virtuoso";
import { useTranscriptGeometryLifecycle } from "../lib/useTranscriptGeometryLifecycle";
import { createTranscriptHistoryPrependCoordinator } from "../lib/transcriptHistoryPrependLease";
import { useTranscriptScrollArbiter } from "../lib/useTranscriptScrollArbiter";
import type { TranscriptScrollWriteRecord } from "../lib/transcriptScrollProbe";
import type { TranscriptRow } from "../lib/transcriptRows";
import type { HistoryMutation, Item } from "../lib/useController";
import { installTranscriptRaceClock } from "./helpers/transcriptRaceClock";
import { installTranscriptRecoveryRaceDom } from "./helpers/transcriptRecoveryRaceDom";

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

console.log("\ntranscript history-prepend race");
const idleCoordinator = createTranscriptHistoryPrependCoordinator();
const unrelatedLayoutTransient = { current: true };
idleCoordinator.bind({
  layoutTransientRef: unrelatedLayoutTransient,
  publishPending: () => {}, holdReaderGeometryCommit: () => {}, readerAnchorIsMounted: () => true,
  readerTransactionIsActive: () => false, commitGeometry: () => {},
});
idleCoordinator.noteReaderTerminal(true);
check(unrelatedLayoutTransient.current, "an idle prepend coordinator cannot clear another layout transaction");
const { dom, flushFrames } = installTranscriptRecoveryRaceDom();
const { advanceClock, restore: restoreClock } = installTranscriptRaceClock(dom.window as unknown as Window);
const scrollWrites: TranscriptScrollWriteRecord[] = [];
dom.window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => { scrollWrites.push(write); };

const rectAt = (top: number) => ({
  top, bottom: top + 100, height: 100, left: 0, right: 800,
  width: 800, x: 0, y: top, toJSON: () => ({}),
});
const scrollElement = dom.window.document.getElementById("scroll") as HTMLDivElement;
const rowElement = scrollElement.querySelector<HTMLElement>(".transcript__row")!;
scrollElement.getBoundingClientRect = () => rectAt(0);
rowElement.getBoundingClientRect = () => rectAt(0);
rowElement.dataset.index = "0";
Object.defineProperty(scrollElement, "clientHeight", { configurable: true, value: 100 });
let scrollExtent = 16_369;
Object.defineProperty(scrollElement, "scrollHeight", { configurable: true, get: () => scrollExtent });
Object.defineProperty(scrollElement, "scrollTop", { configurable: true, writable: true, value: 15_000 });

let scrollByCalls = 0;
let scrollToIndexCalls = 0;
const virtuosoHandle = {
  scrollBy: (options?: { top?: number }) => {
    scrollByCalls += 1;
    scrollElement.scrollTop += options?.top ?? 0;
  },
  scrollToIndex: () => { scrollToIndexCalls += 1; },
  scrollTo: (options?: { top?: number }) => { scrollElement.scrollTop = options?.top ?? scrollElement.scrollTop; },
  getState: () => {},
} as unknown as VirtuosoHandle;

const item: Item = { kind: "assistant", id: "row-a", text: "answer", reasoning: "", streaming: false };
const traceRow = (key: string): TranscriptRow => ({ kind: "answer", key, item: { ...item, id: key } });
const currentRows = [traceRow("row-a"), ...Array.from({ length: 100 }, (_, index) => traceRow(`current-${index}`))];
const pageOne = [...Array.from({ length: 70 }, (_, index) => traceRow(`page-1-${index}`)), ...currentRows];
const pageTwo = [...Array.from({ length: 70 }, (_, index) => traceRow(`page-2-${index}`)), ...pageOne];
const renderedItems = (rows: TranscriptRow[], count = rows.length): ListItem<TranscriptRow>[] => rows
  .slice(0, count)
  .map((data, index) => ({ data, index, offset: index * 100, size: 100 }));

let arbiter: ReturnType<typeof useTranscriptScrollArbiter> | undefined;
let geometry: ReturnType<typeof useTranscriptGeometryLifecycle> | undefined;
function RenderedRange({ rows, count, deliver }: {
  rows: TranscriptRow[];
  count: number;
  deliver: (rendered: ListItem<TranscriptRow>[]) => void;
}) {
  useLayoutEffect(() => deliver(renderedItems(rows, count)), [count, deliver, rows]);
  return null;
}
function Probe({ rows, historyMutation, deliveredCount }: {
  rows: TranscriptRow[];
  historyMutation: HistoryMutation;
  deliveredCount?: number;
}) {
  const scroll = useTranscriptScrollArbiter();
  geometry = useTranscriptGeometryLifecycle({
    virtualRowCount: rows.length,
    hydrating: false,
    readerTransactionActive: scroll.readerTransactionActive,
    historyMutation,
    historyPrependLease: scroll.historyPrependLease,
    scrollModeRef: scroll.modeRef,
    followGrowingTail: scroll.followGrowingTail,
    revalidateTail: scroll.revalidateTail,
    reconcileLogicalFocus: () => {},
    handleRecoveryItemsRendered: () => {},
    scheduleActiveQuestionSync: () => {},
    markSurfaceItemsRendered: () => {},
  });
  arbiter = scroll;
  return deliveredCount == null ? null : (
    <RenderedRange rows={rows} count={deliveredCount} deliver={geometry.handleItemsRendered} />
  );
}

const root = createRoot(dom.window.document.getElementById("root")!);
await act(async () => root.render(<Probe rows={currentRows} historyMutation={{ seq: 0, kind: "replace" }} />));
await act(async () => {
  (arbiter!.virtuosoRef as { current: VirtuosoHandle | null }).current = virtuosoHandle;
  arbiter!.scrollerRef(scrollElement);
  arbiter!.reset();
  arbiter!.deliverScroll();
});
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false, deltaX: 0, deltaY: -40, target: scrollElement,
} as React.WheelEvent<HTMLElement>));
scrollWrites.length = 0;
const firstGeneration = arbiter!.historyPrependLease.begin(0);

scrollExtent = 56_008;
await act(async () => root.render(<Probe rows={pageOne} deliveredCount={101} historyMutation={{ seq: 1, kind: "prepend" }} />));
rowElement.dataset.index = "70";
await act(async () => geometry?.handleTotalListHeightChanged());
await advanceClock(100);
await flushFrames();
check(arbiter?.historyPrependLease.pendingRef.current, "101/171 mounted rows keep the first prepend pending");
check(scrollByCalls === 0 && scrollToIndexCalls === 0, "partial first-page coverage emits no correction");

scrollExtent = 26_046;
await act(async () => geometry?.handleItemsRendered(renderedItems(pageOne)));
await advanceClock(160);
for (let frame = 0; frame < 3; frame += 1) await flushFrames();
const secondGeneration = arbiter!.historyPrependLease.begin(1);
check(secondGeneration === firstGeneration, "the second page inherits the active prepend generation");

scrollExtent = 65_721;
await act(async () => root.render(<Probe rows={pageTwo} deliveredCount={171} historyMutation={{ seq: 2, kind: "prepend" }} />));
rowElement.dataset.index = "140";
rowElement.getBoundingClientRect = () => rectAt(-12_299);
scrollExtent = 69_204;
await advanceClock(80);
await act(async () => geometry?.handleTotalListHeightChanged());
scrollExtent = 46_235;
await advanceClock(80);
await act(async () => geometry?.handleItemsRendered(renderedItems(pageTwo)));
await advanceClock(160);
for (let frame = 0; frame < 4; frame += 1) await flushFrames();
check(arbiter?.historyPrependLease.pendingRef.current, "171/241 coverage and late geometry cannot release early");
check(scrollByCalls === 0 && scrollToIndexCalls === 0, "101 to 171 to 241 spends zero early writers");
check(scrollElement.dataset.transcriptReaderVisualGuard === "true", "the old surface stays visually guarded");

await advanceClock(1_100);
for (let frame = 0; frame < 8; frame += 1) await flushFrames();
const prependWrites = scrollWrites.filter((write) => write.owner === "reader-stability");
check(arbiter?.historyPrependLease.pendingRef.current === false, "full stable coverage releases within the wall-clock budget");
check(prependWrites.length === 1 && prependWrites[0].kind === "scrollBy", "the stable key gets exactly one final correction");

// Continuous reader input may keep extending the reader transaction, but it
// cannot extend a covered prepend's independent wall-clock commit bound. The
// old shared deadline left physical-bottom readers permanently manual: every
// wheel postponed the geometry commit that tail handoff was waiting for.
await act(async () => arbiter?.reset());
scrollExtent = 46_235;
scrollElement.scrollTop = 45_000;
rowElement.getBoundingClientRect = () => rectAt(0);
rowElement.dataset.transcriptLastRow = "true";
await act(async () => arbiter?.deliverScroll());
await act(async () => arbiter?.setMode("manual", "test-continuous-reader-prepend"));
await act(async () => arbiter?.onWheelIntent({
  ctrlKey: false, deltaX: 0, deltaY: 640, target: scrollElement,
} as React.WheelEvent<HTMLElement>));
const continuousGeneration = arbiter!.historyPrependLease.begin(2);
await act(async () => root.render(
  <Probe rows={pageTwo} deliveredCount={pageTwo.length} historyMutation={{ seq: 3, kind: "prepend" }} />,
));
for (let gesture = 0; gesture < 10; gesture += 1) {
  await advanceClock(120);
  await act(async () => arbiter?.onWheelIntent({
    ctrlKey: false, deltaX: 0, deltaY: 640, target: scrollElement,
  } as React.WheelEvent<HTMLElement>));
  scrollElement.scrollTop = Math.min(scrollExtent - scrollElement.clientHeight, scrollElement.scrollTop + 120);
  await act(async () => arbiter?.deliverScroll());
}
check(arbiter?.historyPrependLease.pendingRef.current, "continuous wheels retain the prepend until a stable reader interval");
await advanceClock(181);
for (let frame = 0; frame < 10; frame += 1) await flushFrames();
check(arbiter?.historyPrependLease.pendingRef.current === false,
  "continuous wheels cannot postpone a covered prepend past its wall-clock bound");
check(arbiter?.historyPrependLease.generationRef.current === continuousGeneration,
  "the bounded commit completes the same prepend generation");
check(String(arbiter?.modeRef.current) === "tail-follow",
  "the physical-bottom reader hands ownership to the tail after the bounded commit");
delete rowElement.dataset.transcriptLastRow;

const competingOwners: [string, () => void][] = [
  ["selection", () => arbiter!.setMode("selection")],
  ["user resize", () => arbiter!.setMode("user-resize")],
  ["programmatic navigation", () => arbiter!.setMode("restoring")],
  ["jump bottom", () => arbiter!.scrollToBottom()],
  ["indexed jump", () => arbiter!.scrollToDataIndex(0)],
];
for (const [owner, takeOwnership] of competingOwners) {
  await act(async () => arbiter!.reset());
  await act(async () => arbiter!.onWheelIntent({
    ctrlKey: false, deltaX: 0, deltaY: -40, target: scrollElement,
  } as React.WheelEvent<HTMLElement>));
  const interruptedGeneration = arbiter!.historyPrependLease.begin(2);
  arbiter!.historyPrependLease.noteCoverage(interruptedGeneration, pageTwo.length, pageTwo.length);
  await act(async () => takeOwnership());
  check(!arbiter!.historyPrependLease.pendingRef.current, `${owner} cancels the covered prepend lease`);
}
await act(async () => arbiter!.reset());
arbiter!.historyPrependLease.begin(2);
await act(async () => arbiter!.setMode("selection"));
check(!arbiter!.historyPrependLease.pendingRef.current, "an explicit owner cancels a prepend without an active reader");

await act(async () => root.unmount());
restoreClock();
dom.window.close();
console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
