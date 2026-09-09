#!/usr/bin/env node

import { spawn } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync, createWriteStream } from "node:fs";
import { createTimings } from "./app-memory-timing.mjs";
import { once } from "node:events";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { startPreviewServer } from "./vite-preview-server.mjs";
import { chooseAppLayout } from "./app-page-actions.mjs";
import { attributeRetention, buildIdentity, evidenceIntegrity, retainedCohorts, screeningBlockers, summarizeHeap } from "./app-memory-evidence.mjs";
import { completeShard, verifyIdentity, MEMORY_FIXTURES, MEMORY_PROTOCOL } from "./app-memory-shards.mjs";

const frontendDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
process.env.PLAYWRIGHT_BROWSERS_PATH = !process.env.PLAYWRIGHT_BROWSERS_PATH || process.env.PLAYWRIGHT_BROWSERS_PATH === ".pw-browsers"
  ? path.join(frontendDir, ".pw-browsers")
  : process.env.PLAYWRIGHT_BROWSERS_PATH;
// Playwright reads PLAYWRIGHT_BROWSERS_PATH at module evaluation; import it
// only after the path normalization above.
const { chromium } = await import("playwright");

function integerEnv(name, fallback) {
  const value = Number(process.env[name]);
  return Number.isInteger(value) && value > 0 ? value : fallback;
}

const CYCLES = integerEnv("REASONIX_APP_MEMORY_CYCLES", 128);
const MIXED_CYCLES = integerEnv("REASONIX_APP_MEMORY_MIXED_CYCLES", 512);
const BASELINE_ATTEMPTS = integerEnv("REASONIX_APP_MEMORY_BASELINE_ATTEMPTS", 4);
const SHARD = process.env.REASONIX_APP_MEMORY_SHARD === undefined ? null : Number(process.env.REASONIX_APP_MEMORY_SHARD);
if (SHARD !== null && ![1, 2, 3].includes(SHARD)) throw new Error("memory shard must be 1, 2 or 3");
const PROCESSES = SHARD === null ? integerEnv("REASONIX_APP_MEMORY_PROCESSES", 3) : 1;
const preparedFile = process.env.REASONIX_APP_MEMORY_PREPARED;
const prepared = preparedFile ? JSON.parse(readFileSync(preparedFile, "utf8")) : null;
if (SHARD !== null && (!prepared || CYCLES !== 128 || MIXED_CYCLES !== 512)) throw new Error("memory shard requires the shared build and complete 128/512 protocol");
const PORT = integerEnv("REASONIX_APP_MEMORY_PORT", 4647);
const artifacts = path.resolve(process.env.REASONIX_APP_MEMORY_ARTIFACTS ?? path.join(frontendDir, "bench/app-memory-artifacts"));
mkdirSync(artifacts, { recursive: true });

const fixtures = MEMORY_FIXTURES;
const timings = createTimings();

async function ensureBuild() {
  if (prepared) {
    verifyIdentity(buildIdentity(frontendDir), prepared.identity);
    if (!prepared.executionId || prepared.identity.sourceSHA !== process.env.EXPECTED_SOURCE_SHA) throw new Error("prepared memory build belongs to another commit");
    return;
  }
  // Each run owns a fresh production build; an unverified dist is not evidence.
  await new Promise((resolve, reject) => {
    const child = spawn("pnpm", ["build"], { cwd: frontendDir, stdio: "inherit" });
    child.once("exit", (code) => code === 0 ? resolve() : reject(new Error(`pnpm build exited ${code}`)));
  });
}

async function settleFrames(page, count = 6) {
  await timings.measure("settle.frames", () => page.evaluate((frames) => new Promise((resolve) => {
    const tick = () => --frames <= 0 ? resolve() : requestAnimationFrame(tick);
    requestAnimationFrame(tick);
  }), count));
}

async function selectFixture(page, fixture) {
  const active = await timings.measure("navigation.active", () => page.locator(".project-tree__topic--active .project-tree__topic-label").textContent().catch(() => ""));
  if (active?.includes(fixture.label)) throw new Error(`invalid repeated navigation: ${fixture.label}`);
  await timings.measure("navigation.click", () => page.locator(`.project-tree__topic-main:has-text("${fixture.label}")`).click());
  // Sample a resting page, not a hover card whose 350ms timer races hydration.
  await timings.measure("navigation.pointer", () => page.mouse.move(0, 0));
  await timings.measure("navigation.ready", () => page.waitForFunction(({ label, marker }) => {
    const activeLabel = document.querySelector(".project-tree__topic--active .project-tree__topic-label")?.textContent ?? "";
    const transcript = document.querySelector(".transcript");
    return activeLabel.includes(label)
      && transcript?.dataset.transcriptHydrating === "false"
      && transcript.textContent?.includes(marker)
      && !document.querySelector(".transcript-navigation-overlay");
  }, fixture, { timeout: 45_000, polling: "raf" }));
  await settleFrames(page);
}

async function forceGc(cdp, page) {
  await timings.measure("gc.collect", () => cdp.send("HeapProfiler.collectGarbage"));
  await settleFrames(page, 2);
  await timings.measure("gc.collect", () => cdp.send("HeapProfiler.collectGarbage"));
  await settleFrames(page, 2);
  const [heap, dom, lifecycle, performance] = await Promise.all([
    cdp.send("Runtime.getHeapUsage"),
    cdp.send("Memory.getDOMCounters"),
    page.evaluate(() => window.__reasonixAppLifecycle?.snapshot()),
    page.evaluate(() => ({ entries: window.performance.getEntries().length, attachedElements: document.querySelectorAll("*").length })),
  ]);
  if (!lifecycle) throw new Error("App lifecycle probe was not published by the production build");
  return { heap, dom, lifecycle, performance };
}

async function enterSafety(page) {
  await selectFixture(page, fixtures.windowed);
  await page.evaluate(() => {
    const transcript = document.querySelector(".transcript");
    if (!(transcript instanceof HTMLElement)) throw new Error("transcript viewport missing");
    Object.defineProperty(transcript, "scrollHeight", { configurable: true, get: () => Number.NaN });
    transcript.dispatchEvent(new Event("scroll"));
  });
  await timings.measure("safety.ready", () => page.waitForFunction(() => (
    document.querySelector(".transcript__projection")?.getAttribute("data-transcript-safe-fallback") === "true"
  ), undefined, { timeout: 15_000, polling: "raf" }));
  await page.evaluate(() => {
    const transcript = document.querySelector(".transcript");
    if (transcript instanceof HTMLElement) delete transcript.scrollHeight;
  });
  await settleFrames(page);
}

async function heapSnapshot(cdp, name) {
  const file = path.join(artifacts, `${name}.heapsnapshot`);
  const output = createWriteStream(file);
  const listener = ({ chunk }) => output.write(chunk);
  cdp.on("HeapProfiler.addHeapSnapshotChunk", listener);
  try { await timings.measure("heap.capture", () => cdp.send("HeapProfiler.takeHeapSnapshot", { reportProgress: false, captureNumericValue: true })); }
  finally { cdp.off("HeapProfiler.addHeapSnapshotChunk", listener); output.end(); }
  await once(output, "finish");
  const summary = await timings.measure("heap.summarize", () => summarizeHeap(JSON.parse(readFileSync(file, "utf8"))));
  writeFileSync(path.join(artifacts, `${name}.summary.json`), JSON.stringify(summary, null, 2));
  return { file: path.basename(file), summary };
}

async function runProcess(index) {
  const browser = await chromium.launch({
    headless: true,
    args: ["--enable-precise-memory-info", "--disable-dev-shm-usage"],
  });
  const context = await browser.newContext({ viewport: MEMORY_PROTOCOL.viewport });
  const page = await context.newPage();
  const pageErrors = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  const cdp = await context.newCDPSession(page);
  try {
    await page.goto(`http://127.0.0.1:${PORT}/?mock=bench&bench=1&app-lifecycle-probe=1&bench-hydration=soak`, { waitUntil: "domcontentloaded" });
    await page.locator("textarea.composer__input:not([aria-hidden=true])").waitFor();
    await selectFixture(page, fixtures.geometry);
    await selectFixture(page, fixtures.full);
    await enterSafety(page);
    await selectFixture(page, fixtures.full);
    for (const [label, className] of [["Creation", "app--creation"], ["Workbench", "app--workbench"]]) {
      await chooseAppLayout(page, label, className);
      await page.mouse.move(0, 0);
      await settleFrames(page);
    }
    // Baseline and checkpoints share a post-navigation resting state. Layout
    // controls can still own transient listeners immediately after closing, so
    // one early reading can sit above the resting value and make every later
    // reading look displaced. Settle and require consecutive identical readings
    // before accepting the baseline; an unsettled baseline is reported instead
    // of being judged as drift.
    await selectFixture(page, fixtures.geometry);
    await selectFixture(page, fixtures.full);
    const samples = [];
    const baselineReadings = [];
    for (let attempt = 1; attempt <= BASELINE_ATTEMPTS; attempt++) {
      await settleFrames(page, 12);
      const reading = await forceGc(cdp, page);
      baselineReadings.push({ nodes: reading.dom.nodes, jsEventListeners: reading.dom.jsEventListeners });
      const previous = baselineReadings.at(-2);
      const stable = previous && previous.nodes === reading.dom.nodes && previous.jsEventListeners === reading.dom.jsEventListeners;
      if (stable || attempt === BASELINE_ATTEMPTS) {
        samples.push({ phase: "baseline", roundTrips: 0, baselineStable: Boolean(stable), baselineReadings, ...reading });
        process.stdout.write(`[app-memory] process=${index} phase=baseline stable=${Boolean(stable)} readings=${JSON.stringify(baselineReadings)}\n`);
        break;
      }
    }
    const snapshots = [await heapSnapshot(cdp, `${index}-baseline`)];
    for (const phase of ["full", "windowed", "safety", "mixed"]) {
      const count = phase === "mixed" ? MIXED_CYCLES : CYCLES;
      for (let round = 1; round <= count; round++) {
        const safety = phase === "safety" || phase === "mixed" && round % 3 === 0;
        if (safety) await enterSafety(page);
        else await selectFixture(page, phase === "full" || phase === "mixed" && round % 3 === 1 ? fixtures.geometry : fixtures.windowed);
        await selectFixture(page, fixtures.full);
        if (round % 32 === 0 || round === count) {
          const sample = { phase, roundTrips: round, ...await forceGc(cdp, page) };
          samples.push(sample);
          writeFileSync(path.join(artifacts, `${index}-samples.json`), JSON.stringify(samples, null, 2));
          process.stdout.write(`[app-memory] process=${index} phase=${phase} roundTrips=${round} nodes=${sample.dom.nodes} listeners=${sample.dom.jsEventListeners} tokens=${sample.lifecycle.liveRenderTokens}\n`);
        }
      }
      snapshots.push(await heapSnapshot(cdp, `${index}-${phase}`));
      writeFileSync(path.join(artifacts, "timings.json"), JSON.stringify(timings.snapshot(), null, 2));
    }
    // The classifier blocks on a displaced final tail, so the tail must be
    // measured at rest: mid-cleanup listener blips (614 vs the 512 baseline)
    // resolve a few tasks after the last navigation. Settle, GC, and take the
    // quiescent confirmation sample the verdict actually judges.
    await settleFrames(page, 12);
    const settled = { phase: "settled", roundTrips: MIXED_CYCLES, ...await forceGc(cdp, page) };
    samples.push(settled);
    writeFileSync(path.join(artifacts, `${index}-samples.json`), JSON.stringify(samples, null, 2));
    process.stdout.write(`[app-memory] process=${index} phase=settled nodes=${settled.dom.nodes} listeners=${settled.dom.jsEventListeners} tokens=${settled.lifecycle.liveRenderTokens}\n`);
    return {
      process: index,
      browser: browser.version(),
      samples,
      snapshots,
      cohorts: retainedCohorts(samples),
      attribution: "pending",
      checks: {
        evidenceIntegrity: evidenceIntegrity(samples),
        instrumentedOperationsReleased: samples.every(sample => sample.lifecycle.activeOperations === 0),
        noPageErrors: pageErrors.length === 0,
      },
      metrics: { pageErrors },
    };
  } finally {
    await context.close();
    await browser.close();
  }
}

await ensureBuild();
const preview = await startPreviewServer(frontendDir, PORT);
const report = { identity: buildIdentity(frontendDir), fixtures, protocol: MEMORY_PROTOCOL, startedAt: new Date().toISOString(), cycles: CYCLES, mixedCycles: MIXED_CYCLES,
  ...(SHARD === null ? {} : { shard: { id: SHARD, total: 3, executionId: prepared.executionId } }), processes: [] };
try {
  for (let index = 1; index <= PROCESSES; index += 1) {
    const result = await runProcess(SHARD ?? index);
    result.attribution = attributeRetention(result.samples, result.cohorts);
    report.processes.push(result);
    process.stdout.write(`[app-memory] process ${index}: ${JSON.stringify({ checks: result.checks, attribution: result.attribution, metrics: result.metrics })}\n`);
  }
} catch (error) {
  report.failure = error.message;
} finally {
  await preview.close();
}
report.finishedAt = new Date().toISOString();
report.timings = timings.snapshot();
writeFileSync(path.join(artifacts, "timings.json"), JSON.stringify(report.timings, null, 2));
process.stdout.write(`[app-memory] timings ${JSON.stringify(report.timings)}\n`);
report.protocolComplete = CYCLES >= 128 && MIXED_CYCLES >= 512 && report.processes.length >= 3;
report.shardComplete = SHARD !== null && completeShard(report);
// The automated gate passes on clean screening: protocol complete, every
// integrity/release/page-error check true, and no disqualifying attribution
// reason. Heap-retainer and control attribution stays an offline duty
// recorded in each run's attribution reasons.
report.verdict = !report.failure && (report.protocolComplete || report.shardComplete)
  && report.processes.every((run) => Object.values(run.checks).every(Boolean)
    && screeningBlockers(run.attribution?.reasons ?? ["missing-attribution"]).length === 0)
  ? SHARD === null ? "PASS" : "SHARD_PASS" : report.failure ? "FAIL" : "NEEDS_ATTRIBUTION";
writeFileSync(path.join(artifacts, "report.json"), JSON.stringify(report, null, 2));
process.stdout.write(`[app-memory] verdict ${report.verdict}\n`);
process.exitCode = report.verdict === "PASS" || report.verdict === "SHARD_PASS" ? 0 : 1;
