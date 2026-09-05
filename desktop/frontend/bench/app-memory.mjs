#!/usr/bin/env node

import { spawn } from "node:child_process";
import { existsSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { startPreviewServer } from "./vite-preview-server.mjs";

const frontendDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
process.env.PLAYWRIGHT_BROWSERS_PATH ||= path.join(frontendDir, ".pw-browsers");

function integerEnv(name, fallback) {
  const value = Number(process.env[name]);
  return Number.isInteger(value) && value > 0 ? value : fallback;
}

const CYCLES = integerEnv("REASONIX_APP_MEMORY_CYCLES", 128);
const MIXED_CYCLES = integerEnv("REASONIX_APP_MEMORY_MIXED_CYCLES", 512);
const PROCESSES = integerEnv("REASONIX_APP_MEMORY_PROCESSES", 3);
const PORT = integerEnv("REASONIX_APP_MEMORY_PORT", 4647);
const MAX_RETIRED_RENDER_TOKENS = 4;

const fixtures = {
  full: { label: "bench:small-6t", marker: "ASYNC LAYOUT EXPANSION COMPLETE" },
  geometry: { label: "bench:geometry", marker: "Geometry contract fixture complete." },
  windowed: { label: "bench:windowed-1000t", marker: "Windowed turn 1000" },
};

async function ensureBuild() {
  if (existsSync(path.join(frontendDir, "dist/index.html")) && process.env.REASONIX_APP_MEMORY_BUILD !== "1") return;
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
  if (!active?.includes(fixture.label)) {
    await page.locator(`.project-tree__topic-main:has-text("${fixture.label}")`).click();
  }
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
  const [heap, dom, lifecycle] = await Promise.all([
    cdp.send("Runtime.getHeapUsage"),
    cdp.send("Memory.getDOMCounters"),
    page.evaluate(() => window.__reasonixAppLifecycle?.snapshot()),
  ]);
  if (!lifecycle) throw new Error("App lifecycle probe was not published by the production build");
  return { heap, dom, lifecycle };
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

async function runAlternating(page, count, left, right) {
  for (let index = 0; index < count; index += 1) {
    await selectFixture(page, index % 2 === 0 ? left : right);
  }
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
    await selectFixture(page, fixtures.full);
    await runAlternating(page, 8, fixtures.geometry, fixtures.full);
    const baseline = await forceGc(cdp, page);

    await runAlternating(page, CYCLES, fixtures.geometry, fixtures.full);
    const full = await forceGc(cdp, page);
    await runAlternating(page, CYCLES, fixtures.windowed, fixtures.full);
    const windowed = await forceGc(cdp, page);
    for (let cycle = 0; cycle < CYCLES; cycle += 1) {
      await enterSafety(page);
      await selectFixture(page, fixtures.full);
    }
    const safety = await forceGc(cdp, page);
    const mixedFixtures = [fixtures.geometry, fixtures.windowed, fixtures.full];
    for (let cycle = 0; cycle < MIXED_CYCLES; cycle += 1) {
      if (cycle % 11 === 10) await enterSafety(page);
      else await selectFixture(page, mixedFixtures[cycle % mixedFixtures.length]);
    }
    await selectFixture(page, fixtures.full);
    const mixed = await forceGc(cdp, page);

    const samples = { baseline, full, windowed, safety, mixed };
    const maxTokens = Math.max(...Object.values(samples).map((sample) => sample.lifecycle.liveRenderTokens));
    const tokenGrowth = mixed.lifecycle.liveRenderTokens - baseline.lifecycle.liveRenderTokens;
    const activeOperations = mixed.lifecycle.activeOperations;
    const detachedDomGrowth = mixed.dom.nodes - baseline.dom.nodes;
    return {
      process: index,
      browser: browser.version(),
      samples,
      checks: {
        renderTokensBounded: tokenGrowth <= MAX_RETIRED_RENDER_TOKENS,
        activeOperationsReleased: activeOperations === 0,
        domNodesBounded: detachedDomGrowth <= Math.ceil(baseline.dom.nodes * 0.1),
        noPageErrors: pageErrors.length === 0,
      },
      metrics: { maxTokens, tokenGrowth, activeOperations, detachedDomGrowth, pageErrors },
    };
  } finally {
    await context.close();
    await browser.close();
  }
}

await ensureBuild();
const preview = await startPreviewServer(frontendDir, PORT);
const report = { startedAt: new Date().toISOString(), cycles: CYCLES, mixedCycles: MIXED_CYCLES, processes: [] };
try {
  for (let index = 1; index <= PROCESSES; index += 1) {
    const result = await runProcess(index);
    report.processes.push(result);
    process.stdout.write(`[app-memory] process ${index}: ${JSON.stringify({ checks: result.checks, metrics: result.metrics })}\n`);
  }
} finally {
  await preview.close();
}
report.finishedAt = new Date().toISOString();
report.verdict = report.processes.every((run) => Object.values(run.checks).every(Boolean)) ? "PASS" : "FAIL";
writeFileSync(path.join(frontendDir, "bench/app-memory-results.json"), JSON.stringify(report, null, 2));
process.stdout.write(`[app-memory] verdict ${report.verdict}\n`);
if (report.verdict !== "PASS") process.exitCode = 1;
