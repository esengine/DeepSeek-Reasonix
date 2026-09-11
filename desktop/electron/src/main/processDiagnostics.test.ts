import assert from "node:assert/strict";
import type { ProcessMetric } from "electron";
import { test } from "node:test";
import { ProcessDiagnostics } from "./processDiagnostics.js";

test("samples are bounded, redact metadata and preserve unavailable fields", () => {
  let now = 0;
  let calls = 0;
  const sampler = new ProcessDiagnostics(() => {
    calls++;
    return [{ pid: 42, creationTime: 1, type: "Tab", name: "private title", cpu: { percentCPUUsage: 2 }, memory: { workingSetSize: 2048 } }] as ProcessMetric[];
  }, () => now);
  const first = sampler.snapshot();
  assert.deepEqual(first.samples[0].processes, [{ pid: 42, creationTime: 1, type: "Tab", cpuPercent: null, workingSetMb: 2, privateMb: null }]);
  sampler.snapshot();
  assert.equal(calls, 1);
  for (let i = 1; i <= 20; i++) { now = i * 30_000; sampler.sample(); }
  const snapshot = sampler.snapshot();
  assert.ok(snapshot.samples.length <= 12);
  assert.ok(snapshot.samples.every((sample) => sample.ageMs <= 300_000));
  assert.equal(snapshot.samples.at(-1)?.intervalMs, 30_000);
  assert.equal(snapshot.samples.at(-1)?.processes[0].cpuPercent, 2);
  assert.equal(JSON.stringify(snapshot).includes("private title"), false);
});

test("unavailable and expired samples do not become zero readings", () => {
  let now = 0;
  let fail = false;
  const sampler = new ProcessDiagnostics(() => { if (fail) throw Error("gone"); return []; }, () => now);
  sampler.sample();
  fail = true;
  now = 301000;
  assert.deepEqual(sampler.snapshot().samples, []);
});

test("foreground samples at most twice per minute and background once per minute", () => {
  let now = 0;
  let foreground = true;
  let calls = 0;
  const sampler = new ProcessDiagnostics(() => { calls++; return []; }, () => now, () => foreground);
  sampler.sample();
  now = 29_999; sampler.snapshot();
  assert.equal(calls, 1);
  now = 30_000; sampler.sample();
  assert.equal(calls, 2);
  foreground = false;
  now = 60_000; sampler.sample();
  assert.equal(calls, 2);
  now = 90_000; sampler.sample();
  assert.equal(calls, 3);
});

test("growth requires sustained readings, uses private memory, and rejects PID reuse", () => {
  let now = 0;
  let memory = 100;
  let creationTime = 1;
  const sampler = new ProcessDiagnostics(() => [{ pid: 42, creationTime, type: "Tab", cpu: { percentCPUUsage: 0 }, memory: { workingSetSize: 900 * 1024, privateBytes: memory * 1024 } }] as ProcessMetric[], () => now);
  sampler.sample();
  now = 30_000; sampler.sample();
  memory = 400;
  now = 60_000; sampler.sample();
  assert.equal(sampler.snapshot().growth.length, 0);
  now = 90_000; sampler.sample();
  now = 120_000;
  assert.deepEqual(sampler.snapshot().growth, [{ pid: 42, type: "Tab", metric: "private", baselineMb: 100, currentMb: 400, durationMs: 120_000 }]);
  creationTime = 2;
  now = 150_000;
  assert.equal(sampler.snapshot().growth.length, 0);
  assert.equal(sampler.snapshot().samples.at(-1)?.processes[0].cpuPercent, null);
});
