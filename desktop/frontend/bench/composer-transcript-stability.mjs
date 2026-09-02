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
const port = Number(process.env.REASONIX_COMPOSER_SCROLL_PORT ?? 4622);
const url = `http://127.0.0.1:${port}/?mock=bench&bench=1&platform=windows`;

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
  throw new Error("composer scroll test server did not become ready");
}

async function clickIfVisible(page, selector) {
  const locator = page.locator(selector);
  if (await locator.count() > 0 && await locator.first().isVisible()) {
    await locator.first().click();
    return true;
  }
  return false;
}

async function waitForTail(page) {
  try {
    await page.waitForFunction(() => {
      const transcript = document.querySelector(".transcript");
      return transcript instanceof HTMLElement
        && transcript.scrollHeight - transcript.scrollTop - transcript.clientHeight <= 4;
    }, undefined, { timeout: 15_000 });
  } catch (error) {
    const state = await page.evaluate(() => {
      const transcript = document.querySelector(".transcript");
      return transcript instanceof HTMLElement ? {
        top: transcript.scrollTop,
        height: transcript.scrollHeight,
        clientHeight: transcript.clientHeight,
        distance: transcript.scrollHeight - transcript.scrollTop - transcript.clientHeight,
        mode: transcript.dataset.scrollMode,
        jumpBottom: Boolean(document.querySelector(".transcript__jump-bottom")),
      } : null;
    });
    throw new Error(`composer fixture did not reach the physical tail (${JSON.stringify(state)})`, { cause: error });
  }
  await page.evaluate(() => new Promise((resolve) => {
    let previous = null;
    let stableFrames = 0;
    const sample = () => {
      const transcript = document.querySelector(".transcript");
      if (!(transcript instanceof HTMLElement)) return requestAnimationFrame(sample);
      const current = [transcript.scrollTop, transcript.scrollHeight, transcript.clientHeight];
      const unchanged = previous != null && current.every((value, index) => Math.abs(value - previous[index]) <= 0.5);
      stableFrames = unchanged ? stableFrames + 1 : 0;
      previous = current;
      if (stableFrames >= 6) resolve();
      else requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
  }));
}

async function resetScrollProbe(page) {
  return page.evaluate(() => {
    const transcript = document.querySelector(".transcript");
    if (!(transcript instanceof HTMLElement)) throw new Error("transcript is unavailable");
    window.__composerScrollProbe = { samples: [], writes: [] };
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => window.__composerScrollProbe.writes.push(write);
    const record = (source) => window.__composerScrollProbe.samples.push({
      source,
      top: transcript.scrollTop,
      height: transcript.scrollHeight,
      clientHeight: transcript.clientHeight,
      distance: transcript.scrollHeight - transcript.scrollTop - transcript.clientHeight,
      at: performance.now(),
    });
    if (!window.__composerScrollProbeInstalled) {
      transcript.addEventListener("scroll", () => record("scroll"), { passive: true });
      new ResizeObserver(() => record("resize")).observe(transcript);
      window.__composerScrollProbeInstalled = true;
    }
    record("baseline");
    return window.__composerScrollProbe.samples[0];
  });
}

async function readScrollProbe(page) {
  return page.evaluate(() => {
    const transcript = document.querySelector(".transcript");
    const probe = window.__composerScrollProbe;
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = undefined;
    if (!(transcript instanceof HTMLElement) || !probe) throw new Error("composer scroll probe is unavailable");
    return {
      samples: probe.samples,
      writes: probe.writes,
      final: {
        top: transcript.scrollTop,
        height: transcript.scrollHeight,
        clientHeight: transcript.clientHeight,
        distance: transcript.scrollHeight - transcript.scrollTop - transcript.clientHeight,
        mode: transcript.dataset.scrollMode,
      },
    };
  });
}

async function settleFrames(page, count = 4) {
  await page.evaluate((frames) => new Promise((resolve) => {
    const settle = () => {
      frames -= 1;
      if (frames <= 0) resolve();
      else requestAnimationFrame(settle);
    };
    requestAnimationFrame(settle);
  }), count);
}

const server = await startPreviewServer(frontendDir, port);

let browser;
try {
  await waitForServer();
  browser = await chromium.launch({
    headless: true,
    ...(process.env.PLAYWRIGHT_EXECUTABLE_PATH ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH } : {}),
  });
  const page = await browser.newPage({
    viewport: { width: 1095, height: 720 },
    deviceScaleFactor: 2,
  });
  const pageErrors = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  await page.goto(url, { waitUntil: "domcontentloaded" });
  await page.waitForFunction(() => !document.querySelector(".startup-splash"), undefined, { timeout: 30_000 });
  await page.click('.project-tree__topic-main:has-text("bench:tools-38t")');
  await page.waitForFunction(() => (
    document.querySelector(".project-tree__topic--active .project-tree__topic-label")?.textContent?.includes("bench:tools-38t")
      && document.querySelector(".transcript")?.textContent?.includes("pkg-41/mod.go")
  ), undefined, { timeout: 30_000 });
  await page.waitForFunction(() => !document.querySelector(".transcript-navigation-overlay"), undefined, { timeout: 30_000 });

  await clickIfVisible(page, ".transcript__jump-bottom");
  await waitForTail(page);

  const input = page.locator("textarea.composer__input:not(.composer__input--measure)");
  await input.fill("existing first line\nexisting second line");
  await waitForTail(page);
  const multilineHeight = await input.evaluate((element) => element.getBoundingClientRect().height);
  assert(multilineHeight > 32, `fixture starts with an existing multiline draft (${multilineHeight.toFixed(1)}px)`);

  const baseline = await resetScrollProbe(page);

  await input.focus();
  await input.press("End");
  await input.type("abcdef", { delay: 80 });
  for (let index = 0; index < 6; index += 1) {
    await input.press("Backspace");
    await page.waitForTimeout(80);
  }
  await page.waitForTimeout(350);

  const result = await readScrollProbe(page);
  const minTop = Math.min(baseline.top, ...result.samples.map((sample) => sample.top));
  const maxReverse = baseline.top - minTop;
  const geometryChanges = result.samples.filter((sample) => (
    Math.abs(sample.height - baseline.height) > 0.5 || sample.clientHeight !== baseline.clientHeight
  ));
  assert(maxReverse <= 1, `ordinary input/delete never displaces scrollTop away from the tail (${maxReverse.toFixed(1)}px)`);
  assert(geometryChanges.length === 0, `ordinary input/delete keeps transcript geometry stable (${geometryChanges.length} changes)`);
  assert(result.final.mode === "tail-follow" && result.final.distance <= 4,
    `ordinary input/delete finishes at the physical tail (${result.final.distance.toFixed(1)}px)`);

  // Locate the exact character that causes a visual line wrap at this viewport,
  // then replay only that character while observing the reader. A real growth
  // may advance the physical tail, but it must never move in reverse or bounce.
  await input.fill("xxxxxxxx");
  await waitForTail(page);
  const singleLineHeight = await input.evaluate((element) => element.getBoundingClientRect().height);
  let lastSingleLineLength = 8;
  let firstWrappedLength = 256;
  while (lastSingleLineLength + 1 < firstWrappedLength) {
    const candidate = Math.floor((lastSingleLineLength + firstWrappedLength) / 2);
    await input.fill("x".repeat(candidate));
    await settleFrames(page, 2);
    const height = await input.evaluate((element) => element.getBoundingClientRect().height);
    if (height <= singleLineHeight + 1) lastSingleLineLength = candidate;
    else firstWrappedLength = candidate;
  }
  await input.fill("x".repeat(lastSingleLineLength));
  await waitForTail(page);
  const wrapBaseline = await resetScrollProbe(page);
  await input.type("x");
  await page.waitForTimeout(350);
  const wrapResult = await readScrollProbe(page);
  const wrappedHeight = await input.evaluate((element) => element.getBoundingClientRect().height);
  const wrapTops = [wrapBaseline.top, ...wrapResult.samples.map((sample) => sample.top)];
  const wrapDistinctTops = [...new Set(wrapTops.map((top) => Math.round(top * 2) / 2))];
  assert(wrappedHeight > singleLineHeight + 1,
    `fixture crosses exactly one visual line boundary (${singleLineHeight.toFixed(1)}px → ${wrappedHeight.toFixed(1)}px)`);
  assert(Math.min(...wrapTops) >= wrapBaseline.top - 1,
    `a real line wrap moves only toward the new tail (${wrapBaseline.top.toFixed(1)}px → ${wrapResult.final.top.toFixed(1)}px)`);
  assert(wrapDistinctTops.length <= 2,
    `a real line wrap performs at most one visible tail adjustment (${JSON.stringify(wrapDistinctTops)})`);
  assert(wrapResult.final.mode === "tail-follow" && wrapResult.final.distance <= 4,
    `a real line wrap settles at the physical tail (${wrapResult.final.distance.toFixed(1)}px)`);

  // Reader mode is user-owned: editing a draft while reading upward must not
  // reclaim the viewport or emit a compensating tail movement.
  await input.fill("existing first line\nexisting second line");
  await waitForTail(page);
  const transcript = page.locator(".transcript");
  const transcriptBox = await transcript.boundingBox();
  if (!transcriptBox) throw new Error("transcript has no visible bounding box");
  await page.mouse.move(transcriptBox.x + transcriptBox.width / 2, transcriptBox.y + transcriptBox.height / 2);
  await page.mouse.wheel(0, -600);
  await page.waitForFunction(() => document.querySelector(".transcript")?.dataset.scrollMode === "manual", undefined, { timeout: 5_000 });
  await page.waitForTimeout(150);
  await input.focus();
  const readerBaseline = await resetScrollProbe(page);
  await input.type("z");
  await input.press("Backspace");
  await page.waitForTimeout(250);
  const readerResult = await readScrollProbe(page);
  const readerDeviation = Math.max(0, ...readerResult.samples.map((sample) => Math.abs(sample.top - readerBaseline.top)));
  assert(readerDeviation <= 1, `editing while reading upward preserves scrollTop (${readerDeviation.toFixed(1)}px deviation)`);
  assert(readerResult.final.mode === "manual", "editing while reading upward preserves manual reader ownership");

  // A saved manual composer height uses the same off-flow mirror. Once the
  // resize itself settles, ordinary edits must leave reader geometry untouched.
  await clickIfVisible(page, ".transcript__jump-bottom");
  await waitForTail(page);
  const resizeHandle = page.locator(".composer-resize-handle");
  await resizeHandle.focus();
  await resizeHandle.press("ArrowUp");
  await resizeHandle.press("ArrowUp");
  await waitForTail(page);
  assert(await page.locator(".composer-card--resized").count() === 1, "fixture enters user-resized composer mode");
  await input.focus();
  const resizedBaseline = await resetScrollProbe(page);
  await input.type("q");
  await input.press("Backspace");
  await page.waitForTimeout(250);
  const resizedResult = await readScrollProbe(page);
  const resizedReverse = resizedBaseline.top - Math.min(resizedBaseline.top, ...resizedResult.samples.map((sample) => sample.top));
  assert(resizedReverse <= 1, `editing a user-resized composer does not reverse scrollTop (${resizedReverse.toFixed(1)}px)`);
  assert(resizedResult.samples.every((sample) => (
    Math.abs(sample.height - resizedBaseline.height) <= 0.5 && sample.clientHeight === resizedBaseline.clientHeight
  )), "editing a user-resized composer keeps transcript geometry stable");

  // Re-enter autosize mode and replay Chromium's IME event order. The
  // provisional value is measured by the mirror without collapsing the live
  // textarea in the layout flow.
  await resizeHandle.dblclick();
  await waitForTail(page);
  await input.focus();
  const imeBaseline = await resetScrollProbe(page);
  await input.evaluate((element) => {
    element.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true, data: "" }));
    const nextValue = `${element.value}你`;
    const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")?.set;
    setter?.call(element, nextValue);
    element.setSelectionRange(nextValue.length, nextValue.length);
    element.dispatchEvent(new InputEvent("input", {
      bubbles: true,
      data: "你",
      inputType: "insertCompositionText",
      isComposing: true,
    }));
  });
  await page.waitForTimeout(80);
  await input.evaluate((element) => {
    element.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true, data: "你" }));
  });
  await page.waitForTimeout(300);
  const imeResult = await readScrollProbe(page);
  const imeReverse = imeBaseline.top - Math.min(imeBaseline.top, ...imeResult.samples.map((sample) => sample.top));
  assert(imeReverse <= 1, `IME composition does not reverse scrollTop (${imeReverse.toFixed(1)}px)`);
  assert(imeResult.final.mode === "tail-follow" && imeResult.final.distance <= 4,
    `IME composition finishes at the physical tail (${imeResult.final.distance.toFixed(1)}px)`);
  assert(pageErrors.length === 0, `browser reports no page errors (${pageErrors.length})`);
} finally {
  if (browser) await browser.close();
  await server.close();
}
