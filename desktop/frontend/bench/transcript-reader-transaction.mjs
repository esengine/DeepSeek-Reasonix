#!/usr/bin/env node

import { spawn } from "node:child_process";
import http from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";

const frontendDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
process.env.PLAYWRIGHT_BROWSERS_PATH = !process.env.PLAYWRIGHT_BROWSERS_PATH || process.env.PLAYWRIGHT_BROWSERS_PATH === ".pw-browsers"
  ? path.join(frontendDir, ".pw-browsers")
  : process.env.PLAYWRIGHT_BROWSERS_PATH;
const { chromium, webkit } = await import("playwright");
const port = Number(process.env.REASONIX_TRANSCRIPT_READER_PORT ?? 4621);
const iterations = Number(process.env.REASONIX_TRANSCRIPT_READER_ITERATIONS ?? 20);
const url = `http://127.0.0.1:${port}/?mock=bench&bench=1`;

function assert(condition, message) {
  if (!condition) throw new Error(message);
  process.stdout.write(`  PASS  ${message}\n`);
}

async function waitForServer() {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    const ready = await new Promise((resolve) => {
      const request = http.get(url, (response) => {
        response.resume();
        resolve((response.statusCode ?? 500) < 500);
      });
      request.on("error", () => resolve(false));
    });
    if (ready) return;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error("reader transaction preview did not become ready");
}

async function waitStable(page, requireTail = false) {
  await page.evaluate(({ requireTail }) => new Promise((resolve, reject) => {
    const started = performance.now();
    let previous;
    let stable = 0;
    const tick = () => {
      const element = document.querySelector(".transcript");
      if (element instanceof HTMLElement) {
        const current = {
          top: element.scrollTop,
          height: element.scrollHeight,
          mode: element.dataset.scrollMode,
          distance: element.scrollHeight - element.scrollTop - element.clientHeight,
          occupied: [...element.querySelectorAll(".transcript__row")].some((row) => {
            const rowRect = row.getBoundingClientRect();
            const viewport = element.getBoundingClientRect();
            return rowRect.bottom > viewport.top && rowRect.top < viewport.bottom;
          }),
        };
        stable = current.occupied && previous?.occupied
          && Math.abs(previous.top - current.top) <= 1
          && Math.abs(previous.height - current.height) <= 1
          && previous.mode === current.mode
          ? stable + 1
          : 0;
        previous = current;
        if (stable >= 2 && (!requireTail || (current.mode === "tail-follow" && current.distance <= 4))) {
          resolve();
          return;
        }
      }
      if (performance.now() - started > 15_000) {
        reject(new Error(`transcript did not stabilize: ${JSON.stringify(previous)}`));
        return;
      }
      requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  }), { requireTail });
}

async function loadLongFixture(page) {
  await page.click('.project-tree__topic-main:has-text("bench:tools-38t")');
  await page.waitForFunction(() => {
    const element = document.querySelector(".transcript");
    return document.querySelector(".project-tree__topic--active .project-tree__topic-label")?.textContent?.includes("bench:tools-38t")
      && element instanceof HTMLElement
      && element.textContent?.includes("pkg-41/mod.go")
      && Number.parseInt(element.dataset.transcriptRowCount ?? "0", 10) > 0;
  }, undefined, { timeout: 30_000 });
  await page.waitForFunction(() => !document.querySelector(".transcript-navigation-overlay"), undefined, { timeout: 15_000 });
  await page.waitForTimeout(300);
  const transcript = page.locator(".transcript");
  let stalledPages = 0;
  for (let pageIndex = 0; pageIndex < 32; pageIndex += 1) {
    const current = await transcript.evaluate((element) => Number.parseInt(element.dataset.transcriptRowCount ?? "0", 10));
    if (current >= 400) break;
    const box = await transcript.boundingBox();
    if (!box) throw new Error("transcript is unavailable for history paging");
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.wheel(0, -100_000);
    const loaded = await page.waitForFunction((before) => (
      Number.parseInt(document.querySelector(".transcript")?.dataset.transcriptRowCount ?? "0", 10) > before
    ), current, { timeout: 5_000 }).then(() => true, () => false);
    if (loaded) {
      stalledPages = 0;
      await page.waitForFunction(() => !document.querySelector(".transcript-navigation-overlay"), undefined, { timeout: 15_000 });
    } else {
      // WebKit can deliver the top wheel while a retained page is still
      // committing. Retry the native paging gesture after that boundary;
      // three quiet attempts mean the fixture has genuinely reached history.
      stalledPages += 1;
      if (stalledPages >= 3) break;
      await page.waitForTimeout(100);
    }
  }
  let rows = await transcript.evaluate((element) => Number.parseInt(element.dataset.transcriptRowCount ?? "0", 10));
  if (rows < 400) {
    // A second browser engine can inherit a quiet top range after the mock's
    // one-wheel paging authorization has already been consumed. Use the
    // product's real unloaded-question transaction as a deterministic user
    // fallback; this exercises the same history API without a test-only hook.
    for (let jump = 0; jump < 4 && rows < 400; jump += 1) {
      const marker = page.locator('.jump-item[data-loaded="false"]').last();
      const markerBox = await marker.boundingBox();
      if (!markerBox) break;
      await page.mouse.click(markerBox.x + markerBox.width / 2, markerBox.y + markerBox.height / 2);
      await page.waitForFunction(() => {
        const element = document.querySelector(".transcript");
        return element instanceof HTMLElement
          && element.dataset.scrollMode !== "restoring"
          && !document.querySelector(".transcript-navigation-overlay");
      }, undefined, { timeout: 30_000 });
      rows = await transcript.evaluate((element) => Number.parseInt(element.dataset.transcriptRowCount ?? "0", 10));
    }
  }
  assert(rows >= 400, `long reader fixture mounts at least 400 logical rows (${rows})`);
  // The mock history can expose its first 400-row window before the retained
  // surface finishes hydration. Reader assertions begin only after the same
  // production completion signal used by the general stability bench, so the
  // intentional loading/bootstrap surface is not counted as an empty frame.
  await page.waitForFunction(() => document.querySelector(".transcript")?.dataset.transcriptHydrating === "false", undefined, { timeout: 15_000 });
  await page.waitForTimeout(300);
  const stableRows = await transcript.evaluate((element) => Number.parseInt(element.dataset.transcriptRowCount ?? "0", 10));
  assert(stableRows >= 400, `long reader fixture keeps its loaded window (${stableRows})`);
  return transcript;
}

async function runIteration(page, transcript, label, iteration) {
  const inputStates = [];
  const setupMode = await transcript.getAttribute("data-scroll-mode");
  // Test positioning is meaningful only after a real reader input owns the
  // surface. Otherwise tail-follow can legitimately pin the raw scrollTop
  // assignment back to the physical bottom before the transaction begins.
  if (setupMode === "tail-follow") {
    await transcript.dispatchEvent("wheel", { deltaY: -24, deltaMode: 0, bubbles: true, cancelable: true });
    await page.waitForFunction(() => document.querySelector(".transcript")?.dataset.scrollMode === "manual", undefined, { timeout: 2_000 }).catch((error) => {
      throw new Error(`reader setup could not enter manual mode from ${setupMode}: ${error.message}`);
    });
  }
  await transcript.evaluate((element) => {
    element.scrollTop = Math.max(element.clientHeight * 2, (element.scrollHeight - element.clientHeight) * 0.55);
    element.dispatchEvent(new Event("scroll"));
  });
  await waitStable(page);
  await page.evaluate(() => {
    if (!(document.querySelector(".transcript") instanceof HTMLElement)) throw new Error("transcript missing");
    window.__readerTransactionProbe = { active: true, frames: [], writes: [] };
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => window.__readerTransactionProbe?.writes.push(write);
    // Sample after the animation-frame callbacks have committed their visual
    // guards. Reading in an earlier rAF callback can observe Virtuoso's new
    // row geometry before the geometry controller's later callback applies
    // its same-paint transform, reporting a jump the user never sees.
    const schedulePaintSample = () => requestAnimationFrame(() => setTimeout(sample, 0));
    const sample = () => {
      const probe = window.__readerTransactionProbe;
      if (!probe?.active) return;
      const element = document.querySelector(".transcript");
      if (!(element instanceof HTMLElement)) {
        probe.frames.push({ top: 0, height: 0, occupied: false, mode: "missing", guard: "", visible: [], connected: false });
        schedulePaintSample();
        return;
      }
      const viewport = element.getBoundingClientRect();
      const rows = [...element.querySelectorAll(".transcript__row")]
        .filter((row) => row.getBoundingClientRect().bottom > viewport.top && row.getBoundingClientRect().top < viewport.bottom);
      probe.frames.push({
        top: element.scrollTop,
        height: element.scrollHeight,
        occupied: rows.length > 0,
        mode: element.dataset.scrollMode,
        readerIntent: element.dataset.transcriptReaderIntent,
        guard: element.style.getPropertyValue("--transcript-reader-visual-offset"),
        connected: element.isConnected,
        visible: rows.map((row) => ({ index: row.dataset.index ?? row.dataset.logicalIndex ?? "", top: row.getBoundingClientRect().top - viewport.top })),
      });
      schedulePaintSample();
    };
    schedulePaintSample();
  });

  for (let tick = 0; tick < 6; tick += 1) {
    await transcript.evaluate((element) => {
      element.dispatchEvent(new WheelEvent("wheel", { deltaY: 24, deltaMode: 0, bubbles: true, cancelable: true }));
    });
    if (tick === 0) {
      await page.waitForFunction(() => document.querySelector(".transcript")?.dataset.transcriptReaderIntent === "true", undefined, { timeout: 2_000 });
    }
    inputStates.push(await page.evaluate((currentTick) => ({
      tick: currentTick,
      surfaces: [...document.querySelectorAll(".transcript")].map((element) => ({
        connected: element.isConnected,
        intent: element.dataset.transcriptReaderIntent,
        mode: element.dataset.scrollMode,
        rows: element.dataset.transcriptRowCount,
        top: element.scrollTop,
        height: element.scrollHeight,
      })),
    }), tick));
    await page.waitForTimeout(16);
  }
  const intentBeforeCollapse = await transcript.getAttribute("data-transcript-reader-intent");
  if (intentBeforeCollapse !== "true") throw new Error(`${label} ${iteration + 1}/${iterations}: reader transaction ended before collapse ${JSON.stringify(inputStates)}`);
  await transcript.evaluate(async (element) => {
    const physicalHeight = element.scrollHeight;
    const collapse = Math.min(4_000, Math.max(2_000, element.clientHeight * 3));
    const collapsedHeight = Math.max(element.clientHeight + 1, physicalHeight - collapse);
    Object.defineProperty(element, "scrollHeight", { configurable: true, get: () => collapsedHeight });
    element.scrollTop = Math.max(0, element.scrollTop - collapse);
    element.dispatchEvent(new Event("scroll"));
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    delete element.scrollHeight;
    element.dispatchEvent(new Event("scroll"));
  });
  for (let tick = 0; tick < 12; tick += 1) {
    await transcript.evaluate((element) => {
      element.dispatchEvent(new WheelEvent("wheel", { deltaY: 24, deltaMode: 0, bubbles: true, cancelable: true }));
    });
    await page.waitForTimeout(16);
  }
  await page.waitForFunction(() => (
    document.querySelector(".transcript")?.dataset.transcriptReaderIntent !== "true"
  ), undefined, { timeout: 1_500 });
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  const probe = await page.evaluate(() => {
    const value = window.__readerTransactionProbe;
    if (value) value.active = false;
    window.__readerTransactionProbe = undefined;
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = undefined;
    return value;
  });

  let maxReverse = 0;
  let maxReversePair;
  for (let index = 1; index < probe.frames.length; index += 1) {
    const previous = probe.frames[index - 1];
    const current = probe.frames[index];
    const currentRows = new Map(current.visible.filter((row) => row.index).map((row) => [row.index, row.top]));
    const visual = previous.visible
      .filter((row) => row.index && currentRows.has(row.index))
      .map((row) => currentRows.get(row.index) - row.top)
      .sort((left, right) => left - right);
    if (visual.length > 0) {
      const reverse = visual[Math.floor(visual.length / 2)];
      if (reverse > maxReverse) {
        maxReverse = reverse;
        maxReversePair = { previous, current, reverse };
      }
    }
  }
  const acceptedCorrections = probe.writes.filter((write) => write.owner === "reader-stability" && !write.rejectedReason);
  const correctionsByTransaction = new Map();
  for (const write of acceptedCorrections) correctionsByTransaction.set(write.transactionId, (correctionsByTransaction.get(write.transactionId) ?? 0) + 1);
  const blankFrames = probe.frames.filter((frame) => !frame.occupied);
  const reverseDetail = maxReverse > 96
    ? `; ${JSON.stringify(maxReversePair)}; writes=${JSON.stringify(probe.writes.filter((write) => write.owner === "reader-stability"))}`
    : "";
  const blankDetail = blankFrames.length > 0
    ? `; first=${JSON.stringify(blankFrames[0])}; writes=${JSON.stringify(probe.writes.filter((write) => write.owner === "reader-stability"))}`
    : "";
  assert(maxReverse <= 96, `${label} ${iteration + 1}/${iterations}: no visual reverse jump above 96px (${maxReverse.toFixed(1)}px${reverseDetail})`);
  assert(blankFrames.length === 0, `${label} ${iteration + 1}/${iterations}: mounted rows cover every sampled frame (${blankFrames.length}/${probe.frames.length} blank${blankDetail})`);
  assert([...correctionsByTransaction.values()].every((count) => count <= 1), `${label} ${iteration + 1}/${iterations}: each reader transaction corrects at most once`);
}

async function runBrowser(browserType, label) {
  const browser = await browserType.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
    await page.addInitScript(() => localStorage.setItem("reasonix-process-fold", "expanded"));
    await page.goto(url, { waitUntil: "domcontentloaded" });
    await page.waitForFunction(() => !document.querySelector(".startup-splash"), undefined, { timeout: 30_000 });
    const transcript = await loadLongFixture(page);
    for (let iteration = 0; iteration < iterations; iteration += 1) await runIteration(page, transcript, label, iteration);
    const jumpBottom = page.locator(".transcript__jump-bottom");
    if (await jumpBottom.isVisible()) {
      await jumpBottom.click();
    } else {
      // A physical-bottom clamp can hide the affordance while ownership is
      // still manual. One real downward wheel transaction must perform the
      // same two-frame stability handoff without a programmatic shortcut.
      await transcript.dispatchEvent("wheel", { deltaY: 24, deltaMode: 0, bubbles: true, cancelable: true });
    }
    await waitStable(page, true);
    const final = await transcript.evaluate((element) => ({
      distance: element.scrollHeight - element.scrollTop - element.clientHeight,
      mode: element.dataset.scrollMode,
      occupied: [...element.querySelectorAll(".transcript__row")].some((row) => {
        const rowRect = row.getBoundingClientRect();
        const viewport = element.getBoundingClientRect();
        return rowRect.bottom > viewport.top && rowRect.top < viewport.bottom;
      }),
    }));
    assert(final.distance <= 4 && final.mode === "tail-follow", `${label}: final viewport reaches the physical tail`);
    assert(final.occupied, `${label}: final viewport has no blank range`);
  } finally {
    await browser.close();
  }
}

const packageManager = process.platform === "win32" ? "pnpm.cmd" : "pnpm";
const preview = spawn(packageManager, ["exec", "vite", "preview", "--port", String(port), "--strictPort", "--host", "127.0.0.1"], {
  cwd: frontendDir,
  stdio: "ignore",
  shell: process.platform === "win32",
});

try {
  await waitForServer();
  await runBrowser(chromium, "Chromium");
  await runBrowser(webkit, "WebKit");
  process.stdout.write("transcript reader transaction browser replay passed\n");
} finally {
  preview.kill("SIGTERM");
}
