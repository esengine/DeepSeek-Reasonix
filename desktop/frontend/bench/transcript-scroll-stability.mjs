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
const port = Number(process.env.REASONIX_TRANSCRIPT_SCROLL_PORT ?? 4619);
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
  throw new Error("transcript scroll preview did not become ready");
}

const preview = spawn("pnpm", ["exec", "vite", "preview", "--port", String(port), "--strictPort", "--host", "127.0.0.1"], {
  cwd: frontendDir,
  stdio: "ignore",
});

let browser;
try {
  await waitForServer();
  browser = await chromium.launch({
    headless: true,
    ...(process.env.PLAYWRIGHT_EXECUTABLE_PATH ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH } : {}),
  });
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  await page.goto(url, { waitUntil: "domcontentloaded" });
  await page.waitForFunction(() => !document.querySelector(".startup-splash"), undefined, { timeout: 30_000 });
  await page.click('.project-tree__topic-main:has-text("bench:tools-38t")');
  await page.waitForFunction(() => document.querySelectorAll(".transcript__row").length > 4, undefined, { timeout: 30_000 });
  await page.waitForFunction(() => document.querySelector(".transcript")?.textContent?.includes("pkg-41/mod.go"), undefined, { timeout: 30_000 });

  const transcript = page.locator(".transcript");
  const box = await transcript.boundingBox();
  assert(box != null, "bench exposes the Virtuoso transcript viewport");
  assert(await page.locator('[data-virtuoso-scroller="true"]').count() === 1, "Transcript is backed by React Virtuoso");

  // Start away from either edge and record a visible stable row. Growing an
  // already-mounted row above it reproduces async Markdown/tool hydration.
  const beforeGrowth = await transcript.evaluate((element) => {
    element.scrollTop = Math.max(0, element.scrollHeight - element.clientHeight * 2);
    element.dispatchEvent(new Event("scroll"));
    const viewport = element.getBoundingClientRect();
    const rows = [...element.querySelectorAll(".transcript__row")];
    const visible = rows
      .filter((row) => row.getBoundingClientRect().bottom > viewport.top && row.getBoundingClientRect().top < viewport.bottom)
      .sort((left, right) => left.getBoundingClientRect().top - right.getBoundingClientRect().top);
    const anchor = visible.find((row) => row.getBoundingClientRect().top >= viewport.top) ?? visible[0];
    const above = rows
      .filter((row) => row.getBoundingClientRect().bottom <= anchor?.getBoundingClientRect().top)
      .sort((left, right) => right.getBoundingClientRect().bottom - left.getBoundingClientRect().bottom)[0];
    return {
      top: element.scrollTop,
      anchorKey: anchor?.dataset.rowKey ?? null,
      anchorOffset: anchor ? anchor.getBoundingClientRect().top - viewport.top : null,
      grownKey: above?.dataset.rowKey ?? null,
    };
  });
  assert(beforeGrowth.anchorKey && beforeGrowth.grownKey, "bench exposes a visible anchor and mounted dynamic row above it");

  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await transcript.evaluate((element) => {
    const initialTop = element.scrollTop;
    let previousTop = initialTop;
    let stableFrames = 0;
    element.dataset.benchWheelSettled = "false";
    const sample = () => {
      const currentTop = element.scrollTop;
      const moved = Math.abs(currentTop - initialTop) > 1;
      stableFrames = moved && Math.abs(currentTop - previousTop) <= 0.5 ? stableFrames + 1 : 0;
      previousTop = currentTop;
      if (stableFrames >= 2) {
        element.dataset.benchWheelSettled = "true";
        return;
      }
      requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
  });
  await page.mouse.wheel(0, -360);
  await page.waitForFunction(() => document.querySelector(".transcript")?.dataset.benchWheelSettled === "true");
  const gestureStart = await transcript.evaluate((element) => element.scrollTop);
  await transcript.evaluate((element) => {
    const viewport = element.getBoundingClientRect();
    const rows = [...element.querySelectorAll(".transcript__row")];
    const visible = rows
      .filter((row) => row.getBoundingClientRect().bottom > viewport.top && row.getBoundingClientRect().top < viewport.bottom)
      .sort((left, right) => left.getBoundingClientRect().top - right.getBoundingClientRect().top);
    const above = rows
      .filter((row) => row.getBoundingClientRect().bottom <= viewport.top)
      .sort((left, right) => right.getBoundingClientRect().bottom - left.getBoundingClientRect().bottom)[0];
    if (above instanceof HTMLElement) above.style.paddingBottom = `${Number.parseFloat(above.style.paddingBottom || "0") + 1200}px`;
    window.__reasonixScrollSamples = [];
    const sample = () => {
      const currentRows = [...element.querySelectorAll(".transcript__row")];
      const rect = element.getBoundingClientRect();
      const occupied = currentRows.some((row) => {
        const rowRect = row.getBoundingClientRect();
        return rowRect.bottom > rect.top && rowRect.top < rect.bottom;
      });
      window.__reasonixScrollSamples.push({ top: element.scrollTop, occupied });
    };
    element.addEventListener("scroll", sample, { passive: true });
    sample();
  });
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  const afterGrowth = await transcript.evaluate((element) => ({
    top: element.scrollTop,
    samples: window.__reasonixScrollSamples ?? [],
  }));
  assert(afterGrowth.top >= gestureStart - 2, `dynamic measurement never reverses an upward gesture into a multi-screen jump (${gestureStart} → ${afterGrowth.top})`);
  assert(afterGrowth.samples.every((sample) => sample.occupied), "dynamic measurement never exposes a blank transcript viewport");

  // Rapid direction changes are the exact user report. Sample every frame and
  // require that Virtuoso always maintains mounted coverage.
  for (const delta of [-700, -700, 480, -600, 520, -460]) {
    await page.mouse.wheel(0, delta);
    await page.waitForTimeout(24);
  }
  const rapid = await transcript.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    const visible = [...element.querySelectorAll(".transcript__row")].filter((row) => {
      const rowRect = row.getBoundingClientRect();
      return rowRect.bottom > rect.top && rowRect.top < rect.bottom;
    });
    return { visible: visible.length, top: element.scrollTop, max: element.scrollHeight - element.clientHeight };
  });
  assert(rapid.visible > 0, `rapid bidirectional scrolling leaves rendered coverage (${rapid.visible} visible rows)`);
  assert(rapid.top >= 0 && rapid.top <= rapid.max + 1, `rapid scrolling stays within the native scroll range (${rapid.top}/${rapid.max})`);

  // A native scrollbar thumb drag owns the browser's scroll range. Keep
  // Virtuoso's estimated size tree fixed until pointer release so newly
  // visited variable-height rows cannot resize the thumb under the pointer.
  // This is deliberately pointer-gutter-specific; the wheel assertions above
  // continue to exercise ordinary chat-content scrolling and live measuring.
  await transcript.evaluate((element) => {
    element.scrollTop = 0;
    element.dispatchEvent(new Event("scroll"));
  });
  await page.waitForFunction(() => document.querySelector(".transcript__row"));
  const nativeThumbProbe = await transcript.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    const scaleX = rect.width / element.offsetWidth;
    const contentRight = rect.left + (element.clientLeft + element.clientWidth) * scaleX;
    const row = element.querySelector(".transcript__row");
    if (!(row instanceof HTMLElement)) return null;
    row.dataset.nativeScrollbarProbe = "true";
    return {
      x: Math.min(rect.right - 1, contentRight + Math.max(1, (rect.right - contentRight) / 2)),
      y: rect.top + 5,
      knownSize: Number.parseFloat(row.dataset.knownSize || "0"),
      gutter: rect.right - contentRight,
      scrollHeight: element.scrollHeight,
    };
  });
  assert(nativeThumbProbe && nativeThumbProbe.gutter > 1, `workbench exposes a native scrollbar gutter (${nativeThumbProbe?.gutter ?? 0}px)`);
  assert(nativeThumbProbe.knownSize > 0, `native scrollbar probe starts from a measured row (${nativeThumbProbe.knownSize}px)`);
  await page.mouse.move(nativeThumbProbe.x, nativeThumbProbe.y);
  await page.mouse.down();
  await page.waitForFunction(() => document.querySelector(".transcript")?.dataset.nativeScrollbarDrag === "true");
  await page.waitForFunction(
    (knownSize) => document.querySelector('[data-native-scrollbar-probe="true"]')?.style.height === `${knownSize}px`,
    nativeThumbProbe.knownSize,
  );
  await transcript.evaluate((element) => {
    const row = element.querySelector('[data-native-scrollbar-probe="true"]');
    const content = row?.firstElementChild;
    if (content instanceof HTMLElement) content.style.paddingBottom = `${Number.parseFloat(content.style.paddingBottom || "0") + 900}px`;
  });
  await page.waitForTimeout(100);
  const duringNativeThumbDrag = await transcript.evaluate((element) => {
    const row = element.querySelector('[data-native-scrollbar-probe="true"]');
    return {
      knownSize: row instanceof HTMLElement ? Number.parseFloat(row.dataset.knownSize || "0") : 0,
      fixedHeight: row instanceof HTMLElement ? row.style.height : "",
      rowHeight: row instanceof HTMLElement ? row.getBoundingClientRect().height : 0,
      listHeight: element.querySelector('[data-testid="virtuoso-item-list"]')?.getBoundingClientRect().height ?? 0,
      scrollHeight: element.scrollHeight,
    };
  });
  assert(duringNativeThumbDrag.knownSize === nativeThumbProbe.knownSize, `native thumb drag freezes new row measurements (${duringNativeThumbDrag.knownSize}px)`);
  assert(duringNativeThumbDrag.fixedHeight === `${nativeThumbProbe.knownSize}px`, `native thumb drag fixes mounted row layout (${duringNativeThumbDrag.fixedHeight})`);
  assert(Math.abs(duringNativeThumbDrag.scrollHeight - nativeThumbProbe.scrollHeight) <= 8, `native thumb drag keeps the physical scroll range stable (${nativeThumbProbe.scrollHeight} → ${duringNativeThumbDrag.scrollHeight}; row ${duringNativeThumbDrag.rowHeight}; list ${duringNativeThumbDrag.listHeight})`);
  await page.mouse.up();
  await page.waitForFunction(
    (knownSize) => {
      const transcriptElement = document.querySelector(".transcript");
      const row = document.querySelector('[data-native-scrollbar-probe="true"]');
      return transcriptElement?.dataset.nativeScrollbarDrag !== "true"
        && row instanceof HTMLElement
        && Number.parseFloat(row.dataset.knownSize || "0") > knownSize + 800;
    },
    nativeThumbProbe.knownSize,
  );
  assert(true, "native thumb release resumes real row measurement");

  // Explicit bottom owns the tail. Subsequent async growth must use Virtuoso's
  // autoscroll API and remain at the physical bottom without Reasonix scrollTop
  // correction loops.
  const jumpBottom = page.locator(".transcript__jump-bottom");
  await transcript.evaluate((element) => {
    element.scrollTop = Math.max(0, element.scrollHeight - element.clientHeight * 2);
  });
  await jumpBottom.waitFor({ state: "visible" });
  await jumpBottom.click();
  await page.waitForFunction(() => {
    const element = document.querySelector(".transcript");
    return element
      && element.dataset.scrollMode === "tail-follow"
      && element.scrollHeight - element.scrollTop - element.clientHeight <= 1;
  });
  await transcript.evaluate((element) => {
    const tail = [...element.querySelectorAll(".transcript__row")].at(-1);
    if (tail instanceof HTMLElement) tail.style.paddingBottom = `${Number.parseFloat(tail.style.paddingBottom || "0") + 320}px`;
  });
  await page.waitForFunction(() => {
    const element = document.querySelector(".transcript");
    return element && element.scrollHeight - element.scrollTop - element.clientHeight <= 1;
  });
  assert(true, "pinned dynamic tail growth remains at the physical bottom");

  process.stdout.write("\ntranscript scroll stability browser gate passed\n");
} finally {
  await browser?.close();
  preview.kill("SIGTERM");
}
