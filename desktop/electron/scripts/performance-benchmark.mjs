// Synthetic native A/B: identical renderer work in fresh Electron processes.
// This measures diagnostic overhead, not the original Windows user workload.
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { join, resolve } from "node:path";
import { execFileSync } from "node:child_process";
import { performanceFixture } from "./performance-fixture.mjs";

const rows = [];
const modes = ["off", "monitor", "capture"];
const artifactDir = resolve(import.meta.dirname, "../artifacts/performance");
mkdirSync(artifactDir, { recursive: true });
writeFileSync(join(artifactDir, "overhead.json"), JSON.stringify({ status: "running", rows }));
const metricSnapshot = (app) => app.evaluate(({ app }) => app.getAppMetrics().map((m) => ({
  pid: m.pid, creationTime: m.creationTime, type: m.type,
  cpuSeconds: m.cpu.cumulativeCPUUsage ?? null,
  cpuPercent: m.cpu.percentCPUUsage,
  workingSetMb: m.memory?.workingSetSize === undefined ? null : m.memory.workingSetSize / 1024,
})));
const sumMemory = (metrics) => metrics.every((m) => m.workingSetMb !== null) ? metrics.reduce((sum, m) => sum + m.workingSetMb, 0) : null;
try {
for (let trial = 0; trial < 3; trial++) {
  for (let index = 0; index < modes.length; index++) {
    const mode = modes[(index + trial) % modes.length];
    const fixture = await performanceFixture(mode !== "off", { benchmark: true });
    try {
      const { app, page, temp } = fixture;
      const rendererBuildSha256 = createHash("sha256").update(readFileSync(join(temp, "assets/workload.js"))).digest("hex");
      await page.evaluate(() => window.diagnosticFixture.run(1000));
      // Measure after the real monitor's 15s startup grace, and across the
      // native sampler's 30s tick. Short startup-only trials miss both costs.
      await page.waitForFunction(() => performance.now() >= 16_000);
      const before = await metricSnapshot(app);
      const started = performance.now();
      const [work, profile] = await Promise.all([
        page.evaluate(() => window.diagnosticFixture.run(16_000)),
        mode === "capture" ? page.evaluate(() => window.reasonixDesktop.native.captureRendererProfile()) : Promise.resolve(null),
      ]);
      const elapsedMs = performance.now() - started;
      const after = await metricSnapshot(app);
      if (mode === "capture" && profile.status !== "captured") throw new Error(`capture was ${profile.status}; invalid benchmark trial`);
      const cpuSeconds = after.every((m) => m.cpuSeconds !== null) && before.every((m) => m.cpuSeconds !== null)
        ? after.reduce((sum, m) => sum + Math.max(0, m.cpuSeconds - (before.find((b) => b.pid === m.pid && b.creationTime === m.creationTime)?.cpuSeconds ?? 0)), 0) : null;
      const metricCost = await app.evaluate(({ app }) => {
        const samples = [];
        for (let i = 0; i < 30; i++) { const start = performance.now(); app.getAppMetrics(); samples.push(performance.now() - start); }
        samples.sort((a, b) => a - b);
        return { p95Ms: samples[Math.floor(samples.length * .95)], maxMs: samples[samples.length - 1] };
      });
      const row = { trial, mode, rendererBuildSha256, ...work, elapsedMs, cpuSeconds, workingSetBeforeMb: sumMemory(before), workingSetAfterMb: sumMemory(after), metricCost, profileStatus: profile?.status ?? "off" };
      rows.push(row);
      console.log(JSON.stringify(row));
    } finally { await fixture.close(); }
  }
}
if (new Set(rows.map((row) => row.rendererBuildSha256)).size !== 1) throw new Error("renderer bundles differ between benchmark modes");
} catch (error) {
  writeFileSync(join(artifactDir, "overhead.json"), JSON.stringify({ status: "failed", rows, failure: String(error) }, null, 2));
  throw error;
}
const median = (values) => values.filter((v) => v !== null).sort((a, b) => a - b)[Math.floor(values.length / 2)] ?? null;
const summary = modes.map((mode) => {
  const subset = rows.filter((r) => r.mode === mode);
  return { mode, frames: median(subset.map((r) => r.frames)), frameP95Ms: median(subset.map((r) => r.frameP95Ms)), cpuSeconds: median(subset.map((r) => r.cpuSeconds)), workingSetAfterMb: median(subset.map((r) => r.workingSetAfterMb)), metricP95Ms: median(subset.map((r) => r.metricCost.p95Ms)) };
});
writeFileSync(join(artifactDir, "overhead.json"), JSON.stringify({
  status: "completed",
  platform: process.platform, arch: process.arch,
  sourceHead: execFileSync("git", ["rev-parse", "HEAD"], { encoding: "utf8", cwd: resolve(import.meta.dirname, "..") }).trim(),
  sourceStatus: execFileSync("git", ["status", "--short", "--", "src", "scripts", "../frontend/src"], { encoding: "utf8", cwd: resolve(import.meta.dirname, "..") }).trim(),
  scope: "Synthetic Electron workload with controlled foreground signals and background throttling disabled; three fresh-process trials per mode; heap snapshots excluded; production activity lifecycle separately tested by native smoke", rows, summary,
}, null, 2));
console.log(JSON.stringify({ summary }));
