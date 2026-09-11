import { TranscriptMeasurementLedger } from "../lib/transcriptMeasurementLedger";
import { commitTranscriptWindowRange } from "../lib/transcriptWindowRange";
import { commitTranscriptWindowGeometry, findTranscriptMeasurementPublicationBoundary } from "../lib/transcriptWindowGeometry";
import assert from "node:assert/strict";
function ok(condition: unknown, label: string) { assert.ok(condition, label); console.log(`PASS ${label}`); }
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
const invalidBatch = commitTranscriptWindowGeometry({ ...geometryInput, previous: snapshot, measurementCommit: true });
assert.equal(invalidBatch.measurementCommitted, false, "an invalid prefix cannot acknowledge a pending batch");
backing[50].start = 5000;
const recoveredBatch = commitTranscriptWindowGeometry({ ...geometryInput, previous: invalidBatch, measurementCommit: true });
assert.equal(recoveredBatch.measurementCommitted, true, "a recovered valid prefix closes the pending batch");
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

// A safe future measurement must become painted geometry before native travel.
// Retaining the old prefix defers +24px until block 38 is already visible.
const baseline = Array.from({ length: 100 }, (_, index) => ({
  key: `turn:${index}`, index, start: index * 191, end: (index + 1) * 191, size: 191,
}));
const revised = baseline.map(item => ({ ...item,
  start: item.start + (item.index > 38 ? 24 : 0),
  end: item.end + (item.index >= 38 ? 24 : 0),
  size: item.size + (item.index === 38 ? 24 : 0),
}));
const futureInput = { ...geometryInput, measurements: baseline, candidate: baseline.slice(23, 61),
  structureRevision: "future-growth", scrollTop: 31 * 191, clientHeight: 596, totalSize: 19100 };
const beforePublication = commitTranscriptWindowGeometry(futureInput);
for (const candidate of [revised.slice(23, 61), revised.slice(70, 90), baseline.slice(23, 61)]) {
  const published = commitTranscriptWindowGeometry({ ...futureInput, previous: beforePublication,
    measurements: revised, candidate,
    totalSize: 19124, measurementCommit: true });
  assert.equal(published.prefix.items[39].start, revised[39].start,
    "approved post-viewport sizes enter the painted prefix in their publication transaction");
  assert.equal(published.range.totalSize, published.prefix.extent);
  for (const item of published.range.items) {
    assert.equal(item.start, published.prefix.items[item.index].start,
      "even a covering stale candidate takes placements from the published prefix");
  }
  assert.equal(published.mode, "windowed", "a stale candidate reconstructs from the published prefix");
  for (const index of [31, 32, 33, 34]) {
    assert.equal(published.prefix.items[index].start, baseline[index].start, "publication leaves visible reader coordinates unchanged");
  }
  let previous = published;
  for (const scrollTop of [38 * 191 + 144, 59 * 191]) {
    const advanced = commitTranscriptWindowGeometry({ ...futureInput, previous, scrollTop,
      measurements: revised, candidate: revised.slice(Math.floor(scrollTop / 191) - 8, Math.floor(scrollTop / 191) + 30), totalSize: 19124 });
    for (const item of advanced.range.items) {
      assert.equal(item.start, published.prefix.items[item.index].start,
        "native range advance cannot expose a deferred measurement shift");
    }
    previous = advanced;
  }
}
console.log("PASS measurement publication paints the complete prefix before native range advancement");

// Recorded GTK ordering: input reaches 61,577px while native top is 50,313px.
// Future DOM heights are available before native travel catches up.
const stagedLedger = new TranscriptMeasurementLedger();
const makePrefix = () => {
  let top = 0;
  return Array.from({ length: 650 }, (_, index) => {
    const key = `gtk:${index}`, size = stagedLedger.sizeFor(key, 171);
    const item = { key, index, start: top, end: top + size, size };
    top += size;
    return item;
  });
};
const gtkInitial = makePrefix();
const gtkMounted = gtkInitial.slice(282, 320);
const gtkDOM = gtkMounted.map(item => ({ index: item.index, top: item.start - 50_313 }));
const gtkBoundary = findTranscriptMeasurementPublicationBoundary({
  paintedItems: gtkMounted, domItems: gtkDOM, scrollTop: 50_313, clientHeight: 596,
});
assert.equal(gtkBoundary, 302, "actual viewport retains a publishable future suffix despite delayed native travel");
assert.equal(findTranscriptMeasurementPublicationBoundary({
  paintedItems: gtkMounted, domItems: gtkDOM, scrollTop: 60_000, clientHeight: 596,
}), undefined, "native progress after render rejects a stale mounted suffix");
assert.equal(findTranscriptMeasurementPublicationBoundary({
  paintedItems: gtkMounted, domItems: gtkDOM.map(item => ({ ...item, top: item.top - 600 })),
  scrollTop: 50_313, clientHeight: 596,
}), 305, "DOM movement advances the safe boundary beyond an older painted candidate");
assert.equal(findTranscriptMeasurementPublicationBoundary({
  paintedItems: gtkMounted, domItems: gtkDOM, scrollTop: 50_313, clientHeight: Number.NaN,
}), undefined, "invalid viewport geometry cannot authorize a batch");
stagedLedger.stage(gtkMounted.map(item => ({ key: item.key, size: 190 })));
const gtkPublished = stagedLedger.publishStaged(key => Number(key.slice(4)) >= gtkBoundary!);
assert.equal(gtkPublished.length, 18, "future measured rows publish while native ownership remains active");
const gtkMeasured = makePrefix();
for (const index of [294, 295, 296, 297]) {
  assert.equal(gtkMeasured[index].start, gtkInitial[index].start, "publication cannot move any common visible block");
}
const gtkBase = { ...geometryInput, candidate: gtkMounted, measurements: gtkInitial,
  totalSize: gtkInitial[gtkInitial.length - 1].end, structureRevision: "gtk-native-backlog", scrollTop: 50_313, clientHeight: 596 };
const gtkBefore = commitTranscriptWindowGeometry(gtkBase);
const gtkCommitted = commitTranscriptWindowGeometry({ ...gtkBase, previous: gtkBefore,
  candidate: gtkMounted, measurements: gtkMeasured, totalSize: gtkMeasured[gtkMeasured.length - 1].end, measurementCommit: true });
assert.equal(gtkCommitted.range.items.find(item => item.index === 303)?.size, 190,
  "an old covering candidate paints the measured suffix before native catch-up");
const catchupTop = gtkMeasured[302].start + 20;
const commonBeforeRelease = [302, 303, 304, 305].map(index => gtkMeasured[index].start - catchupTop);
// The sole anchor writer may reconcile measurements above the viewport after
// release, but all visible rows must retain their individual screen positions.
stagedLedger.publishStaged();
const gtkReleased = makePrefix();
const correctedTop = catchupTop + gtkReleased[302].start - gtkMeasured[302].start;
assert.deepEqual([302, 303, 304, 305].map(index => gtkReleased[index].start - correctedTop), commonBeforeRelease,
  "release after catch-up preserves all visible rows without the GTK 19/38px squeeze");
console.log("PASS viewport-owned publication survives native backlog, stale render and multi-row release");
