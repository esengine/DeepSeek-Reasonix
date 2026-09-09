import { attributeRetention, evidenceIntegrity, retainedCohorts, screeningBlockers } from "./app-memory-evidence.mjs";

// Version 3 requires a settled, repeated baseline reading before the measured
// cycles, so a single early reading can no longer anchor the drift verdict.
export const MEMORY_PROTOCOL = Object.freeze({ version: 3, shards: 3, cycles: 128, mixedCycles: 512, hydration: "async-task", pointerRest: [0, 0], viewport: { width: 1440, height: 1000 } });
export const MEMORY_FIXTURES = Object.freeze({
  full: { label: "bench:small-6t", marker: "ASYNC LAYOUT EXPANSION COMPLETE" },
  geometry: { label: "bench:geometry", marker: "Geometry contract fixture complete." },
  windowed: { label: "bench:windowed-1000t", marker: "Windowed turn 1000" },
});
const identityFields = ["sourceSHA", "trackedDiffSHA256", "untrackedSourceSHA256", "buildSHA256", "node", "platform", "arch"];

export function verifyIdentity(actual, expected) {
  if (!actual || !expected || actual.sourceStatus !== "" || expected.sourceStatus !== "") throw new Error("memory evidence requires a clean source checkout");
  for (const field of identityFields) {
    if (typeof actual[field] !== "string" || !actual[field] || actual[field] !== expected[field]) throw new Error(`memory identity mismatch: ${field}`);
  }
}

export function protocolSamples(samples) {
  const expected = [["baseline", 0]];
  for (const phase of ["full", "windowed", "safety", "mixed"]) {
    const count = phase === "mixed" ? MEMORY_PROTOCOL.mixedCycles : MEMORY_PROTOCOL.cycles;
    for (let round = 32; round <= count; round += 32) expected.push([phase, round]);
  }
  expected.push(["settled", MEMORY_PROTOCOL.mixedCycles]);
  return Array.isArray(samples) && samples.length === expected.length
    && samples.every((sample, index) => sample.phase === expected[index][0] && sample.roundTrips === expected[index][1]);
}

export function completeShard(report) {
  const run = report.processes?.[0];
  return JSON.stringify(report.protocol) === JSON.stringify(MEMORY_PROTOCOL)
    && report.cycles === MEMORY_PROTOCOL.cycles && report.mixedCycles === MEMORY_PROTOCOL.mixedCycles
    && report.processes?.length === 1 && run.process === report.shard?.id
    && protocolSamples(run.samples)
    && Array.isArray(run.snapshots) && run.snapshots.length === 5
    && ["baseline", "full", "windowed", "safety", "mixed"].every((phase, index) =>
      run.snapshots[index].file === `${run.process}-${phase}.heapsnapshot` && run.snapshots[index].summary);
}

export function aggregateShards(reports, manifest, sourceSHA) {
  if (JSON.stringify(manifest.protocol) !== JSON.stringify(MEMORY_PROTOCOL)
    || manifest.identity?.sourceSHA !== sourceSHA || !manifest.executionId) throw new Error("invalid memory build manifest");
  if (!Array.isArray(reports) || reports.length !== 3) throw new Error("three complete independent memory shards are required");
  const seen = new Set();
  const processes = [];
  let browser;
  for (const report of reports) {
    const id = report.shard?.id;
    if (![1, 2, 3].includes(id) || seen.has(id)) throw new Error("duplicate or invalid memory shard");
    seen.add(id);
    if (report.shard.executionId !== manifest.executionId || report.shard.total !== 3) throw new Error("memory shard belongs to another workflow attempt");
    verifyIdentity(report.identity, manifest.identity);
    if (JSON.stringify(report.fixtures) !== JSON.stringify(MEMORY_FIXTURES)) throw new Error("memory fixture configuration differs");
    if (report.failure || report.verdict !== "SHARD_PASS" || !report.shardComplete || !completeShard(report)) throw new Error(`incomplete memory shard ${id}`);
    const run = report.processes[0];
    if (!run.browser || (browser && browser !== run.browser)) throw new Error("memory browser versions differ");
    browser = run.browser;
    const cohorts = retainedCohorts(run.samples);
    const attribution = attributeRetention(run.samples, cohorts);
    if (!evidenceIntegrity(run.samples) || !run.samples.every(sample => sample.lifecycle.activeOperations === 0)
      || !["evidenceIntegrity", "instrumentedOperationsReleased", "noPageErrors"].every(key => run.checks?.[key] === true)
      || !Array.isArray(run.metrics?.pageErrors) || run.metrics.pageErrors.length !== 0
      || screeningBlockers(attribution.reasons).length !== 0) throw new Error(`memory screening failed in shard ${id}`);
    processes.push({ ...run, cohorts, attribution });
  }
  return { identity: manifest.identity, executionId: manifest.executionId, protocol: MEMORY_PROTOCOL,
    protocolComplete: true, verdict: "PASS", attribution: "pending", processes: processes.sort((a, b) => a.process - b.process) };
}
