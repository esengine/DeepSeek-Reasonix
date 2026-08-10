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
  await page.goto(url, { waitUntil: "domcontentloaded" });
  await page.waitForFunction(() => document.querySelectorAll(".transcript__row").length > 4, undefined, { timeout: 30_000 });
  await page.waitForFunction(() => !document.querySelector(".startup-splash"), undefined, { timeout: 30_000 });
  await page.click('.project-tree__topic-main:has-text("bench:tools-38t")');
  await page.waitForFunction(() => (
    document.querySelector(".project-tree__topic--active .project-tree__topic-label")?.textContent?.includes("bench:tools-38t")
      && document.querySelector(".transcript")?.textContent?.includes("pkg-41/mod.go")
  ), undefined, { timeout: 30_000 });
  await page.waitForTimeout(300);
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
      edge: { x: startX, y: viewport.top + 24 },
    };
  });
  assert(points != null, "bench transcript exposes a selectable visible message");

  await page.evaluate(() => {
    window.__transcriptProgrammaticWrites = [];
    window.__trackTranscriptWrites = true;
    const original = HTMLElement.prototype.scrollTo;
    HTMLElement.prototype.scrollTo = function (...args) {
      if (window.__trackTranscriptWrites && this.classList?.contains("transcript")) {
        window.__transcriptProgrammaticWrites.push(args);
      }
      return original.apply(this, args);
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

  const during = await page.evaluate(() => {
    const selection = document.getSelection();
    return {
      collapsed: selection?.isCollapsed ?? true,
      anchorConnected: selection?.anchorNode?.isConnected ?? false,
      focusConnected: selection?.focusNode?.isConnected ?? false,
      rows: document.querySelectorAll(".transcript__row").length,
      writes: window.__transcriptProgrammaticWrites?.length ?? -1,
      mode: document.querySelector(".transcript")?.dataset.scrollMode,
      anchorRow: selection?.anchorNode?.parentElement?.closest(".transcript__row")?.dataset.rowKey,
      focusRow: selection?.focusNode?.parentElement?.closest(".transcript__row")?.dataset.rowKey,
    };
  });
  assert(!during.collapsed, "real mouse drag creates a non-collapsed cross-page selection");
  assert(during.anchorConnected && during.focusConnected, "native Selection endpoints remain connected while scrolling");
  assert(during.anchorRow !== during.focusRow, `real drag crosses selectable rows (${during.anchorRow} → ${during.focusRow})`);
  assert(during.rows > baselineRows, `native fallback retains the selected row interval (${baselineRows} → ${during.rows})`);
  if (during.writes !== 0) {
    throw new Error(`selection gesture in mode ${during.mode} allowed ${during.writes} programmatic transcript scroll writes`);
  }
  assert(true, "selection gesture rejects programmatic transcript scroll writes");

  await page.mouse.up();
  await page.keyboard.press("Escape");
  await page.waitForTimeout(250);
  const after = await page.evaluate(() => {
    window.__trackTranscriptWrites = false;
    return {
      collapsed: document.getSelection()?.isCollapsed ?? true,
      rows: document.querySelectorAll(".transcript__row").length,
    };
  });
  assert(after.collapsed, "Escape clears the retained native selection");
  assert(after.rows <= Math.ceil(baselineRows * 1.1) + 2, "clearing selection restores the normal virtual DOM window");
} finally {
  await browser?.close();
  preview.kill("SIGTERM");
}
