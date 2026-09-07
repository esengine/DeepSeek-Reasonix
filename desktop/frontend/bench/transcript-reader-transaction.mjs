#!/usr/bin/env node

import http from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { startPreviewServer } from "./vite-preview-server.mjs";

const frontendDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
process.env.PLAYWRIGHT_BROWSERS_PATH = !process.env.PLAYWRIGHT_BROWSERS_PATH || process.env.PLAYWRIGHT_BROWSERS_PATH === ".pw-browsers"
  ? path.join(frontendDir, ".pw-browsers")
  : process.env.PLAYWRIGHT_BROWSERS_PATH;
const { chromium, webkit } = await import("playwright");
const port = Number(process.env.REASONIX_TRANSCRIPT_READER_PORT ?? 4621);
const iterations = Number(process.env.REASONIX_TRANSCRIPT_READER_ITERATIONS ?? 6);
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

async function waitForNativeViewportSettlement(page, requiredStableFrames = 6) {
  await page.evaluate((stableFrameTarget) => new Promise((resolve, reject) => {
    let previous = null;
    let stableFrames = 0;
    let sampledFrames = 0;
    const sample = () => {
      const transcript = document.querySelector(".transcript");
      if (!(transcript instanceof HTMLElement)) {
        reject(new Error("transcript viewport disappeared while native scrolling settled"));
        return;
      }
      const current = [transcript.scrollTop, transcript.scrollHeight, transcript.clientHeight];
      const unchanged = previous != null
        && current.every((value, index) => Math.abs(value - previous[index]) <= 0.5);
      stableFrames = unchanged ? stableFrames + 1 : 0;
      sampledFrames += 1;
      previous = current;
      if (stableFrames >= stableFrameTarget) {
        resolve();
        return;
      }
      if (sampledFrames >= 240) {
        reject(new Error(`native viewport did not settle: ${JSON.stringify(current)}`));
        return;
      }
      requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
  }), requiredStableFrames);
}

async function loadLongFixture(page) {
  await page.click('.project-tree__topic-main:has-text("bench:windowed-1000t")');
  await page.waitForFunction(() => {
    const element = document.querySelector(".transcript");
    return document.querySelector(".project-tree__topic--active .project-tree__topic-label")?.textContent?.includes("bench:windowed-1000t")
      && element instanceof HTMLElement
      && element.dataset.transcriptHydrating === "false"
      && element.textContent?.includes("Windowed turn 1000")
      && element.querySelector(".transcript__projection")?.getAttribute("data-transcript-render-mode") === "windowed";
  }, undefined, { timeout: 30_000 });
  await frames(page, 8);
  return page.locator(".transcript");
}

async function anchorSnapshot(page) {
  return page.evaluate(() => {
    const element = document.querySelector(".transcript");
    if (!(element instanceof HTMLElement)) return null;
    const viewport = element.getBoundingClientRect();
    const blocks = [...element.querySelectorAll("[data-transcript-block-key]")];
    const visible = blocks.filter((block) => {
      const rect = block.getBoundingClientRect();
      return rect.height > 0 && rect.bottom > viewport.top && rect.top < viewport.bottom;
    });
    const first = visible[0];
    const projection = element.querySelector(".transcript__projection");
    const item = first?.closest(".transcript__window-item");
    return {
      key: first?.getAttribute("data-transcript-block-key"),
      top: first ? first.getBoundingClientRect().top - viewport.top : null,
      index: item?.getAttribute("data-index"),
      itemTop: item instanceof HTMLElement ? item.style.top : null,
      scrollTop: element.scrollTop,
      visible: visible.length,
      visibleBlocks: visible.map((block) => ({
        key: block.getAttribute("data-transcript-block-key"),
        top: block.getBoundingClientRect().top - viewport.top,
      })),
      intent: element.dataset.transcriptIntent,
      distance: element.scrollHeight - element.scrollTop - element.clientHeight,
      mounted: Number(projection?.getAttribute("data-transcript-mounted-blocks")),
      rangeSource: projection?.getAttribute("data-transcript-range-source"),
    };
  });
}

async function jumpToTail(page) {
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
}

async function runSustainedWheelTraversal(page, transcript, label) {
  const box = await transcript.boundingBox();
  if (!box) throw new Error(`${label}: transcript viewport unavailable for sustained traversal`);
  await transcript.evaluate((element) => {
    element.scrollTop = 0;
    element.dispatchEvent(new Event("scroll"));
  });
  await frames(page, 4);
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.evaluate(() => {
    window.__readerProbe = { active: true, blankFrames: 0, maxMounted: 0 };
    const sample = () => {
      const probe = window.__readerProbe;
      const element = document.querySelector(".transcript");
      if (!probe?.active || !(element instanceof HTMLElement)) return;
      const viewport = element.getBoundingClientRect();
      const occupied = [...element.querySelectorAll("[data-transcript-block-key]")].some((block) => {
        const rect = block.getBoundingClientRect();
        return rect.height > 0 && rect.bottom > viewport.top && rect.top < viewport.bottom;
      });
      if (!occupied) probe.blankFrames += 1;
      probe.maxMounted = Math.max(probe.maxMounted, Number(
        element.querySelector(".transcript__projection")?.getAttribute("data-transcript-mounted-blocks") ?? "0",
      ));
      requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
  });
  for (let step = 0; step < 80; step += 1) {
    const atTail = await transcript.evaluate((element) => element.scrollHeight - element.scrollTop - element.clientHeight <= 4);
    if (atTail) break;
    // Match the largest coalesced WKWebView delta observed by the native gate.
    await page.mouse.wheel(0, 2_880);
    await frames(page, 1);
  }
  const result = await transcript.evaluate((element) => {
    const probe = window.__readerProbe;
    if (probe) probe.active = false;
    window.__readerProbe = undefined;
    return {
      blankFrames: probe?.blankFrames ?? -1,
      maxMounted: probe?.maxMounted ?? Number.POSITIVE_INFINITY,
      scrollTop: element.scrollTop,
    };
  });
  assert(result.scrollTop > 10_000, `${label}: coalesced native-size wheel steps traverse deep history`);
  assert(result.blankFrames === 0, `${label}: sustained forward wheel traversal produces zero blank frames`);
  assert(result.maxMounted <= 40, `${label}: sustained traversal keeps the completed-block mount cap (${result.maxMounted})`);
}

async function runIteration(page, transcript, label, iteration) {
  // Each measurement transaction owns an independent reader baseline. Without
  // this reset, repeated upward wheel input eventually crosses the history
  // boundary and a valid prepend transaction replaces the anchor under test.
  await jumpToTail(page);
  const box = await transcript.boundingBox();
  if (!box) throw new Error(`${label}: transcript viewport unavailable`);
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.wheel(0, -(520 + iteration * 13));
  await page.waitForFunction(() => document.querySelector(".transcript")?.getAttribute("data-transcript-intent") === "reader",
    undefined, { timeout: 5_000 });
  // WebKit may deliver one native wheel delta across several compositor
  // frames. Establish the transaction baseline only after native geometry is
  // stable; otherwise the remainder of the user's own wheel movement is
  // indistinguishable from a product-induced anchor drift.
  await waitForNativeViewportSettlement(page);
  const before = await anchorSnapshot(page);
  assert(before?.key && before.visible > 0, `${label} ${iteration + 1}/${iterations}: reader has a visible logical anchor`);

  await page.evaluate(() => {
    window.__readerProbe = { active: true, held: true, blankFrames: 0, heldAccepted: [], writes: [], diagnostics: [] };
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => {
      window.__readerProbe?.writes.push(write);
      if (window.__readerProbe?.held && write.outcome === "accepted") window.__readerProbe.heldAccepted.push(write);
    };
    window.__REASONIX_TRANSCRIPT_SCROLL_DIAGNOSTIC__ = (type, fields) => {
      const diagnostics = window.__readerProbe?.diagnostics;
      if (!diagnostics) return;
      diagnostics.push({ type, fields });
      if (diagnostics.length > 40) diagnostics.shift();
    };
    window.addEventListener("pointerup", () => {
      if (window.__readerProbe) window.__readerProbe.held = false;
    }, { capture: true, once: true });
    const sample = () => {
      const probe = window.__readerProbe;
      const element = document.querySelector(".transcript");
      if (!probe?.active || !(element instanceof HTMLElement)) return;
      const viewport = element.getBoundingClientRect();
      const occupied = [...element.querySelectorAll("[data-transcript-block-key]")].some((block) => {
        const rect = block.getBoundingClientRect();
        return rect.height > 0 && rect.bottom > viewport.top && rect.top < viewport.bottom;
      });
      if (!occupied) probe.blankFrames += 1;
      requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
  });
  await page.mouse.down();
  const mutation = await transcript.evaluate((element, iterationIndex) => {
    const viewport = element.getBoundingClientRect();
    const items = [...element.querySelectorAll(".transcript__window-item")];
    const firstVisibleIndex = items.findIndex((item) => item.getBoundingClientRect().bottom > viewport.top);
    const earlier = firstVisibleIndex > 0 ? items[firstVisibleIndex - 1] : items[firstVisibleIndex];
    const postViewport = items.find((item) => item.getBoundingClientRect().top >= viewport.bottom);
    const earlierBlock = earlier;
    const visibleBlock = items[firstVisibleIndex];
    const postViewportBlock = postViewport;
    if (!(earlierBlock instanceof HTMLElement)
      || !(visibleBlock instanceof HTMLElement)
      || !(postViewportBlock instanceof HTMLElement)) return null;
    earlierBlock.style.paddingBottom = `${120 + iterationIndex * 3}px`;
    visibleBlock.style.paddingBottom = "24px";
    postViewportBlock.style.paddingBottom = "32px";
    return {
      earlierKey: earlierBlock.dataset.transcriptBlockKey,
      earlierIndex: earlier?.dataset.index,
      visibleKey: visibleBlock.dataset.transcriptBlockKey,
      visibleIndex: items[firstVisibleIndex]?.dataset.index,
      postViewportKey: postViewportBlock.dataset.transcriptBlockKey,
      postViewportIndex: postViewport?.dataset.index,
    };
  }, iteration);
  await frames(page, 6);
  const held = await anchorSnapshot(page);
  const heldOriginal = await page.evaluate((key) => {
    const element = document.querySelector(".transcript");
    const block = [...document.querySelectorAll("[data-transcript-block-key]")]
      .find((candidate) => candidate.getAttribute("data-transcript-block-key") === key);
    const item = block?.closest(".transcript__window-item");
    return element instanceof HTMLElement && block instanceof HTMLElement
      ? {
          top: block.getBoundingClientRect().top - element.getBoundingClientRect().top,
          index: item?.getAttribute("data-index"),
          itemTop: item instanceof HTMLElement ? item.style.top : null,
          height: item instanceof HTMLElement ? item.getBoundingClientRect().height : null,
        }
      : null;
  }, before.key);
  await page.mouse.up();
  let settlementError;
  try {
    await page.waitForFunction(({ key, top }) => {
      const element = document.querySelector(".transcript");
      const block = [...document.querySelectorAll("[data-transcript-block-key]")]
        .find((candidate) => candidate.getAttribute("data-transcript-block-key") === key);
      return element instanceof HTMLElement && block instanceof HTMLElement
        && Math.abs(block.getBoundingClientRect().top - element.getBoundingClientRect().top - top) <= 4;
    }, { key: before.key, top: before.top }, { timeout: 5_000 });
  } catch (error) {
    settlementError = String(error?.message ?? error);
  }
  await frames(page, 2);

  const result = await page.evaluate((key) => {
    const probe = window.__readerProbe;
    if (probe) probe.active = false;
    const element = document.querySelector(".transcript");
    const block = [...document.querySelectorAll("[data-transcript-block-key]")]
      .find((candidate) => candidate.getAttribute("data-transcript-block-key") === key);
    const top = element instanceof HTMLElement && block instanceof HTMLElement
      ? block.getBoundingClientRect().top - element.getBoundingClientRect().top
      : null;
    const projection = element?.querySelector(".transcript__projection");
    const viewport = element?.getBoundingClientRect();
    window.__readerProbe = undefined;
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = undefined;
    window.__REASONIX_TRANSCRIPT_SCROLL_DIAGNOSTIC__ = undefined;
    return {
      top,
      scrollTop: element instanceof HTMLElement ? element.scrollTop : null,
      intent: element?.getAttribute("data-transcript-intent"),
      mounted: Number(projection?.getAttribute("data-transcript-mounted-blocks")),
      blankFrames: probe?.blankFrames ?? -1,
      heldAccepted: probe?.heldAccepted ?? [],
      diagnostics: probe?.diagnostics ?? [],
      visibleBlocks: element instanceof HTMLElement && viewport
        ? [...element.querySelectorAll("[data-transcript-block-key]")]
            .filter((candidate) => {
              const rect = candidate.getBoundingClientRect();
              return rect.bottom > viewport.top && rect.top < viewport.bottom;
            })
            .map((candidate) => ({
              key: candidate.getAttribute("data-transcript-block-key"),
              top: candidate.getBoundingClientRect().top - viewport.top,
            }))
        : [],
    };
  }, before.key);
  assert(result.heldAccepted.length === 0,
    `${label} ${iteration + 1}/${iterations}: user-held transaction accepts zero programmatic writes`);
  assert(result.blankFrames === 0, `${label} ${iteration + 1}/${iterations}: geometry churn produces zero blank frames`);
  const heldDrift = heldOriginal?.top == null ? null : Math.abs(heldOriginal.top - before.top);
  const heldDiagnostic = heldDrift == null || heldDrift > 4
    ? `; ${JSON.stringify({ before, mutation, held, heldOriginal })}`
    : "";
  assert(heldDrift != null && heldDrift <= 4,
    `${label} ${iteration + 1}/${iterations}: staged prefix geometry cannot move the held reader anchor${heldDiagnostic}`);
  const finalTops = new Map(result.visibleBlocks.map((block) => [block.key, block.top]));
  const visibleDrift = Math.max(0, ...before.visibleBlocks
    .filter((block) => finalTops.has(block.key))
    .map((block) => Math.abs(finalTops.get(block.key) - block.top)));
  assert(visibleDrift <= 4,
    `${label} ${iteration + 1}/${iterations}: staged measurements cannot reflow the painted viewport (${visibleDrift.toFixed(1)}px)`);
  const drift = result.top == null ? null : Math.abs(result.top - before.top);
  const driftDiagnostic = settlementError || drift == null || drift > 4
    ? `; ${JSON.stringify({ settlementError, before, mutation, held, heldOriginal, result })}`
    : "";
  assert(!settlementError && drift != null && drift <= 4,
    `${label} ${iteration + 1}/${iterations}: logical anchor drift is at most 4px (${drift == null ? "missing" : drift.toFixed(1)}px${driftDiagnostic})`);
  assert(result.intent === "reader", `${label} ${iteration + 1}/${iterations}: reader retains viewport ownership`);
  assert(result.mounted <= 40, `${label} ${iteration + 1}/${iterations}: mounted completed blocks remain bounded (${result.mounted})`);
}

async function runBrowser(browserType, label) {
  const browser = await browserType.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.goto(url, { waitUntil: "domcontentloaded" });
    await page.waitForFunction(() => !document.querySelector(".startup-splash"), undefined, { timeout: 30_000 });
    const transcript = await loadLongFixture(page);
    for (let iteration = 0; iteration < iterations; iteration += 1) {
      await runIteration(page, transcript, label, iteration);
    }
    await runSustainedWheelTraversal(page, transcript, label);
    await jumpToTail(page);
    const final = await anchorSnapshot(page);
    assert(final.visible > 0 && final.distance <= 4, `${label}: final viewport is visibly covered at the native tail`);
    assert(errors.length === 0, `${label}: replay emits no page errors (${errors.length})`);
  } finally {
    await browser.close();
  }
}

const preview = await startPreviewServer(frontendDir, port);
try {
  await waitForServer();
  await runBrowser(chromium, "Chromium");
  await runBrowser(webkit, "WebKit");
  process.stdout.write("transcript reader transaction browser replay passed\n");
} finally {
  await preview.close();
}
