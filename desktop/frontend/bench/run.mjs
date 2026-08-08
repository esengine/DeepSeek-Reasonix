#!/usr/bin/env node
// Real-DOM performance benchmark for the session-switch/history pipeline
// (Phase F). Drives the production build (vite preview) in real Chromium via
// Playwright against the ?mock=bench dev-mock fixtures (see makeMockApp in
// src/lib/bridge.ts): a tool-dense 38-turn session (~3.2k messages) and a
// markdown-heavy 46-turn session (one ~500KiB answer + oversized code block).
//
// Scenario A: cold open — surface first paint and time-to-interactive
// (composer enabled + first transcript slice rendered).
// Scenario B: N alternating switches between the two heaviest sessions —
// per-switch latency, long-task/event-timing stats, INP-ish input probes, and
// a forced-GC retained-heap + cache-budget check against the warmup baseline.
//
// Usage:
//   pnpm build            # once (the bench reuses dist/; REASONIX_BENCH_BUILD=1 forces a rebuild)
//   pnpm test:bench
//
// Gates (env-overridable, defaults = plan values):
//   REASONIX_BENCH_FIRST_PAINT_P95_MS   (100)
//   REASONIX_BENCH_INTERACTIVE_P95_MS   (300)
//   REASONIX_BENCH_INP_P95_MS           (200)
//   REASONIX_BENCH_LONGTASK_P95_MS      (50)
//   REASONIX_BENCH_LONGTASK_MAX_MS      (500)
//   REASONIX_BENCH_MARKDOWN_PARSE_MAX_MS (3000)
//   REASONIX_BENCH_HEAP_GROWTH_MIB      (20)
//   REASONIX_BENCH_SWITCHES             (100)
//   REASONIX_BENCH_COLD_RUNS            (5)
//   REASONIX_BENCH_WARMUP               (6)
//   REASONIX_BENCH_PORT                 (4617)
//
// Exit code is 0 when every gate passes, 1 otherwise. Results are written to
// bench/results.json and summarized on stdout.

import { spawn } from "node:child_process";
import { existsSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import http from "node:http";

const frontendDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
// Keep the browser download inside the repo when the caller did not pin one.
process.env.PLAYWRIGHT_BROWSERS_PATH ??= path.join(frontendDir, ".pw-browsers");

const { chromium } = await import("playwright");

function numEnv(name, fallback) {
  const raw = process.env[name];
  const value = raw === undefined ? NaN : Number(raw);
  return Number.isFinite(value) && value > 0 ? value : fallback;
}

const GATES = {
  firstPaintP95Ms: numEnv("REASONIX_BENCH_FIRST_PAINT_P95_MS", 100),
  interactiveP95Ms: numEnv("REASONIX_BENCH_INTERACTIVE_P95_MS", 300),
  inpP95Ms: numEnv("REASONIX_BENCH_INP_P95_MS", 200),
  longTaskP95Ms: numEnv("REASONIX_BENCH_LONGTASK_P95_MS", 50),
  longTaskMaxMs: numEnv("REASONIX_BENCH_LONGTASK_MAX_MS", 500),
  markdownParseMaxMs: numEnv("REASONIX_BENCH_MARKDOWN_PARSE_MAX_MS", 3000),
  heapGrowthMiB: numEnv("REASONIX_BENCH_HEAP_GROWTH_MIB", 20),
};
const SWITCHES = Math.round(numEnv("REASONIX_BENCH_SWITCHES", 100));
const COLD_RUNS = Math.round(numEnv("REASONIX_BENCH_COLD_RUNS", 5));
const WARMUP_SWITCHES = Math.round(numEnv("REASONIX_BENCH_WARMUP", 6));
const PORT = Math.round(numEnv("REASONIX_BENCH_PORT", 4617));

// Cache budgets mirrored from transcriptStore.ts defaults (asserted via the
// __reasonixPerf debug hook, which reports the store's own numbers).
const BODY_BUDGET_BYTES = 32 << 20;
const MARKDOWN_BUDGET_BYTES = 16 << 20;
const MAX_RESIDENT_SESSIONS = 3;

const PAGE_URL = `http://127.0.0.1:${PORT}/?mock=bench&bench=1`;
// Single-surface (workbench) layout: switches are sidebar topic clicks driving
// the ticketed StartTopicActivation flow. Active state is the --active class
// on the topic row; the transcript marker text proves the target session's
// first slice rendered.
const TAB = {
  // Markers must be in the STUCK-TO-BOTTOM viewport (the virtual list only
  // mounts overscan rows): the tools session ends on its last read_file card,
  // the markdown session on the oversized code block (worker-parsed).
  markdown: { label: "bench:markdown-46t", marker: "generated migration" },
  tools: { label: "bench:tools-38t", marker: "pkg-41/mod.go" },
};

function percentile(values, p) {
  if (values.length === 0) return undefined;
  const sorted = [...values].sort((a, b) => a - b);
  const index = Math.min(sorted.length - 1, Math.ceil((p / 100) * sorted.length) - 1);
  return sorted[Math.max(0, index)];
}

function statsOf(values) {
  const nums = values.filter((v) => Number.isFinite(v));
  return {
    n: nums.length,
    p50: percentile(nums, 50),
    p95: percentile(nums, 95),
    max: nums.length ? Math.max(...nums) : undefined,
  };
}

async function waitForServer(url, timeoutMs = 30_000) {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const ok = await new Promise((resolve) => {
      const req = http.get(url, (res) => {
        res.resume();
        resolve(res.statusCode !== undefined && res.statusCode < 500);
      });
      req.on("error", () => resolve(false));
      req.setTimeout(2000, () => {
        req.destroy();
        resolve(false);
      });
    });
    if (ok) return;
    if (Date.now() > deadline) throw new Error(`preview server did not start at ${url}`);
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
}

async function ensureBuild() {
  const distIndex = path.join(frontendDir, "dist", "index.html");
  const force = process.env.REASONIX_BENCH_BUILD === "1";
  if (existsSync(distIndex) && !force) return;
  console.log("[bench] building frontend (vite build)…");
  await new Promise((resolve, reject) => {
    const child = spawn("pnpm", ["build"], { cwd: frontendDir, stdio: "inherit" });
    child.on("exit", (code) => (code === 0 ? resolve() : reject(new Error(`pnpm build exited ${code}`))));
  });
}

async function startPreview() {
  const child = spawn("pnpm", ["exec", "vite", "preview", "--port", String(PORT), "--strictPort", "--host", "127.0.0.1"], {
    cwd: frontendDir,
    stdio: ["ignore", "pipe", "pipe"],
  });
  let stderr = "";
  child.stderr.on("data", (chunk) => {
    const message = chunk.toString();
    stderr += message;
    process.stderr.write(`[preview] ${message}`);
  });
  await new Promise((resolve, reject) => {
    let stdout = "";
    const timeout = setTimeout(() => {
      cleanup();
      child.kill();
      reject(new Error(`preview server did not announce readiness on port ${PORT}`));
    }, 30_000);
    const cleanup = () => {
      clearTimeout(timeout);
      child.stdout.off("data", onStdout);
      child.off("exit", onExit);
    };
    const onStdout = (chunk) => {
      stdout += chunk.toString();
      if (!/\bLocal:\s+http/.test(stdout)) return;
      cleanup();
      resolve();
    };
    const onExit = (code, signal) => {
      cleanup();
      const detail = stderr.trim();
      reject(new Error(
        `preview server exited before readiness (code ${code ?? "null"}, signal ${signal ?? "none"})${detail ? `: ${detail}` : ""}`,
      ));
    };
    child.stdout.on("data", onStdout);
    child.once("exit", onExit);
  });
  await waitForServer(`http://127.0.0.1:${PORT}/`);
  return child;
}

// Installed before any app script: long-task, event-timing (INP-ish) and paint
// collectors. All bounded by the scenario durations (a few minutes).
const COLLECTOR_INIT = () => {
  const metrics = { longTasks: [], events: [], paints: [] };
  window.__benchMetrics = metrics;
  try {
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) metrics.longTasks.push({ start: entry.startTime, duration: entry.duration });
    }).observe({ type: "longtask", buffered: true });
  } catch { /* longtask unsupported */ }
  try {
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) metrics.events.push({ name: entry.name, duration: entry.duration });
    }).observe({ type: "event", durationThreshold: 16, buffered: true });
  } catch { /* event timing unsupported */ }
  try {
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) metrics.paints.push({ name: entry.name, start: entry.startTime });
    }).observe({ type: "paint", buffered: true });
  } catch { /* paint timing unsupported */ }
};

async function forceGcAndHeap(cdp, page) {
  await cdp.send("HeapProfiler.enable").catch(() => {});
  await cdp.send("HeapProfiler.collectGarbage").catch(() => {});
  await page.waitForTimeout(150);
  await cdp.send("HeapProfiler.collectGarbage").catch(() => {});
  await page.waitForTimeout(150);
  return page.evaluate(() => {
    const memory = performance.memory;
    return {
      usedJSHeapBytes: memory ? memory.usedJSHeapSize : undefined,
      totalJSHeapBytes: memory ? memory.totalJSHeapSize : undefined,
      domNodes: document.getElementsByTagName("*").length,
    };
  });
}

const INTERACTIVE_FN = () => {
  const input = document.querySelector("textarea.composer__input");
  const inputReady = Boolean(input && !input.disabled);
  const rows = document.querySelectorAll(".transcript__row").length;
  return inputReady && rows > 0;
};

async function coldOpenOnce(browser) {
  const context = await browser.newContext();
  const page = await context.newPage();
  await page.addInitScript(COLLECTOR_INIT);
  await page.goto(PAGE_URL, { waitUntil: "domcontentloaded" });
  await page.waitForFunction(INTERACTIVE_FN, undefined, { timeout: 30_000, polling: "raf" });
  const result = await page.evaluate(() => {
    const paints = (window.__benchMetrics?.paints ?? []);
    const fcp = paints.find((p) => p.name === "first-contentful-paint") ?? paints.find((p) => p.name === "first-paint");
    return { firstPaintMs: fcp ? fcp.start : undefined, interactiveMs: performance.now() };
  });
  await context.close();
  return result;
}

async function waitForSessionVisible(page, tab, timeoutMs = 15_000) {
  await page.waitForFunction(
    ({ label, marker }) => {
      const active = document.querySelector(".project-tree__topic--active .project-tree__topic-label");
      if (!active || !active.textContent?.includes(label)) return false;
      const transcript = document.querySelector(".transcript");
      return Boolean(transcript && transcript.textContent?.includes(marker));
    },
    { label: tab.label, marker: tab.marker },
    { timeout: timeoutMs, polling: "raf" },
  );
}

async function switchTo(page, tab) {
  const startedAt = await page.evaluate(() => performance.now());
  await page.click(`.project-tree__topic-main:has-text("${tab.label}")`);
  await waitForSessionVisible(page, tab);
  const settledAt = await page.evaluate(() => performance.now());
  return settledAt - startedAt;
}

// Let in-flight worker parses finish so the parsed-markdown cache is populated
// and exercised (a fast switch-away cancels pending parses by design). Runs
// AFTER the switch-latency measurement so click→first-render stays pure.
async function settleMarkdownWorker(page, timeoutMs = 10_000) {
  await page
    .waitForFunction(() => (window.__reasonixPerf?.stats()?.markdownWorker?.pending ?? 0) === 0, undefined, {
      timeout: timeoutMs,
      polling: 100,
    })
    .catch(() => {});
}

// Progressive history blocks intentionally mount in idle slices after parsing.
// Stabilize that deferred work only around heap/DOM snapshots; waiting on every
// switch would change the interaction workload the benchmark is measuring.
async function settleMarkdownMounts(page, timeoutMs = 10_000) {
  try {
    await page.waitForFunction(() => (
      [...document.querySelectorAll(".transcript [data-markdown-blocks]")].every((element) => (
        element.getAttribute("data-markdown-visible-blocks") === element.getAttribute("data-markdown-blocks")
      ))
    ), undefined, { timeout: timeoutMs, polling: 100 });
  } catch (error) {
    const counts = await page.evaluate(() => (
      [...document.querySelectorAll(".transcript [data-markdown-blocks]")].map((element) => ({
        total: element.getAttribute("data-markdown-blocks"),
        visible: element.getAttribute("data-markdown-visible-blocks"),
      }))
    ));
    throw new Error(`active Markdown mounts did not settle: ${JSON.stringify(counts)}`, { cause: error });
  }
}

async function main() {
  await ensureBuild();
  const preview = await startPreview();
  const report = {
    startedAt: new Date().toISOString(),
    url: PAGE_URL,
    gates: GATES,
    switches: SWITCHES,
    coldRuns: COLD_RUNS,
    checks: [],
    scenarioA: {},
    scenarioB: {},
  };
  const check = (name, value, gate, pass, unit = "ms") => {
    report.checks.push({ name, value, gate, pass, unit });
    console.log(`  ${pass ? "PASS" : "FAIL"}  ${name}: ${typeof value === "number" ? value.toFixed(1) : value}${unit} (gate ${gate}${unit})`);
  };

  const browser = await chromium.launch({
    headless: true,
    args: ["--enable-precise-memory-info", "--disable-dev-shm-usage"],
  });
  try {
    // ── Scenario A: cold open ──────────────────────────────────────────────
    console.log(`[bench] scenario A: ${COLD_RUNS} cold opens of the markdown-heavy session…`);
    const coldFirstPaint = [];
    const coldInteractive = [];
    for (let i = 0; i < COLD_RUNS; i += 1) {
      const { firstPaintMs, interactiveMs } = await coldOpenOnce(browser);
      if (firstPaintMs !== undefined) coldFirstPaint.push(firstPaintMs);
      coldInteractive.push(interactiveMs);
    }
    report.scenarioA = { firstPaintMs: statsOf(coldFirstPaint), interactiveMs: statsOf(coldInteractive) };
    check("A first-paint P95", report.scenarioA.firstPaintMs.p95, GATES.firstPaintP95Ms, report.scenarioA.firstPaintMs.p95 <= GATES.firstPaintP95Ms);
    check("A interactive P95", report.scenarioA.interactiveMs.p95, GATES.interactiveP95Ms, report.scenarioA.interactiveMs.p95 <= GATES.interactiveP95Ms);

    // ── Scenario B: alternating heaviest-session switches ──────────────────
    console.log(`[bench] scenario B: ${SWITCHES} alternating switches (warmup ${WARMUP_SWITCHES})…`);
    const context = await browser.newContext();
    const page = await context.newPage();
    await page.addInitScript(COLLECTOR_INIT);
    await page.goto(PAGE_URL, { waitUntil: "domcontentloaded" });
    await page.waitForFunction(INTERACTIVE_FN, undefined, { timeout: 30_000, polling: "raf" });
    await page.waitForFunction(() => Boolean(window.__reasonixPerf), { timeout: 10_000 });
    const cdp = await context.newCDPSession(page);

    // Warmup: fill the LRU with both sessions, then settle back on the
    // markdown tab so the baseline and the final reading show the same view.
    for (let i = 0; i < WARMUP_SWITCHES; i += 1) {
      const target = i % 2 === 0 ? TAB.tools : TAB.markdown;
      await switchTo(page, target);
      if (target === TAB.markdown) await settleMarkdownWorker(page);
    }
    await settleMarkdownMounts(page);
    const baseline = await forceGcAndHeap(cdp, page);
    const baselineStats = await page.evaluate(() => window.__reasonixPerf.stats());
    await page.evaluate(() => {
      window.__benchMetrics.longTasks.length = 0;
      window.__benchMetrics.events.length = 0;
      window.__reasonixPerf.reset();
    });

    const switchLatencies = [];
    for (let i = 0; i < SWITCHES; i += 1) {
      // First switch leaves the markdown tab (warmup ended there); even count
      // lands back on markdown so the final heap/DOM reading compares like for
      // like with the baseline.
      const target = i % 2 === 0 ? TAB.tools : TAB.markdown;
      switchLatencies.push(await switchTo(page, target));
      if (target === TAB.markdown) await settleMarkdownWorker(page);
      if ((i + 1) % 10 === 0) {
        // INP-ish probe: real key events against the composer while the
        // pipeline is warm; event-timing entries capture the latency.
        const composer = page.locator("textarea.composer__input");
        await composer.click();
        await page.keyboard.type("x", { delay: 5 });
        await page.keyboard.press("Backspace");
      }
    }

    await settleMarkdownMounts(page);

    // Freeze interaction metrics before the explicit HeapProfiler GC below.
    // The GC is part of retained-heap measurement, not the user switching
    // workflow, and can itself create a >50ms task on a loaded test host.
    const { finalStats, activations, benchMetrics } = await page.evaluate(() => ({
      finalStats: window.__reasonixPerf.stats(),
      activations: window.__reasonixPerf.activations(),
      benchMetrics: {
        longTasks: window.__benchMetrics.longTasks.map((t) => t.duration),
        events: window.__benchMetrics.events.map((e) => e.duration),
      },
    }));
    const final = await forceGcAndHeap(cdp, page);
    await context.close();

    const switchStats = statsOf(switchLatencies);
    const readyStats = statsOf(
      activations.filter((a) => a.outcome === "ready" && a.settledAtMs !== undefined).map((a) => a.settledAtMs - a.requestedAtMs),
    );
    const longTaskStats = statsOf(benchMetrics.longTasks);
    const eventStats = statsOf(benchMetrics.events);
    const heapGrowthBytes = (final.usedJSHeapBytes ?? 0) - (baseline.usedJSHeapBytes ?? 0);
    const heapGrowthMiB = heapGrowthBytes / (1 << 20);
    const cache = finalStats?.transcriptCache ?? {};
    const worker = finalStats?.markdownWorker ?? {};

    report.scenarioB = {
      switchMs: switchStats,
      activationReadyMs: readyStats,
      longTasksMs: longTaskStats,
      inputEventsMs: eventStats,
      heap: {
        baselineUsedMiB: (baseline.usedJSHeapBytes ?? 0) / (1 << 20),
        finalUsedMiB: (final.usedJSHeapBytes ?? 0) / (1 << 20),
        growthMiB: heapGrowthMiB,
      },
      domNodes: { baseline: baseline.domNodes, final: final.domNodes },
      transcriptCache: cache,
      markdownWorker: worker,
      baselineCache: baselineStats?.transcriptCache,
    };

    check("B switch latency P95 (click→target session rendered)", switchStats.p95, GATES.interactiveP95Ms, switchStats.p95 <= GATES.interactiveP95Ms);
    if (readyStats.n > 0) {
      check("B activation ready P95", readyStats.p95, GATES.interactiveP95Ms, readyStats.p95 <= GATES.interactiveP95Ms);
    }
    check("B input-event P95 (INP-ish)", eventStats.p95 ?? 0, GATES.inpP95Ms, (eventStats.p95 ?? 0) <= GATES.inpP95Ms);
    check("B long-task P95", longTaskStats.p95 ?? 0, GATES.longTaskP95Ms, (longTaskStats.p95 ?? 0) <= GATES.longTaskP95Ms);
    check("B long-task max", longTaskStats.max ?? 0, GATES.longTaskMaxMs, (longTaskStats.max ?? 0) <= GATES.longTaskMaxMs);
    check("B markdown Worker completed parses (minimum)", worker.completed ?? 0, 1, (worker.completed ?? 0) >= 1, "");
    check(
      "B markdown Worker max parse",
      worker.maxParseMs ?? 0,
      GATES.markdownParseMaxMs,
      (worker.completed ?? 0) >= 1 && (worker.maxParseMs ?? Infinity) <= GATES.markdownParseMaxMs,
    );
    check("B retained heap growth", heapGrowthMiB, GATES.heapGrowthMiB, heapGrowthMiB <= GATES.heapGrowthMiB, "MiB");
    check(
      "B cache: body bytes within budget",
      (cache.bodyBytes ?? 0) / (1 << 20),
      BODY_BUDGET_BYTES / (1 << 20),
      (cache.bodyBytes ?? 0) <= BODY_BUDGET_BYTES,
      "MiB",
    );
    check(
      "B cache: markdown bytes within budget",
      (cache.markdownBytes ?? 0) / (1 << 20),
      MARKDOWN_BUDGET_BYTES / (1 << 20),
      (cache.markdownBytes ?? 0) <= MARKDOWN_BUDGET_BYTES,
      "MiB",
    );
    check("B cache: resident sessions", cache.residentSessions ?? 0, MAX_RESIDENT_SESSIONS, (cache.residentSessions ?? 0) <= MAX_RESIDENT_SESSIONS, "");
    const domGrowthPct = baseline.domNodes > 0 ? ((final.domNodes - baseline.domNodes) / baseline.domNodes) * 100 : 0;
    check("B DOM node growth vs warmup baseline", domGrowthPct, 10, domGrowthPct <= 10, "%");
  } finally {
    await browser.close();
    preview.kill();
  }

  report.finishedAt = new Date().toISOString();
  const failed = report.checks.filter((c) => !c.pass);
  report.verdict = failed.length === 0 ? "PASS" : "FAIL";
  writeFileSync(path.join(frontendDir, "bench", "results.json"), JSON.stringify(report, null, 2));
  console.log(`\n[bench] verdict: ${report.verdict} (${failed.length} gate${failed.length === 1 ? "" : "s"} failed) — results in bench/results.json`);
  if (failed.length > 0) process.exitCode = 1;
}

main().catch((err) => {
  console.error(`[bench] fatal: ${err instanceof Error ? err.stack ?? err.message : err}`);
  process.exitCode = 2;
});
