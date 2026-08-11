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
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  await page.goto(url, { waitUntil: "domcontentloaded" });
  await page.waitForFunction(() => document.querySelectorAll(".transcript__row").length > 4, undefined, { timeout: 30_000 });
  await page.waitForFunction(() => !document.querySelector(".startup-splash"), undefined, { timeout: 30_000 });
  await page.click('.project-tree__topic-main:has-text("bench:tools-38t")');
  await page.waitForFunction(() => document.querySelector(".transcript")?.textContent?.includes("pkg-41/mod.go"), undefined, { timeout: 30_000 });

  await page.evaluate(() => {
    const transcript = document.querySelector(".transcript");
    if (!transcript) return;
    transcript.scrollTop = Math.max(0, transcript.scrollHeight - transcript.clientHeight * 2);
    window.__scrollWrites = [];
    window.__scrollGestureTrace = [];
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (owner, top) => window.__scrollWrites.push({ owner, top });
    new MutationObserver(() => {
      window.__scrollGestureTrace.push(transcript.dataset.scrollGesture ?? "idle");
    }).observe(transcript, { attributes: true, attributeFilter: ["data-scroll-gesture"] });
  });

  const box = await page.locator(".transcript").boundingBox();
  assert(box != null, "bench exposes the transcript viewport");
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.wheel(0, -420);
  await page.waitForFunction(() => window.__scrollGestureTrace?.includes("wheel"), undefined, { timeout: 3_000 });
  assert(true, "real Chromium wheel input enters the user scroll session");

  const midGesture = await page.evaluate(() => {
    const transcript = document.querySelector(".transcript");
    if (!transcript) return null;
    window.__scrollWrites = [];
    transcript.dispatchEvent(new WheelEvent("wheel", { deltaY: -240, bubbles: true, cancelable: true }));
    transcript.scrollTop = Math.max(0, transcript.scrollTop - 240);
    transcript.dispatchEvent(new Event("scroll"));
    const row = [...transcript.querySelectorAll(".transcript__row")].find((candidate) => {
      const rect = candidate.getBoundingClientRect();
      return rect.bottom > transcript.getBoundingClientRect().top;
    });
    if (row instanceof HTMLElement) row.style.paddingTop = "180px";
    return { gesture: transcript.dataset.scrollGesture, top: transcript.scrollTop, rowKey: row?.dataset.rowKey ?? null };
  });
  assert(midGesture?.gesture === "wheel", "variable-height mutation occurs while wheel ownership is active");
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  const during = await page.evaluate(() => ({
    writes: window.__scrollWrites ?? [],
    gesture: document.querySelector(".transcript")?.dataset.scrollGesture,
  }));
  assert(during.writes.every((write) => !["virtualizer", "stream", "container-resize", "footer-resize", "row-size"].includes(write.owner)),
    `compensating owners stay silent during variable-height scroll (${JSON.stringify(during.writes)})`);

  await page.evaluate(() => document.querySelector(".transcript")?.dispatchEvent(new Event("scrollend")));
  await page.waitForFunction(() => !document.querySelector(".transcript")?.dataset.scrollGesture);
  const settledTop = await page.locator(".transcript").evaluate((element) => element.scrollTop);
  await page.waitForTimeout(260);
  const lateTop = await page.locator(".transcript").evaluate((element) => element.scrollTop);
  assert(Math.abs(lateTop - settledTop) <= 1, `scrollend has no delayed full-measure jump (${settledTop} → ${lateTop})`);

  const middle = await page.evaluate(() => {
    const transcript = document.querySelector(".transcript");
    if (!transcript) return null;
    window.__scrollWrites = [];
    transcript.dispatchEvent(new PointerEvent("pointerdown", { button: 1, buttons: 4, pointerId: 9, bubbles: true }));
    const started = transcript.dataset.scrollGesture;
    transcript.scrollTop = Math.max(0, transcript.scrollTop - 160);
    transcript.dispatchEvent(new Event("scroll"));
    return { started, continued: transcript.dataset.scrollGesture, top: transcript.scrollTop };
  });
  assert(middle?.started === "middle-button", "middle-button activation owns Windows auto-scroll");
  assert(middle?.continued === "middle-button", "unowned native scroll refreshes middle-button ownership");
  const middleWrites = await page.evaluate(() => window.__scrollWrites ?? []);
  assert(middleWrites.length === 0, "middle-button auto-scroll admits no compensating programmatic writes");
  await page.evaluate(() => document.querySelector(".transcript")?.dispatchEvent(new Event("scrollend")));
  await page.waitForFunction(() => !document.querySelector(".transcript")?.dataset.scrollGesture);
  assert(true, "middle-button session terminates on native scrollend");

  // Reproduce the subtle tail-follow race: a short upward wheel stays inside
  // the 80px near-bottom band, then the gesture settles before more content
  // arrives. User intent must remain manual across that quiet boundary.
  const jumpBottom = page.locator(".transcript__jump-bottom");
  await jumpBottom.click();
  await page.waitForFunction(() => {
    const transcript = document.querySelector(".transcript");
    return transcript && transcript.scrollHeight - transcript.scrollTop - transcript.clientHeight <= 0.5;
  });
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.wheel(0, -24);
  await page.waitForFunction(() => {
    const transcript = document.querySelector(".transcript");
    if (!transcript) return false;
    const distance = transcript.scrollHeight - transcript.scrollTop - transcript.clientHeight;
    return distance > 0.5 && distance < 80 && transcript.dataset.scrollGesture === "wheel";
  });
  await page.waitForTimeout(64);
  await page.evaluate(() => document.querySelector(".transcript")?.dispatchEvent(new Event("scrollend")));
  await page.waitForFunction(() => !document.querySelector(".transcript")?.dataset.scrollGesture);
  const shortUpward = await page.locator(".transcript").evaluate((element) => ({
    distance: element.scrollHeight - element.scrollTop - element.clientHeight,
    mode: element.dataset.scrollMode,
    top: element.scrollTop,
  }));
  assert(shortUpward.mode === "manual", `short upward wheel remains manual inside the bottom threshold (${JSON.stringify(shortUpward)})`);
  await page.waitForTimeout(260);
  const shortUpwardSettled = await page.locator(".transcript").evaluate((element) => ({
    mode: element.dataset.scrollMode,
    top: element.scrollTop,
  }));
  assert(shortUpwardSettled.mode === "manual" && Math.abs(shortUpwardSettled.top - shortUpward.top) <= 1,
    `quiet-window expiry does not re-pin the short upward gesture (${JSON.stringify(shortUpwardSettled)})`);

  await page.mouse.wheel(0, 120);
  await page.waitForFunction(() => {
    const transcript = document.querySelector(".transcript");
    return transcript
      && transcript.scrollHeight - transcript.scrollTop - transcript.clientHeight <= 0.5
      && transcript.dataset.scrollMode === "tail-follow";
  });
  assert(true, "user wheel back to the physical bottom explicitly restores tail-follow");

  // A harmless downward wheel at the physical bottom still freezes passive
  // writers. If the final row grows during that lock, the repin is deferred and
  // replayed once after idle using the latest scrollHeight.
  const harmlessDown = await page.evaluate(() => {
    const transcript = document.querySelector(".transcript");
    const rows = transcript ? [...transcript.querySelectorAll(".transcript__row")] : [];
    const tailRow = rows.at(-1);
    if (!(transcript instanceof HTMLElement) || !(tailRow instanceof HTMLElement)) return null;
    transcript.scrollTop = transcript.scrollHeight;
    window.__scrollWrites = [];
    transcript.dispatchEvent(new WheelEvent("wheel", { deltaY: 48, bubbles: true, cancelable: true }));
    tailRow.style.paddingBottom = `${Number.parseFloat(tailRow.style.paddingBottom || "0") + 160}px`;
    return { gesture: transcript.dataset.scrollGesture, mode: transcript.dataset.scrollMode };
  });
  assert(harmlessDown?.gesture === "wheel" && harmlessDown.mode === "tail-follow",
    `wheel-down at the physical bottom preserves tail-follow (${JSON.stringify(harmlessDown)})`);
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => requestAnimationFrame(resolve)))));
  const harmlessDuring = await page.evaluate(() => window.__scrollWrites ?? []);
  assert(harmlessDuring.every((write) => !["virtualizer", "stream", "container-resize", "footer-resize", "row-size"].includes(write.owner)),
    `tail growth stays write-free during the downward gesture (${JSON.stringify(harmlessDuring)})`);
  await page.waitForTimeout(64);
  await page.evaluate(() => document.querySelector(".transcript")?.dispatchEvent(new Event("scrollend")));
  await page.waitForFunction(() => {
    const transcript = document.querySelector(".transcript");
    return transcript
      && !transcript.dataset.scrollGesture
      && transcript.dataset.scrollMode === "tail-follow"
      && transcript.scrollHeight - transcript.scrollTop - transcript.clientHeight <= 0.5;
  });
  const harmlessSettled = await page.locator(".transcript").evaluate((element) => ({
    distance: element.scrollHeight - element.scrollTop - element.clientHeight,
    mode: element.dataset.scrollMode,
  }));
  assert(harmlessSettled.distance <= 0.5 && harmlessSettled.mode === "tail-follow",
    `deferred tail growth replays once after gesture idle (${JSON.stringify(harmlessSettled)})`);

  process.stdout.write("\ntranscript scroll stability browser gate passed\n");
} finally {
  await browser?.close();
  preview.kill("SIGTERM");
}
