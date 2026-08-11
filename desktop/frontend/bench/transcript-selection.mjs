#!/usr/bin/env node

import { spawn } from "node:child_process";
import http from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";

const frontendDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
process.env.PLAYWRIGHT_BROWSERS_PATH = !process.env.PLAYWRIGHT_BROWSERS_PATH || process.env.PLAYWRIGHT_BROWSERS_PATH === ".pw-browsers"
  ? path.join(frontendDir, ".pw-browsers")
  : process.env.PLAYWRIGHT_BROWSERS_PATH;
const { chromium } = await import("playwright");
const port = Number(process.env.REASONIX_TRANSCRIPT_BROWSER_PORT ?? 4618);
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
    await new Promise((resolve) => setTimeout(resolve, 150));
  }
  throw new Error("transcript browser preview did not become ready");
}

const preview = spawn("pnpm", ["exec", "vite", "preview", "--port", String(port), "--strictPort", "--host", "127.0.0.1"], {
  cwd: frontendDir,
  stdio: "ignore",
});

let browser;
try {
  await waitForServer();
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Performance.enable");
  const retainedHeap = async () => {
    await cdp.send("HeapProfiler.collectGarbage");
    await page.waitForTimeout(100);
    const metrics = await cdp.send("Performance.getMetrics");
    return metrics.metrics.find((metric) => metric.name === "JSHeapUsedSize")?.value ?? 0;
  };
  await page.goto(url, { waitUntil: "domcontentloaded" });
  await page.waitForFunction(() => document.querySelectorAll(".transcript__row").length > 4, undefined, { timeout: 30_000 });
  await page.waitForFunction(() => !document.querySelector(".startup-splash"), undefined, { timeout: 30_000 });
  await page.click('.project-tree__topic-main:has-text("bench:tools-38t")');
  await page.waitForFunction(() => (
    document.querySelector(".project-tree__topic--active .project-tree__topic-label")?.textContent?.includes("bench:tools-38t")
      && document.querySelector(".transcript")?.textContent?.includes("pkg-41/mod.go")
  ), undefined, { timeout: 30_000 });
  await page.waitForTimeout(300);
  for (let pageIndex = 0; pageIndex < 8; pageIndex += 1) {
    await page.evaluate(() => {
      const transcript = document.querySelector(".transcript");
      if (transcript) transcript.scrollTop = 0;
    });
    await page.waitForTimeout(100);
    const older = page.locator(".transcript__older");
    if (await older.count() === 0) break;
    await older.click();
    await page.waitForTimeout(350);
  }
  await page.evaluate(() => {
    const transcript = document.querySelector(".transcript");
    if (transcript) transcript.scrollTop = transcript.scrollHeight;
  });
  await page.waitForTimeout(250);
  for (let index = 0; index < 20; index += 1) {
    const visibleSelectable = await page.evaluate(() => {
      const transcript = document.querySelector(".transcript");
      if (!transcript) return false;
      const viewport = transcript.getBoundingClientRect();
      const visible = [...transcript.querySelectorAll("[data-transcript-selectable]")].some((element) => {
        const rect = element.getBoundingClientRect();
        return rect.height > 0 && rect.bottom > viewport.top && rect.top < viewport.bottom;
      });
      if (!visible) transcript.scrollTop -= transcript.clientHeight;
      return visible;
    });
    if (visibleSelectable) break;
    await page.waitForTimeout(50);
  }

  const baselineRows = await page.locator(".transcript__row").count();
  const points = await page.evaluate(() => {
    const transcript = document.querySelector(".transcript");
    if (!transcript) return null;
    const viewport = transcript.getBoundingClientRect();
    const bodies = [...transcript.querySelectorAll("[data-transcript-selectable]")]
      .map((element) => element.getBoundingClientRect())
      .filter((rect) => rect.height > 0 && rect.bottom > viewport.top && rect.top < viewport.bottom);
    const textRects = [...transcript.querySelectorAll("[data-transcript-selectable]")].flatMap((element) => {
      const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
      const rects = [];
      for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        if (!node.textContent?.trim()) continue;
        const range = document.createRange();
        range.selectNodeContents(node);
        rects.push(...range.getClientRects());
      }
      return rects;
    }).filter((rect) => rect.width > 8 && rect.bottom > viewport.top && rect.top < viewport.bottom);
    const start = textRects.at(-1) ?? bodies.at(-1);
    if (!start) return null;
    const startX = start.left + 2;
    return {
      start: { x: startX, y: (Math.max(start.top, viewport.top) + Math.min(start.bottom, viewport.bottom)) / 2 },
      activate: { x: Math.min(start.right - 2, startX + 30), y: (Math.max(start.top, viewport.top) + Math.min(start.bottom, viewport.bottom)) / 2 },
      edge: { x: startX, y: viewport.top + 2 },
    };
  });
  assert(points != null, "bench transcript exposes a selectable visible message");

  await page.evaluate(() => {
    window.__transcriptProgrammaticWrites = [];
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (owner, top) => {
      window.__transcriptProgrammaticWrites.push({ owner, top });
    };
  });

  await page.mouse.move(points.start.x, points.start.y);
  await page.mouse.down();
  const downState = await page.evaluate(({ x, y }) => ({
    mode: document.querySelector(".transcript")?.dataset.scrollMode,
    target: document.elementFromPoint(x, y)?.outerHTML.slice(0, 300),
  }), points.start);
  assert(downState.mode === "native-selecting", `primary pointerdown transfers scroll ownership to native selection (${downState.mode}; ${downState.target})`);
  await page.mouse.move(points.activate.x, points.activate.y, { steps: 6 });
  await page.waitForTimeout(50);
  for (let index = 0; index < 8; index += 1) {
    await page.mouse.wheel(0, -650);
    await page.mouse.move(points.edge.x, points.edge.y, { steps: 4 });
    await page.waitForTimeout(60);
  }
  await page.mouse.move(points.edge.x, points.edge.y);
  await page.waitForFunction(() => {
    const transcript = document.querySelector(".transcript");
    if (!transcript) return false;
    const max = transcript.scrollHeight - transcript.clientHeight;
    return max > 0 && transcript.scrollTop <= max * 0.3;
  }, undefined, { timeout: 30_000 });
  const neutralPoint = await page.evaluate(() => {
    const rect = document.querySelector(".transcript")?.getBoundingClientRect();
    return rect ? { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 } : null;
  });
  assert(neutralPoint != null, "deep logical drag keeps the transcript mounted");
  await page.mouse.move(neutralPoint.x, neutralPoint.y);
  await page.waitForTimeout(100);
  let logicalFocusPoint = null;
  for (let index = 0; index < 40 && !logicalFocusPoint; index += 1) {
    logicalFocusPoint = await page.evaluate(() => {
      const transcript = document.querySelector(".transcript");
      if (!transcript) return null;
      const viewport = transcript.getBoundingClientRect();
      const root = [...transcript.querySelectorAll("[data-transcript-selectable]")].find((element) => {
        const rect = element.getBoundingClientRect();
        const turn = element.textContent?.match(/\bbench turn (\d+):/);
        return turn && Number(turn[1]) <= 16
          && rect.height > 0 && rect.bottom > viewport.top && rect.top < viewport.bottom;
      });
      if (!root) return null;
      const rect = root.getBoundingClientRect();
      return {
        x: Math.min(rect.right - 2, rect.left + 8),
        y: (Math.max(rect.top, viewport.top) + Math.min(rect.bottom, viewport.bottom)) / 2,
      };
    });
    if (!logicalFocusPoint) {
      await page.mouse.wheel(0, -250);
      await page.waitForTimeout(50);
    }
  }
  assert(logicalFocusPoint != null, "deep logical drag settles over a visible 20+ turn target");
  await page.mouse.move(logicalFocusPoint.x, logicalFocusPoint.y);
  await page.waitForFunction(
    () => document.querySelectorAll(".transcript-selection-overlay__rect").length > 0,
    undefined,
    { timeout: 5_000 },
  );

  const during = await page.evaluate(({ x, y }) => {
    const selection = document.getSelection();
    const writes = window.__transcriptProgrammaticWrites ?? [];
    const transcript = document.querySelector(".transcript");
    const viewport = transcript?.getBoundingClientRect();
    const rowIndex = (node) => {
      const element = node instanceof Element ? node : node?.parentElement;
      const value = element?.closest(".transcript__row")?.dataset.index;
      return value == null ? null : Number(value);
    };
    const selectableRoots = [...document.querySelectorAll("[data-transcript-selectable]")];
    const visibleSelectableRows = selectableRoots
      .filter((root) => {
        const rect = root.getBoundingClientRect();
        return viewport && rect.height > 0 && rect.bottom > viewport.top && rect.top < viewport.bottom;
      })
      .map(rowIndex);
    const positiveRangeRows = selectableRoots
      .filter((root) => {
        const range = document.createRange();
        range.selectNodeContents(root);
        return [...range.getClientRects()].some((rect) => rect.width > 0 && rect.height > 0);
      })
      .map(rowIndex);
    const hit = document.elementFromPoint(x, y);
    const caret = document.caretPositionFromPoint?.(x, y)?.offsetNode
      ?? document.caretRangeFromPoint?.(x, y)?.startContainer;
    return {
      collapsed: selection?.isCollapsed ?? true,
      rows: document.querySelectorAll(".transcript__row").length,
      writeCount: writes.length,
      writeOwners: [...new Set(writes.map((write) => write.owner))],
      mode: transcript?.dataset.scrollMode,
      overlayRects: document.querySelectorAll(".transcript-selection-overlay__rect").length,
      scrollTop: transcript?.scrollTop ?? null,
      scrollHeight: transcript?.scrollHeight ?? null,
      clientHeight: transcript?.clientHeight ?? null,
      hitRow: rowIndex(hit),
      hitSelectable: Boolean(hit?.closest("[data-transcript-selectable]")),
      caretRow: rowIndex(caret),
      mountedSelectableRows: selectableRoots.map(rowIndex),
      visibleSelectableRows,
      positiveRangeRows,
    };
  }, logicalFocusPoint);
  assert(during.collapsed, `cross-row drag releases the browser Range after logical promotion (${JSON.stringify(during)})`);
  assert(during.mode === "logical-selecting", `cross-page drag remains owned by logical selection (${during.mode})`);
  assert(during.rows <= Math.ceil(baselineRows * 1.1) + 2, `logical selection keeps the virtual DOM bounded (${baselineRows} → ${during.rows})`);
  assert(during.overlayRects > 0, `logical selection paints mounted-row overlay rectangles (${JSON.stringify(during)})`);
  if (during.writeOwners.some((owner) => owner !== "selection-edge-scroll")) {
    throw new Error(`logical gesture admitted non-selection scroll owners: ${JSON.stringify(during.writeOwners)}`);
  }
  assert(true, "logical gesture rejects every non-selection programmatic scroll owner");

  await page.mouse.up();
  await page.waitForTimeout(250);
  const settled = await page.evaluate(() => ({
    mode: document.querySelector(".transcript")?.dataset.scrollMode,
    overlayRects: document.querySelectorAll(".transcript-selection-overlay__rect").length,
    scrollTop: document.querySelector(".transcript")?.scrollTop ?? 0,
  }));
  assert(settled.mode === "manual", "pointerup settles logical selection without a delayed page jump");
  assert(settled.overlayRects > 0, "settled logical selection keeps its visible overlay");

  await page.evaluate(() => {
    const transcript = document.querySelector(".transcript");
    if (!transcript) return;
    transcript.scrollTop = transcript.scrollHeight;
    transcript.dispatchEvent(new Event("scroll"));
  });
  await page.waitForTimeout(200);
  await page.evaluate((top) => {
    const transcript = document.querySelector(".transcript");
    if (!transcript) return;
    transcript.scrollTop = top;
    transcript.dispatchEvent(new Event("scroll"));
  }, settled.scrollTop);
  await page.waitForTimeout(250);
  const restoredRects = await page.locator(".transcript-selection-overlay__rect").count();
  assert(restoredRects > 0, "logical overlay restores after selected rows scroll out and back in");

  await page.evaluate(() => {
    window.__logicalClipboardText = null;
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: async (text) => { window.__logicalClipboardText = text; } },
    });
  });
  await page.keyboard.press(process.platform === "darwin" ? "Meta+C" : "Control+C");
  await page.waitForFunction(() => typeof window.__logicalClipboardText === "string", undefined, { timeout: 30_000 });
  const copied = await page.evaluate(() => window.__logicalClipboardText);
  const copiedTurns = (copied.match(/bench turn /g) ?? []).length;
  assert(copiedTurns >= 20, `logical copy resolves a 20+ turn frozen snapshot (${copiedTurns} turns)`);

  await page.waitForTimeout(100);
  const after = await page.evaluate(() => {
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = undefined;
    return {
      collapsed: document.getSelection()?.isCollapsed ?? true,
      rows: document.querySelectorAll(".transcript__row").length,
      overlayRects: document.querySelectorAll(".transcript-selection-overlay__rect").length,
    };
  });
  assert(after.collapsed, "logical copy leaves no synthetic browser Range behind");
  assert(after.overlayRects === 0, "successful copy clears the logical overlay");
  assert(after.rows <= Math.ceil(baselineRows * 1.1) + 2, "clearing logical selection preserves the normal virtual DOM window");
  const selectionHeapBaseline = await retainedHeap();

  const forwardPoints = await page.evaluate(() => {
    const transcript = document.querySelector(".transcript");
    if (!transcript) return null;
    const viewport = transcript.getBoundingClientRect();
    const textRects = [...transcript.querySelectorAll("[data-transcript-selectable]")].flatMap((element) => {
      const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
      const rects = [];
      for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        if (!node.textContent?.trim()) continue;
        const range = document.createRange();
        range.selectNodeContents(node);
        rects.push(...range.getClientRects());
      }
      return rects;
    }).filter((rect) => rect.width > 8 && rect.bottom > viewport.top + 4 && rect.top < viewport.bottom - 4);
    const start = textRects[0];
    if (!start) return null;
    const y = (Math.max(start.top, viewport.top + 4) + Math.min(start.bottom, viewport.bottom - 4)) / 2;
    return {
      start: { x: start.left + 2, y },
      activate: { x: Math.min(start.right - 2, start.left + 32), y },
      edge: { x: start.left + 2, y: viewport.bottom - 2 },
    };
  });
  assert(forwardPoints != null, "settled reverse selection leaves a viewport where forward selection can start");
  await page.evaluate(() => {
    window.__transcriptProgrammaticWrites = [];
    window.__logicalClipboardText = null;
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (owner, top) => {
      window.__transcriptProgrammaticWrites.push({ owner, top });
    };
  });
  await page.mouse.move(forwardPoints.start.x, forwardPoints.start.y);
  await page.mouse.down();
  await page.mouse.move(forwardPoints.activate.x, forwardPoints.activate.y, { steps: 6 });
  for (let index = 0; index < 8; index += 1) {
    await page.mouse.wheel(0, 650);
    await page.mouse.move(forwardPoints.edge.x, forwardPoints.edge.y, { steps: 4 });
    await page.waitForTimeout(60);
  }
  await page.mouse.move(forwardPoints.edge.x, forwardPoints.edge.y);
  await page.waitForTimeout(6_000);
  const forwardDuring = await page.evaluate(() => ({
    mode: document.querySelector(".transcript")?.dataset.scrollMode,
    rows: document.querySelectorAll(".transcript__row").length,
    owners: [...new Set((window.__transcriptProgrammaticWrites ?? []).map((write) => write.owner))],
  }));
  assert(forwardDuring.mode === "logical-selecting", "downward cross-page drag also promotes to logical selection");
  assert(forwardDuring.rows <= Math.ceil(baselineRows * 1.1) + 2, "forward logical selection also keeps the virtual DOM bounded");
  assert(forwardDuring.owners.every((owner) => owner === "selection-edge-scroll"), "forward logical gesture preserves scroll ownership");
  await page.mouse.up();
  await page.keyboard.press(process.platform === "darwin" ? "Meta+C" : "Control+C");
  await page.waitForFunction(() => typeof window.__logicalClipboardText === "string", undefined, { timeout: 30_000 });
  const forwardCopiedTurns = await page.evaluate(() => (window.__logicalClipboardText.match(/bench turn /g) ?? []).length);
  assert(forwardCopiedTurns >= 20, `forward logical copy resolves a 20+ turn frozen snapshot (${forwardCopiedTurns} turns)`);
  await page.waitForFunction(() => document.querySelectorAll(".transcript-selection-overlay__rect").length === 0);
  const retainedSelectionBytes = Math.max(0, (await retainedHeap()) - selectionHeapBaseline);
  assert(retainedSelectionBytes <= 2 * 1024 * 1024, `cleared logical selection retains at most 2MiB (${(retainedSelectionBytes / 1024 / 1024).toFixed(2)}MiB)`);
  await page.evaluate(() => { window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = undefined; });
} finally {
  await browser?.close();
  preview.kill("SIGTERM");
}
