#!/usr/bin/env node

import http from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { startPreviewServer } from "./vite-preview-server.mjs";

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
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error("transcript selection preview did not become ready");
}

async function frames(page, count = 4) {
  await page.evaluate((remaining) => new Promise((resolve) => {
    const tick = () => {
      remaining -= 1;
      if (remaining <= 0) resolve();
      else requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  }), count);
}

async function loadFixture(page, label, marker) {
  await page.click(`.project-tree__topic-main:has-text("${label}")`);
  await page.waitForFunction(({ label, marker }) => {
    const transcript = document.querySelector(".transcript");
    return document.querySelector(".project-tree__topic--active .project-tree__topic-label")?.textContent?.includes(label)
      && transcript instanceof HTMLElement
      && transcript.dataset.transcriptHydrating === "false"
      && transcript.textContent?.includes(marker)
      && !document.querySelector(".transcript-navigation-overlay");
  }, { label, marker }, { timeout: 30_000 });
  await frames(page, 6);
  return page.locator(".transcript");
}

async function textDragPoints(page) {
  return page.evaluate(() => {
    const transcript = document.querySelector(".transcript");
    if (!(transcript instanceof HTMLElement)) return null;
    const viewport = transcript.getBoundingClientRect();
    const roots = [...transcript.querySelectorAll("[data-transcript-selectable]")]
      .filter((root) => {
        const rect = root.getBoundingClientRect();
        return rect.height > 0 && rect.bottom > viewport.top + 20 && rect.top < viewport.bottom - 20;
      });
    const point = (root, ratio) => {
      const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
      for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        if (!node.textContent?.trim()) continue;
        const range = document.createRange();
        range.selectNodeContents(node);
        const rect = [...range.getClientRects()].find((candidate) => candidate.width > 12
          && candidate.bottom > viewport.top + 8 && candidate.top < viewport.bottom - 8);
        if (rect) return {
          x: Math.max(viewport.left + 4, Math.min(viewport.right - 4, rect.left + rect.width * ratio)),
          y: (Math.max(rect.top, viewport.top) + Math.min(rect.bottom, viewport.bottom)) / 2,
          rowKey: root.closest("[data-row-key]")?.getAttribute("data-row-key"),
          blockKey: root.closest("[data-transcript-block-key]")?.getAttribute("data-transcript-block-key"),
        };
      }
      return null;
    };
    const first = roots[0] ? point(roots[0], 0.2) : null;
    const last = roots.at(-1) ? point(roots.at(-1), 0.8) : null;
    return first && last && first.rowKey !== last.rowKey ? { first, last } : null;
  });
}

async function beginNativeSelection(page, initialPoints) {
  let points = initialPoints;
  for (let attempt = 0; attempt < 3; attempt += 1) {
    await page.mouse.move(points.first.x, points.first.y);
    await page.mouse.down();
    await page.mouse.move(points.last.x, points.last.y, { steps: 16 + attempt * 8 });
    await frames(page, 4);
    const active = await page.evaluate(() =>
      document.querySelector(".transcript")?.getAttribute("data-scroll-mode") === "selection");
    if (active) return points;
    await page.mouse.up();
    await page.evaluate(() => document.getSelection()?.removeAllRanges());
    await frames(page, 2);
    points = await textDragPoints(page) ?? points;
  }
  throw new Error("native text drag did not transfer transcript ownership to selection");
}

async function runTableRepaint(page) {
  const transcript = await loadFixture(page, "bench:selection-table", "SELECTION REPAINT TARGET");
  const target = page.locator("strong", { hasText: "SELECTION REPAINT TARGET" });
  await target.scrollIntoViewIfNeeded();
  const box = await target.boundingBox();
  assert(box != null, "selection fixture exposes the strong-text target");
  const before = await transcript.evaluate((element) => ({ top: element.scrollTop, height: element.scrollHeight }));
  await page.mouse.dblclick(box.x + box.width / 2, box.y + box.height / 2);
  await frames(page, 4);
  const after = await transcript.evaluate((element) => ({
    top: element.scrollTop,
    height: element.scrollHeight,
    selected: document.getSelection()?.toString() ?? "",
  }));
  assert(after.selected.includes("SELECTION") || after.selected.includes("REPAINT") || after.selected.includes("TARGET"),
    "native multi-click produces a real text selection");
  assert(Math.abs(after.top - before.top) <= 1 && Math.abs(after.height - before.height) <= 1,
    "native selection repaint keeps viewport geometry stable");
  await page.keyboard.press("Escape");
  await page.evaluate(() => document.getSelection()?.removeAllRanges());
}

async function runWindowedSelection(page) {
  const transcript = await loadFixture(page, "bench:windowed-1000t", "Windowed turn 1000");
  await page.evaluate(() => {
    const button = document.querySelector(".transcript__jump-bottom:not([hidden])");
    if (button instanceof HTMLElement) button.click();
  });
  await page.waitForFunction(() => {
    const element = document.querySelector(".transcript");
    return element instanceof HTMLElement
      && element.dataset.transcriptIntent === "tail"
      && element.scrollHeight - element.scrollTop - element.clientHeight <= 4;
  }, undefined, { timeout: 15_000 });
  const box = await transcript.boundingBox();
  if (!box) throw new Error("windowed transcript viewport missing");
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  for (let step = 0; step < 8; step += 1) {
    await page.mouse.wheel(0, -420);
    await frames(page, 2);
  }
  await page.waitForFunction(() => document.querySelector(".transcript")?.getAttribute("data-transcript-intent") === "reader",
    undefined, { timeout: 5_000 });

  let points = await textDragPoints(page);
  for (let attempt = 0; attempt < 6 && !points; attempt += 1) {
    await page.mouse.wheel(0, -180);
    await frames(page, 2);
    points = await textDragPoints(page);
  }
  assert(points?.first.blockKey && points?.last.blockKey, "windowed viewport exposes selectable endpoints in stable blocks");

  await page.evaluate(() => {
    window.__selectionWrites = [];
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => window.__selectionWrites.push(write);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: async (text) => { window.__selectionClipboard = text; } },
    });
  });
  points = await beginNativeSelection(page, points);
  await frames(page, 3);
  const active = await page.evaluate((endpointKeys) => {
    const transcript = document.querySelector(".transcript");
    const projection = transcript?.querySelector(".transcript__projection");
    const mounted = [...document.querySelectorAll("[data-transcript-block-key]")]
      .map((block) => block.getAttribute("data-transcript-block-key"));
    return {
      mode: transcript?.getAttribute("data-scroll-mode"),
      protectedCount: Number(document.querySelector(".transcript-shell")?.getAttribute("data-protected-blocks")),
      endpointsMounted: endpointKeys.every((key) => mounted.includes(key)),
      mounted: Number(projection?.getAttribute("data-transcript-mounted-blocks")),
      nativeText: document.getSelection()?.toString() ?? "",
      writes: window.__selectionWrites ?? [],
    };
  }, [points.first.blockKey, points.last.blockKey]);
  assert(active.mode === "selection", "non-collapsed drag transfers ownership to selection");
  assert(active.protectedCount >= 1 && active.endpointsMounted, "selection endpoint blocks remain resident by stable key");
  assert(active.mounted <= 40, `selection keeps the windowed DOM bounded (${active.mounted} blocks)`);
  assert(active.writes.filter((write) => write.outcome === "accepted" && write.owner !== "selection-edge-scroll").length === 0,
    "selection admits no non-selection physical scroll writes");

  await transcript.evaluate((element) => {
    const viewport = element.getBoundingClientRect();
    const first = [...element.querySelectorAll(".transcript__window-item")]
      .find((item) => item.getBoundingClientRect().bottom > viewport.top);
    const block = first;
    if (block instanceof HTMLElement) block.style.paddingBottom = "180px";
  });
  await frames(page, 8);
  const duringRemeasure = await page.evaluate(() => ({
    mode: document.querySelector(".transcript")?.getAttribute("data-scroll-mode"),
    accepted: (window.__selectionWrites ?? []).filter((write) => write.outcome === "accepted" && write.owner !== "selection-edge-scroll").length,
    visible: [...document.querySelectorAll("[data-transcript-block-key]")].some((block) => {
      const transcript = document.querySelector(".transcript");
      const viewport = transcript?.getBoundingClientRect();
      const rect = block.getBoundingClientRect();
      return viewport && rect.height > 0 && rect.bottom > viewport.top && rect.top < viewport.bottom;
    }),
  }));
  assert(duringRemeasure.mode === "selection" && duringRemeasure.accepted === 0,
    "measurement changes cannot run structural corrections during selection");
  assert(duringRemeasure.visible, "selection-time measurement changes keep visible block coverage");

  await page.mouse.up();
  await frames(page, 3);
  const settledSelection = await page.evaluate(() => ({
    text: document.getSelection()?.toString() ?? "",
    overlay: document.querySelectorAll(".transcript-selection-overlay__rect").length,
  }));
  assert(settledSelection.text.length > 0 || settledSelection.overlay > 0,
    "settled selection retains its native or logical representation");
  await page.keyboard.press(process.platform === "darwin" ? "Meta+C" : "Control+C");
  await page.waitForFunction(() => typeof window.__selectionClipboard === "string", undefined, { timeout: 15_000 });
  const copied = await page.evaluate(() => window.__selectionClipboard);
  assert(copied.length > 0, `selection copy resolves the frozen text projection (${copied.length} characters)`);
  await page.waitForFunction(() => document.querySelectorAll(".transcript-selection-overlay__rect").length === 0,
    undefined, { timeout: 5_000 });
  await page.evaluate(() => {
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = undefined;
    window.__selectionWrites = undefined;
    window.__selectionClipboard = undefined;
  });
}

const preview = await startPreviewServer(frontendDir, port);
let browser;
try {
  await waitForServer();
  browser = await chromium.launch({
    headless: true,
    args: ["--js-flags=--expose-gc"],
    ...(process.env.PLAYWRIGHT_EXECUTABLE_PATH ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH } : {}),
  });
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(url, { waitUntil: "domcontentloaded" });
  await page.waitForFunction(() => !document.querySelector(".startup-splash"), undefined, { timeout: 30_000 });
  await runTableRepaint(page);
  await runWindowedSelection(page);
  assert(errors.length === 0, `selection replay emits no page errors (${errors.length})`);
  process.stdout.write("transcript selection browser replay passed\n");
} finally {
  await browser?.close();
  await preview.close();
}
