import { createTranscriptHarness } from "./transcript-dom-harness";
import type { Item } from "../lib/useController";
import { commitTranscriptWindowRange, extractTranscriptWindowIndexes } from "../lib/transcriptWindowRange";
import { TranscriptViewportWriter } from "../lib/transcriptViewportWriter";
import { act } from "react";
import { commitTranscriptWindowGeometry } from "../lib/transcriptWindowGeometry";

let passed = 0;
let failed = 0;
function ok(condition: unknown, label: string) {
  if (condition) { process.stdout.write(`  PASS  ${label}\n`); passed += 1; }
  else { process.stdout.write(`  FAIL  ${label}\n`); failed += 1; }
}
function turns(count: number): Item[] {
  return Array.from({ length: count }, (_, index) => [
    { kind: "user", id: `user-${index}`, text: `question ${index}`, historyTurn: index + 1 } as Item,
    { kind: "assistant", id: `answer-${index}`, text: `answer ${index}`, reasoning: "", streaming: false } as Item,
  ]).flat();
}

console.log("\nTranscript viewport adapters");
const backing = Array.from({ length: 100 }, (_, index) => ({ key: `block:${index}`, index, start: index * 100, end: (index + 1) * 100, size: 100 }));
const lazyPrefix = new Proxy(new Array<(typeof backing)[number]>(100), {
  get: (target, key, receiver) => typeof key === "string" && /^\d+$/.test(key) ? backing[Number(key)] : Reflect.get(target, key, receiver),
});
const geometryInput = { candidate: backing.slice(5, 20), measurements: lazyPrefix, retainedIndexes: new Set<number>(),
  structureRevision: "prefix", scrollTop: 500, clientHeight: 800, scrollMargin: 0, totalSize: 10_000,
  maxItems: 38, direction: "forward" as const, gestureActive: true, residentCount: 2, forceFull: false };
const snapshot = commitTranscriptWindowGeometry(geometryInput);
ok(snapshot.mode === "windowed" && snapshot.prefix.items.length === 100 && snapshot.prefix.items[50].start === 5000,
  "lazy TanStack prefix is concretely materialized before geometry ownership");
backing[50].start = 4990;
ok(snapshot.prefix.items[50].start === 5000, "third-party cache mutation cannot alter a committed prefix snapshot");
const invalid = commitTranscriptWindowGeometry({ ...geometryInput, previous: snapshot });
ok(invalid.mode === "full" && invalid.prefix === snapshot.prefix,
  "invalid prefix enters covered full presentation using the immutable trusted geometry");
backing[50].start = 5000;
const previousRange = {
  structureRevision: "stable",
  scrollTop: 100,
  scrollMargin: 0,
  totalSize: 20_000,
  items: [{ index: 0, start: 50, end: 900 }],
  source: "candidate" as const,
  covered: true,
};
const staleCandidate = [{ index: 50, start: 5_000, end: 5_800 }];
const measurements = Array.from({ length: 200 }, (_, index) => ({ index, start: index * 100, end: (index + 1) * 100 }));
const shrunkBudget = commitTranscriptWindowRange({
  candidate: measurements.slice(0, 38), measurements, retainedIndexes: new Set([0]),
  previous: { ...previousRange, items: measurements.slice(0, 38) },
  structureRevision: "stable", scrollTop: 100, clientHeight: 200,
  scrollMargin: 0, totalSize: 20_000, maxItems: 5, direction: "forward", gestureActive: true,
});
ok(shrunkBudget.covered && shrunkBudget.items.length <= 5,
  "resident growth prunes stale overscan before judging total mount budget");
const retained = commitTranscriptWindowRange({
  candidate: staleCandidate,
  measurements,
  retainedIndexes: new Set(),
  previous: previousRange,
  structureRevision: "stable",
  scrollTop: 180,
  clientHeight: 600,
  scrollMargin: 0,
  totalSize: 20_000,
  maxItems: 8,
  direction: "forward",
  gestureActive: true,
});
ok(retained.items === previousRange.items, "a stale late range cannot replace native viewport coverage");
const measuredCandidate = [{ index: 0, start: 40, end: 940 }];
const measurementOnly = commitTranscriptWindowRange({
  candidate: measuredCandidate,
  measurements,
  retainedIndexes: new Set(),
  previous: previousRange,
  structureRevision: "stable",
  scrollTop: 100,
  clientHeight: 600,
  scrollMargin: 0,
  totalSize: 20_120,
  maxItems: 8,
  direction: "forward",
  gestureActive: true,
});
ok(measurementOnly.items === previousRange.items, "a measurement-only range commit stays frozen during native ownership");
ok(measurementOnly.totalSize === previousRange.totalSize, "a retained range keeps its matching extent snapshot");
const released = commitTranscriptWindowRange({
  candidate: measuredCandidate,
  measurements,
  retainedIndexes: new Set(),
  previous: measurementOnly,
  structureRevision: "stable",
  scrollTop: 100,
  clientHeight: 600,
  scrollMargin: 0,
  totalSize: 20_120,
  maxItems: 8,
  direction: "forward",
  gestureActive: false,
});
ok(released.items !== previousRange.items, "gesture release commits the latest covering measurements");
ok(released.totalSize === 20_120, "gesture release commits range and extent atomically");
const windowSource = await import("node:fs/promises").then((fs) => fs.readFile(new URL("../components/TranscriptWindow.tsx", import.meta.url), "utf8"));
ok(windowSource.includes("useCachedMeasurements: true"), "TanStack cannot publish ResizeObserver sizes outside the viewport commit protocol");
ok(windowSource.includes("measurementLedger.stage(changes)"), "DOM measurements enter the block-keyed staging ledger before publication");
ok(
  windowSource.includes("nativeViewport.clientHeight + publicationLeadPx")
    && windowSource.includes("domSafeIndex")
    && windowSource.includes("paintedSafeIndex == null || domSafeIndex == null")
    && windowSource.includes('kernel.intent === "reader"')
    && windowSource.includes("measurementLedger.publicationLead(kernel.userGestureActive)")
    && windowSource.includes('addEventListener("wheel", observeWheel')
    && windowSource.includes('["pointerdown", "mousedown"]')
    && windowSource.includes("addEventListener(type, beginUnbounded")
    && windowSource.includes('addEventListener("mouseup", endUnownedMouse')
    && windowSource.includes('addEventListener("touchstart", beginUnbounded')
    && windowSource.includes("measurementLedger.endGesture()")
    && windowSource.indexOf("measurementLedger.publicationLead(kernel.userGestureActive)") > windowSource.indexOf("const container = coldContainerRef.current")
    && windowSource.includes("[kernel.generation, kernel.userGestureActive, measurementLedger]")
    && windowSource.includes("measurementLedger.publishStaged(")
    && windowSource.includes("virtualizer.resizeItem(index, change.size);"),
  "reader measurements publish only beyond the prefix-and-DOM compositor frontier",
);
ok(!windowSource.includes("virtualizer.measure();"), "a safe suffix publish cannot invalidate and rebuild the protected prefix");
ok(windowSource.includes("measurementLedger.commit(residentChanges)"), "resident blocks publish exact sizes before leaving ordinary DOM");
const forwardIndexes = extractTranscriptWindowIndexes({ startIndex: 100, endIndex: 104, count: 1_000 }, new Set(), 36, "forward");
ok(forwardIndexes.length === 36 && forwardIndexes[0] === 96 && forwardIndexes.at(-1) === 131,
  "forward scrolling spends the bounded mount budget on compositor runway while retaining a reverse cushion");
const backwardIndexes = extractTranscriptWindowIndexes({ startIndex: 100, endIndex: 104, count: 1_000 }, new Set(), 36, "backward");
ok(backwardIndexes.length === 36 && backwardIndexes[0] === 73 && backwardIndexes.at(-1) === 108,
  "backward scrolling mirrors the bounded compositor runway");
ok(
  windowSource.includes("useSyncExternalStore(subscribe, getSnapshot, getSnapshot)")
    && windowSource.includes("scrollTop: nativeViewport.scrollTop")
    && windowSource.includes("clientHeight: nativeViewport.clientHeight")
    && windowSource.includes("getBoundingClientRect().top - scrollElement.getBoundingClientRect().top + nativeViewport.scrollTop"),
  "range commits use a tear-free native viewport snapshot instead of mutable render-time geometry",
);
const reconstructed = commitTranscriptWindowRange({
  candidate: staleCandidate,
  measurements,
  retainedIndexes: new Set([80]),
  structureRevision: "stable",
  scrollTop: 1_200,
  clientHeight: 600,
  scrollMargin: 0,
  totalSize: 20_000,
  maxItems: 8,
  direction: "forward",
  gestureActive: true,
});
ok(reconstructed.source === "reconstructed", "an uncovered native jump reconstructs from the prefix-size ledger");
ok(reconstructed.items.some((item) => item.start <= 1_200 && item.end >= 1_300), "the reconstructed range covers the native viewport");
ok(reconstructed.items.some((item) => item.index === 80), "reconstruction retains protected blocks");
const unavailable = commitTranscriptWindowRange({
  candidate: [],
  measurements: [],
  retainedIndexes: new Set(),
  structureRevision: "unavailable",
  scrollTop: 1_200,
  clientHeight: 600,
  scrollMargin: 0,
  totalSize: 20_000,
  maxItems: 36,
  direction: "forward",
  gestureActive: true,
});
ok(!unavailable.covered && unavailable.source === "unavailable" && unavailable.items.length === 0,
  "an unavailable ledger fails closed instead of painting an uncovered candidate");

const largeMeasurements = Array.from({ length: 10_000 }, (_, index) => ({ index, start: index * 96, end: (index + 1) * 96 }));
const rangeStartedAt = performance.now();
const largeRange = commitTranscriptWindowRange({
  candidate: [{ index: 2, start: 192, end: 288 }],
  measurements: largeMeasurements,
  retainedIndexes: new Set([9_999]),
  structureRevision: "10k",
  scrollTop: 720_000,
  clientHeight: 800,
  scrollMargin: 0,
  totalSize: 960_000,
  maxItems: 38,
  direction: "forward",
  gestureActive: true,
});
const rangeElapsedMs = performance.now() - rangeStartedAt;
ok(rangeElapsedMs < 1_000, `10,000-turn range reconstruction completes within 1s (${rangeElapsedMs.toFixed(1)}ms)`);
ok(largeRange.source === "reconstructed" && largeRange.items.length <= 40, "10,000-turn reconstruction keeps a bounded mounted range");
ok(largeRange.items.some((item) => item.start <= 720_000 && item.end >= 720_096), "10,000-turn reconstruction covers the authoritative viewport");
ok(largeRange.items.some((item) => item.index === 9_999), "10,000-turn reconstruction preserves protected block identity");
const harness = await createTranscriptHarness({ deterministic: true, viewportHeight: 800, rowHeight: 24 });
try {
  const writerTarget = document.createElement("div");
  let writerTop = 400;
  let physicalAssignments = 0;
  Object.defineProperties(writerTarget, {
    scrollTop: {
      configurable: true,
      get: () => writerTop,
      set: (value: number) => { physicalAssignments += 1; writerTop = value; },
    },
    scrollHeight: { configurable: true, get: () => 1_000 },
    clientHeight: { configurable: true, get: () => 600 },
  });
  const writer = new TranscriptViewportWriter();
  writer.attach(writerTarget, 7);
  const noOpWrite = writer.write({
    session: "writer-no-op",
    generation: 7,
    transactionId: 1,
    geometryRevision: 1,
    owner: "tail-follow",
    intent: "tail",
    offset: Number.POSITIVE_INFINITY,
  });
  ok(noOpWrite.accepted && noOpWrite.changed === false && physicalAssignments === 0, "writer commits an already-landed tail transaction without a redundant DOM assignment");

  await harness.render(turns(100), { geometrySessionKey: "threshold-101" });
  ok(harness.container.querySelector('[data-transcript-render-mode="full"]') != null, "100 completed turns render in full-DOM mode");
  ok(harness.container.querySelectorAll("[data-transcript-block-key]").length === 100, "full-DOM mode mounts every complete turn block");

  await harness.render(turns(101), { geometrySessionKey: "threshold-101" });
  await harness.waitFor(() => Boolean(harness.container.querySelector('[data-transcript-render-mode="windowed"]')), "window adapter loaded");
  await harness.settle();
  const projection = harness.container.querySelector<HTMLElement>('[data-transcript-render-mode="windowed"]');
  const mounted = Number.parseInt(projection?.dataset.transcriptMountedBlocks ?? "999", 10);
  ok(Boolean(projection), "101 completed turns switch to the TanStack window adapter");
  ok(mounted <= 40, `800px viewport mounts at most 40 completed blocks (${mounted})`);
  ok(Array.from(harness.container.querySelectorAll<HTMLElement>(".transcript__window-item"))
    .every((element) => element.style.position === "absolute" && Number.isFinite(Number.parseFloat(element.style.top)) && !element.style.transform),
  "mounted window blocks use native layout top rather than compositor transforms");
  ok(harness.container.querySelectorAll('[data-transcript-resident-tail="true"] [data-transcript-block-key]').length >= 2, "the two latest completed turns remain resident ordinary DOM");

  await harness.render(turns(135), { geometrySessionKey: "threshold-101" });
  await harness.settle();
  const grownProjection = harness.container.querySelector<HTMLElement>('.transcript__projection');
  const grownMode = grownProjection?.dataset.transcriptRenderMode;
  const grownMounted = Number.parseInt(grownProjection?.dataset.transcriptMountedBlocks ?? "999", 10);
  ok(grownMode === "windowed" && grownMounted <= 40,
    `normal growth must remain windowed within the total completed-block budget (${grownMode}:${grownMounted})`);

  const tailAction = harness.container.querySelector<HTMLButtonElement>(".transcript__jump-bottom");
  ok(Boolean(tailAction), "the jump-to-bottom action keeps a stable DOM host while hidden at the tail");
  const transcript = harness.scrollElement();
  await act(async () => {
    transcript.scrollTop = 0;
    transcript.dispatchEvent(new Event("scroll"));
  });
  await harness.waitFor(() => tailAction?.hidden === false, "jump-to-bottom visibility after reader takeover");
  ok(
    harness.container.querySelector(".transcript__jump-bottom") === tailAction,
    "reader takeover changes jump-to-bottom visibility without replacing its DOM identity",
  );
  await act(async () => {
    tailAction?.click();
  });
  await harness.waitFor(() => tailAction?.hidden === true, "jump-to-bottom visibility after tail restore");
  ok(
    transcript.scrollHeight - transcript.scrollTop - transcript.clientHeight <= 4,
    "the identity-stable jump-to-bottom action restores native tail geometry through the kernel",
  );

  const activeItems = [...turns(101), { kind: "user", id: "active-user", text: "active question", historyTurn: 102 } as Item];
  await harness.render(activeItems, { geometrySessionKey: "active", running: true, turnStartAt: Date.now() - 1_000 });
  await harness.settle();
  const active = harness.container.querySelector('[data-transcript-block-phase="active"]');
  ok(Boolean(active), "the current streaming turn is projected as one active block");
  ok(Boolean(active?.closest('[data-transcript-resident-tail="true"]')), "the active block stays outside the windowed history size ledger");
  ok(harness.container.querySelector(".transcript__live-status") != null, "an empty active process keeps its working status reachable");
  await harness.render(turns(135), { geometrySessionKey: "safety-reader" });
  await harness.settle();
  const reader = harness.scrollElement();
  await act(async () => {
    reader.dispatchEvent(new WheelEvent("wheel", { deltaY: -100, bubbles: true }));
    reader.scrollTop = reader.scrollHeight / 2;
    reader.dispatchEvent(new Event("scroll"));
  });
  await harness.flush();
  const visible = Array.from(reader.querySelectorAll<HTMLElement>("[data-transcript-block-key]"))
    .find((block) => block.getBoundingClientRect().bottom > 0 && block.getBoundingClientRect().top < 800)!;
  const top = visible.getBoundingClientRect().top;
  const key = visible.dataset.transcriptBlockKey;
  const focusButton = visible.querySelector<HTMLButtonElement>("button");
  const textNode = document.createTreeWalker(visible, 4).nextNode()!;
  await act(async () => {
    focusButton?.focus();
    document.getSelection()?.setBaseAndExtent(textNode, 0, textNode, Math.min(3, textNode.textContent?.length ?? 0));
    document.dispatchEvent(new Event("selectionchange"));
  });
  const selected = document.getSelection()?.toString();
  const accepted: unknown[] = [];
  window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => { if (write.outcome === "accepted") accepted.push(write); };
  harness.dom.window.history.replaceState(null, "", "?transcriptRenderMode=full");
  await harness.render(turns(135), { geometrySessionKey: "safety-reader" });
  const retainedBlock = Array.from(reader.querySelectorAll<HTMLElement>("[data-transcript-block-key]"))
    .find((block) => block.dataset.transcriptBlockKey === key);
  ok(retainedBlock === visible, "full safety presentation preserves the visible native block host");
  ok(Boolean(focusButton) && document.activeElement === focusButton && Boolean(selected) && document.getSelection()?.toString() === selected,
    "full presentation retains focus, native selection, and the existing action host");
  ok(Math.abs((retainedBlock?.getBoundingClientRect().top ?? Infinity) - top) <= 4 && accepted.length === 0,
    "full presentation during native input preserves reader geometry without a program write");
  harness.dom.window.history.replaceState(null, "", "/");
  const oldObservers = harness.observers.filter(({ target }) => target.closest(".transcript") === reader);
  await harness.render(turns(135), { geometrySessionKey: "fresh-geometry" });
  await harness.settle();
  const diagnostics: unknown[] = [];
  window.__REASONIX_TRANSCRIPT_SCROLL_DIAGNOSTIC__ = (type, fields) => { if (type === "kernel") diagnostics.push(fields); };
  await act(async () => oldObservers.forEach(({ notify }) => notify()));
  await harness.settle();
  ok(diagnostics.length === 0, "queued old observers cannot advance or write the replacement surface geometry");

  const fresh = harness.scrollElement();
  const freshObservers = harness.observers.filter(({ target }) => target.isConnected && target.closest(".transcript") === fresh);
  Object.defineProperty(fresh, "scrollHeight", { configurable: true, get: () => Number.NaN });
  await act(async () => freshObservers.forEach(({ notify }) => notify()));
  await harness.settle();
  ok(harness.container.querySelector('[data-transcript-render-mode="full"]') != null,
    "repeated invalid native geometry locks the shared full presentation");
  delete (fresh as unknown as { scrollHeight?: number }).scrollHeight;
  await act(async () => freshObservers.forEach(({ notify }) => notify()));
  await harness.settle();
  ok(harness.container.querySelector('[data-transcript-render-mode="full"]') != null,
    "a healthy measurement cannot unlock generation-latched full mode");
  await harness.render(turns(135), { geometrySessionKey: "reset-geometry" });
  await harness.settle();
  ok(harness.container.querySelector('[data-transcript-render-mode="windowed"]') != null,
    "only a new surface generation restores the default window policy");
  for (const count of [1000, 10000]) {
    const completed = turns(count);
    const activeUser: Item = { kind: "user", id: `stream-${count}`, text: "streaming question", historyTurn: count + 1 };
    const props = { geometrySessionKey: `completion-${count}`, running: true };
    await harness.render([...completed, activeUser], props);
    await harness.settle();
    const activeHost = harness.container.querySelector<HTMLElement>('[data-transcript-block-phase="active"]')!;
    const activeKey = activeHost.dataset.transcriptBlockKey;
    const finished: Item[] = [...completed, activeUser,
      { kind: "assistant", id: `result-${count}`, text: "completed result", reasoning: "", streaming: false }];
    await harness.render(finished, { ...props, running: false });
    await harness.settle();
    await harness.render([...finished, { kind: "user", id: `next-${count}`, text: "next question", historyTurn: count + 2 }], props);
    await harness.settle();
    const host = Array.from(harness.container.querySelectorAll<HTMLElement>("[data-transcript-block-key]"))
      .find((block) => block.dataset.transcriptBlockKey === activeKey);
    const viewport = harness.scrollElement();
    ok(host === activeHost, `${count} turns: completion and next-round start preserve the resident native identity`);
    ok(harness.container.querySelector('[data-transcript-render-mode="windowed"]') != null
      && harness.container.querySelectorAll('[data-transcript-block-phase="completed"]').length <= 40,
    `${count} turns: stream completion stays windowed within the completed mount budget`);
    ok(Math.abs(viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight) <= 4,
      `${count} turns: stream completion settles at the native tail within 4px`);
  }
} finally {
  await harness.unmount();
  await harness.close();
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed) process.exit(1);
