import assert from "node:assert/strict";
import { test } from "node:test";
import { attributeRetention, evidenceIntegrity, retainedCohorts, screeningBlockers, summarizeHeap } from "./app-memory-evidence.mjs";

const sample = (ids, roundTrips) => ({ phase: "full", roundTrips, lifecycle: {
  liveRenderTokenIds: ids, liveRenderTokens: ids.length,
  activeOperations: 0, activeSubscriptions: 6, invariantViolations: 0, overflow: false,
} });
test("deliberately retained cohorts remain detectable even when totals are constant", () => {
  const samples = [sample([1, 2], 0), sample([3, 4], 32), sample([3, 5], 64), sample([3, 6], 96)];
  assert.equal(evidenceIntegrity(samples), true);
  assert.deepEqual(retainedCohorts(samples).at(-1).retainedPostBaseline, [3]);
});
test("probe overflow, duplicate IDs and cleanup underflow invalidate evidence", () => {
  for (const mutation of [{ overflow: true }, { invariantViolations: 1 }, { activeSubscriptions: -1 }, { liveRenderTokenIds: [1, 1] }]) {
    const value = sample([1, 2], 0);
    Object.assign(value.lifecycle, mutation);
    assert.equal(evidenceIntegrity([value]), false);
  }
});
test("stable owner counters alone cannot establish whole-App retention attribution", () => {
  const samples = [
    { ...sample([1, 2], 0), dom: { nodes: 6000, jsEventListeners: 500 } },
    { ...sample([1, 3], 32), dom: { nodes: 6024, jsEventListeners: 512 } },
    { ...sample([1, 4], 64), dom: { nodes: 6024, jsEventListeners: 512 } },
    { ...sample([1, 5], 96), dom: { nodes: 6024, jsEventListeners: 512 } },
  ];
  assert.equal(attributeRetention(samples).status, "needs-attribution");
  assert.ok(attributeRetention(samples).reasons.includes("heap-retainer-and-control-evidence-required"));
});
test("a stable tail cannot hide growth earlier in the post-GC sequence", () => {
  const samples = [6000, 6024, 6100, 6100, 6100].map((nodes, index) => ({
    ...sample([1, index + 2], index * 32), dom: { nodes, jsEventListeners: 500 },
  }));
  assert.ok(attributeRetention(samples).reasons.includes("post-gc-dom-or-listener-drift"));
});
test("missing or non-finite native counters are invalid evidence", () => {
  for (const dom of [undefined, {}, { nodes: NaN, jsEventListeners: 500 }, { nodes: 6000, jsEventListeners: -1 }]) {
    const samples = Array.from({ length: 4 }, (_, index) => ({ ...sample([1, index + 2], index * 32), dom }));
    assert.ok(attributeRetention(samples).reasons.includes("invalid-native-counters"));
  }
});
test("subscriptions retained after a round trip require attribution", () => {
  const samples = Array.from({ length: 4 }, (_, index) => ({
    ...sample([1, index + 2], index * 32), dom: { nodes: 6000, jsEventListeners: 500 },
  }));
  samples[1].lifecycle.activeSubscriptions++;
  assert.ok(attributeRetention(samples).reasons.includes("subscription-population-drift"));
});
test("persistent post-baseline cohorts remain a qualification blocker", () => {
  const samples = [
    { ...sample([1, 2], 0), dom: { nodes: 6000, jsEventListeners: 500 } },
    { ...sample([1, 3], 32), dom: { nodes: 6024, jsEventListeners: 512 } },
    { ...sample([1, 3], 64), dom: { nodes: 6024, jsEventListeners: 512 } },
    { ...sample([1, 3], 96), dom: { nodes: 6024, jsEventListeners: 512 } },
  ];
  assert.equal(attributeRetention(samples).status, "needs-attribution");
});
test("the automated gate blocks on screening failures, not the offline attribution duty", () => {
  const clean = Array.from({ length: 4 }, (_, index) => ({
    ...sample([1, index + 2], index * 32), dom: { nodes: 6024, jsEventListeners: 512 },
  }));
  assert.deepEqual(screeningBlockers(attributeRetention(clean).reasons), []);
  const drift = [6000, 6024, 6100, 6100].map((nodes, index) => ({
    ...sample([1, index + 2], index * 32), dom: { nodes, jsEventListeners: 500 },
  }));
  assert.deepEqual(screeningBlockers(attributeRetention(drift).reasons), ["post-gc-dom-or-listener-drift"]);
  assert.deepEqual(screeningBlockers(["missing-attribution"]), ["missing-attribution"]);
});
test("native objects are not automatically detached DOM", () => {
  const heap = { snapshot: { meta: {
    node_fields: ["type", "name", "id", "self_size", "detachedness"],
    node_types: [["native", "code"], [], [], [], []],
  } }, strings: ["HTMLDivElement", "compiled function"],
  nodes: [0, 0, 1, 64, 1, 0, 0, 2, 64, 2, 1, 1, 3, 128, 0] };
  const result = summarizeHeap(heap);
  assert.equal(result.categories.native.count, 2);
  assert.equal(result.categories.code.selfBytes, 128);
  assert.deepEqual(result.detached.HTMLDivElement.ids, [2]);
  heap.snapshot.meta.node_fields[4] = "unknown";
  assert.equal(summarizeHeap(heap).detachednessAvailable, false);
  assert.deepEqual(summarizeHeap(heap).detached, {});
});
