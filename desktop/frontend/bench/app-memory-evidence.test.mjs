import assert from "node:assert/strict";
import { test } from "node:test";
import { evidenceIntegrity, retainedCohorts, summarizeHeap } from "./app-memory-evidence.mjs";

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
