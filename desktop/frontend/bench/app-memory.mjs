#!/usr/bin/env node

import { spawn } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync, createWriteStream } from "node:fs";
import { once } from "node:events";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { startPreviewServer } from "./vite-preview-server.mjs";
import { chooseAppLayout } from "./app-page-actions.mjs";
import { attributeRetention, buildIdentity, evidenceIntegrity, retainedCohorts, screeningBlockers, summarizeHeap } from "./app-memory-evidence.mjs";

const frontendDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
process.env.PLAYWRIGHT_BROWSERS_PATH = !process.env.PLAYWRIGHT_BROWSERS_PATH || process.env.PLAYWRIGHT_BROWSERS_PATH === ".pw-browsers"
  ? path.join(frontendDir, ".pw-browsers")
  : process.env.PLAYWRIGHT_BROWSERS_PATH;

function integerEnv(name, fallback) {
  const value = Number(process.env[name]);
  return Number.isInteger(value) && value > 0 ? value : fallback;
}

const CYCLES = integerEnv("REASONIX_APP_MEMORY_CYCLES", 128);
const MIXED_CYCLES = integerEnv("REASONIX_APP_MEMORY_MIXED_CYCLES", 512);
const PROCESSES = integerEnv("REASONIX_APP_MEMORY_PROCESSES", 3);
const PORT = integerEnv("REASONIX_APP_MEMORY_PORT", 4647);
const artifacts = path.resolve(process.env.REASONIX_APP_MEMORY_ARTIFACTS ?? path.join(frontendDir, "bench/app-memory-artifacts"));
mkdirSync(artifacts, { recursive: true });

const fixtures = {
  full: { label: "bench:small-6t", marker: "ASYNC LAYOUT EXPANSION COMPLETE" },
  geometry: { label: "bench:geometry", marker: "Geometry contract fixture complete." },
  windowed: { label: "bench:windowed-1000t", marker: "Windowed turn 1000" },
};

async function ensureBuild() {
  // Each run owns a fresh production build; an unverified dist is not evidence.
  await new Promise((resolve, reject) => {
    const child = spawn("pnpm", ["build"], { cwd: frontendDir, stdio: "inherit" });
    child.once("exit", (code) => code === 0 ? resolve() : reject(new Error(`pnpm build exited ${code}`)));
  });
}

async function settleFrames(page, count = 6) {
  await page.evaluate((frames) => new Promise((resolve) => {
    const tick = () => --frames <= 0 ? resolve() : requestAnimationFrame(tick);
    requestAnimationFrame(tick);
  }), count);
}

async function selectFixture(page, fixture) {
  const active = await page.locator(".project-tree__topic--active .project-tree__topic-label").textContent().catch(() => "");
  if (active?.includes(fixture.label)) throw new Error(`invalid repeated navigation: ${fixture.label}`);
  await page.locator(`.project-tree__topic-main:has-text("${fixture.label}")`).click();
  await page.waitForFunction(({ label, marker }) => {
    const activeLabel = document.querySelector(".project-tree__topic--active .project-tree__topic-label")?.textContent ?? "";
    const transcript = document.querySelector(".transcript");
    return activeLabel.includes(label)
      && transcript?.dataset.transcriptHydrating === "false"
      && transcript.textContent?.includes(marker)
      && !document.querySelector(".transcript-navigation-overlay");
  }, fixture, { timeout: 45_000, polling: "raf" });
  await settleFrames(page);
}

async function forceGc(cdp, page) {
  await cdp.send("HeapProfiler.collectGarbage");
  await settleFrames(page, 2);
  await cdp.send("HeapProfiler.collectGarbage");
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
  await page.waitForFunction(() => (
    document.querySelector(".transcript__projection")?.getAttribute("data-transcript-safe-fallback") === "true"
  ), undefined, { timeout: 15_000, polling: "raf" });
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
  try { await cdp.send("HeapProfiler.takeHeapSnapshot", { reportProgress: false, captureNumericValue: true }); }
  finally { cdp.off("HeapProfiler.addHeapSnapshotChunk", listener); output.end(); }
  await once(output, "finish");
  const summary = summarizeHeap(JSON.parse(readFileSync(file, "utf8")));
  writeFileSync(path.join(artifacts, `${name}.summary.json`), JSON.stringify(summary, null, 2));
  return { file: path.basename(file), summary };
}

async function runProcess(index) {
  const browser = await chromium.launch({
    headless: true,
    args: ["--enable-precise-memory-info", "--disable-dev-shm-usage"],
  });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const page = await context.newPage();
  const pageErrors = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  const cdp = await context.newCDPSession(page);
  try {
    await page.goto(`http://127.0.0.1:${PORT}/?mock=bench&bench=1&app-lifecycle-probe=1`, { waitUntil: "domcontentloaded" });
    await page.locator("textarea.composer__input:not([aria-hidden=true])").waitFor();
    await selectFixture(page, fixtures.geometry);
    await selectFixture(page, fixtures.full);
    await enterSafety(page);
    await selectFixture(page, fixtures.full);
    for (const [label, className] of [["Creation", "app--creation"], ["Workbench", "app--workbench"]]) {
      await chooseAppLayout(page, label, className);
      await settleFrames(page);
    }
    const samples = [{ phase: "baseline", roundTrips: 0, ...await forceGc(cdp, page) }];
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
    }
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
const report = { identity: buildIdentity(frontendDir), fixtures, startedAt: new Date().toISOString(), cycles: CYCLES, mixedCycles: MIXED_CYCLES, processes: [] };
try {
  for (let index = 1; index <= PROCESSES; index += 1) {
    const result = await runProcess(index);
    result.attribution = attributeRetention(result.samples, result.cohorts);
    report.processes.push(result);
    process.stdout.write(`[app-memory] process ${index}: ${JSON.stringify({ checks: result.checks, metrics: result.metrics })}\n`);
  }
} catch (error) {
  report.failure = error.message;
} finally {
  await preview.close();
}
report.finishedAt = new Date().toISOString();
report.protocolComplete = CYCLES >= 128 && MIXED_CYCLES >= 512 && report.processes.length >= 3;
// The automated gate passes on clean screening: protocol complete, every
// integrity/release/page-error check true, and no disqualifying attribution
// reason. Heap-retainer and control attribution stays an offline duty
// recorded in each run's attribution reasons.
report.verdict = !report.failure && report.protocolComplete
  && report.processes.every((run) => Object.values(run.checks).every(Boolean)
    && screeningBlockers(run.attribution?.reasons ?? ["missing-attribution"]).length === 0)
  ? "PASS" : report.failure ? "FAIL" : "NEEDS_ATTRIBUTION";
writeFileSync(path.join(artifacts, "report.json"), JSON.stringify(report, null, 2));
process.stdout.write(`[app-memory] verdict ${report.verdict}\n`);
process.exitCode = report.verdict === "PASS" ? 0 : 1;
