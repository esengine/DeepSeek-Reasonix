import test from "node:test";
import assert from "node:assert/strict";
import { aggregateShards, completeShard, MEMORY_PROTOCOL, MEMORY_FIXTURES } from "./app-memory-shards.mjs";
const identity = { sourceSHA: "a".repeat(40), trackedDiffSHA256: "clean-diff", untrackedSourceSHA256: "clean-untracked", buildSHA256: "shared-build", node: "v24", platform: "linux", arch: "x64", sourceStatus: "" };
const manifest = { identity, protocol: MEMORY_PROTOCOL, executionId: "123:1" };
function report(id) {
  const sample = (phase, roundTrips) => ({ phase, roundTrips,
    lifecycle: { liveRenderTokenIds: [1], liveRenderTokens: 1, activeOperations: 0, activeSubscriptions: 2, overflow: false, invariantViolations: 0 },
    dom: { nodes: 10, jsEventListeners: 2 }, heap: { usedSize: 100 } });
  const samples = [sample("baseline", 0)];
  for (const phase of ["full", "windowed", "safety", "mixed"]) {
    for (let count = 32; count <= (phase === "mixed" ? 512 : 128); count += 32) samples.push(sample(phase, count));
  }
  samples.push(sample("settled", 512));
  return { identity: structuredClone(identity), fixtures: structuredClone(MEMORY_FIXTURES), protocol: structuredClone(MEMORY_PROTOCOL), shard: { id, total: 3, executionId: "123:1" }, cycles: 128, mixedCycles: 512,
    shardComplete: true, protocolComplete: false, verdict: "SHARD_PASS", processes: [{ process: id, browser: "chromium-fixed", samples,
      snapshots: ["baseline", "full", "windowed", "safety", "mixed"].map(phase => ({ file: `${id}-${phase}.heapsnapshot`, summary: {} })),
      checks: { evidenceIntegrity: true, instrumentedOperationsReleased: true, noPageErrors: true }, metrics: { pageErrors: [] } }] };
}
const aggregate = reports => aggregateShards(reports, manifest, identity.sourceSHA);
test("three independent full shards preserve the complete protocol and offline attribution", () => {
  const result = aggregate([report(3), report(1), report(2)]);
  assert.equal(result.verdict, "PASS"); assert.equal(result.protocolComplete, true);
  assert.deepEqual(result.processes.map(run => run.process), [1, 2, 3]);
  assert.equal(result.attribution, "pending");
  assert.ok(result.processes.every(run => run.attribution.reasons.includes("heap-retainer-and-control-evidence-required")));
});
test("one complete process never satisfies the aggregate protocol", () => {
  assert.equal(completeShard(report(1)), true); assert.throws(() => aggregate([report(1)]), /three complete/);
});
for (const [name, mutate] of [
  ["duplicate shard", reports => { reports[2] = report(1); }],
  ["different commit", reports => { reports[1].identity.sourceSHA = "b".repeat(40); }],
  ["different build", reports => { reports[1].identity.buildSHA256 = "other-build"; }],
  ["dirty source", reports => { reports[1].identity.sourceStatus = " M source.ts"; }],
  ["another workflow attempt", reports => { reports[1].shard.executionId = "123:2"; }],
  ["different fixture", reports => { reports[1].fixtures.windowed.label = "short-fixture"; }],
  ["old hydration protocol", reports => { reports[1].protocol.version = 1; }],
  ["short cycles", reports => { reports[1].cycles = 127; }],
  ["missing checkpoint", reports => { reports[1].processes[0].samples.splice(5, 1); }],
  ["missing heap snapshot", reports => { reports[1].processes[0].snapshots.pop(); }],
  ["different browser", reports => { reports[1].processes[0].browser = "another-browser"; }],
  ["missing checks", reports => { reports[1].processes[0].checks = {}; }],
  ["page error", reports => { reports[1].processes[0].metrics.pageErrors.push("boom"); }],
  ["persistent DOM drift despite claimed pass", reports => { reports[1].processes[0].samples.at(-1).dom.nodes++; }],
  ["unfinished operations despite claimed pass", reports => { reports[1].processes[0].samples.at(-1).lifecycle.activeOperations++; }],
  ["missing token identity", reports => { delete reports[1].processes[0].samples[0].lifecycle.liveRenderTokenIds; }],
]) test(`aggregate rejects ${name}`, () => {
  const reports = [report(1), report(2), report(3)]; mutate(reports); assert.throws(() => aggregate(reports));
});
test("requested head and manifest protocol must match", () => {
  assert.throws(() => aggregateShards([report(1), report(2), report(3)], manifest, "wrong-head"));
  assert.throws(() => aggregateShards([report(1), report(2), report(3)], { ...manifest, protocol: { ...MEMORY_PROTOCOL, viewport: { width: 1, height: 1 } } }, identity.sourceSHA));
});
