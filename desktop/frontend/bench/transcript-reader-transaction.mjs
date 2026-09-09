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

  await page.evaluate((anchor) => {
    window.__readerProbe = { active: true, held: true, blankFrames: 0, anchorMaxDrift: 0, heldAccepted: [], writes: [], diagnostics: [] };
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
      const anchoredBlock = [...element.querySelectorAll("[data-transcript-block-key]")]
        .find(block => block.getAttribute("data-transcript-block-key") === anchor.key);
      if (anchoredBlock) probe.anchorMaxDrift = Math.max(probe.anchorMaxDrift,
        Math.abs(anchoredBlock.getBoundingClientRect().top - viewport.top - anchor.top));
      requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
  }, before);
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
      anchorMaxDrift: probe?.anchorMaxDrift ?? Infinity,
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
  // Content growth must reposition subsequent blocks after release. Keeping
  // every old top would preserve the anchor by allowing overlapping content.
  const overlaps = await transcript.evaluate(element => {
    const viewport = element.getBoundingClientRect();
    const blocks = [...element.querySelectorAll("[data-transcript-block-key]")]
      .map(block => block.getBoundingClientRect()).sort((a, b) => a.top - b.top);
    return blocks.slice(0, -1).filter((block, index) => block.bottom > viewport.top
      && block.top < viewport.bottom && block.bottom > blocks[index + 1].top + 1).length;
  });
  assert(overlaps === 0, `${label} ${iteration + 1}/${iterations}: released content growth leaves no overlapping visible blocks`);
  assert(result.anchorMaxDrift <= 4, `${label} ${iteration + 1}/${iterations}: every painted measurement frame preserves the anchor (${result.anchorMaxDrift.toFixed(1)}px)`);
  const drift = result.top == null ? null : Math.abs(result.top - before.top);
  const driftDiagnostic = settlementError || drift == null || drift > 4
    ? `; ${JSON.stringify({ settlementError, before, mutation, held, heldOriginal, result })}`
    : "";
  assert(!settlementError && drift != null && drift <= 4,
    `${label} ${iteration + 1}/${iterations}: logical anchor drift is at most 4px (${drift == null ? "missing" : drift.toFixed(1)}px${driftDiagnostic})`);
  assert(result.intent === "reader", `${label} ${iteration + 1}/${iterations}: reader retains viewport ownership`);
  assert(result.mounted <= 40, `${label} ${iteration + 1}/${iterations}: mounted completed blocks remain bounded (${result.mounted})`);
}

async function runColdExpansion(page, transcript, label) {
  const rail = await page.locator(".jump-scroll").boundingBox();
  if (!rail) throw new Error("question navigator unavailable");
  await page.mouse.click(rail.x + rail.width / 2, rail.y + rail.height * (949.5 / 1000));
  const block = page.locator("[data-transcript-block-key]").filter({ hasText: "windowed turn 950:" });
  await block.locator(".reasoning__head").click();
  const toggle = block.locator(".turn-collapse__reasoning-head");
  // Playwright may scroll an offscreen control before dispatching the click.
  // Establish that input target first; measure expansion from the actual
  // pre-click viewport rather than including actionability setup as drift.
  await toggle.click({ trial: true });
  await waitForNativeViewportSettlement(page);
  const before = await block.evaluate(element => ({ key: element.dataset.transcriptBlockKey,
    top: element.getBoundingClientRect().top, height: element.getBoundingClientRect().height }));
  await toggle.click();
  await page.waitForFunction(({ key, height }) => {
    const blocks = [...document.querySelectorAll("[data-transcript-block-key]")];
    const element = blocks.find(block => block.getAttribute("data-transcript-block-key") === key);
    const rect = element?.getBoundingClientRect();
    const next = blocks[blocks.indexOf(element) + 1]?.getBoundingClientRect();
    return rect && next && rect.height > height + 100 && Math.abs(next.top - rect.bottom) <= 1;
  }, before);
  const expanded = await block.evaluate(element => ({ top: element.getBoundingClientRect().top,
    height: element.getBoundingClientRect().height }));
  assert(Math.abs(expanded.top - before.top) <= 4, `${label}: cold reasoning expansion preserves its reading anchor (${JSON.stringify({ before, expanded })})`);
  assert(expanded.height > before.height + 100, `${label}: real reasoning expansion repositions the next block without overlap`);
  await toggle.click();
  await page.waitForFunction(({ key, height }) => {
    const element = [...document.querySelectorAll("[data-transcript-block-key]")]
      .find(block => block.getAttribute("data-transcript-block-key") === key);
    return element && Math.abs(element.getBoundingClientRect().height - height) <= 1;
  }, before);
  await waitForNativeViewportSettlement(page);
  const gap = await block.evaluate(element => {
    const next = element.nextElementSibling;
    return next ? next.getBoundingClientRect().top - element.getBoundingClientRect().bottom : Infinity;
  });
  assert(Math.abs(gap) <= 1, `${label}: collapsing reasoning also removes the stale measured gap`);
}

async function runNativeMeasurementCommit(browser, label) {
  const page = await browser.newPage({ viewport: { width: 1200, height: 800 } });
  try {
    await page.goto(url, { waitUntil: "domcontentloaded" });
    await page.addScriptTag({ path: path.join(frontendDir, "..", "transcript_native_smoke_contract.js") });
    await page.waitForFunction(() => window.__reasonixNativeTranscriptSmokeState?.phase === "ready",
      undefined, { timeout: 90_000 });
    await page.evaluate(() => {
      const probe = { active: true, frames: 0, maxReverse: 0, maxOverlap: 0, previous: [] };
      window.__measurementCommitProbe = probe;
      const sample = () => {
        if (!probe.active) return;
        const element = document.querySelector(".transcript");
        const viewport = element.getBoundingClientRect();
        const current = [...element.querySelectorAll("[data-transcript-block-key]")]
          .map(block => ({ key: block.dataset.transcriptBlockKey, rect: block.getBoundingClientRect() }))
          .filter(block => block.rect.bottom > viewport.top && block.rect.top < viewport.bottom)
          .sort((a, b) => a.rect.top - b.rect.top);
        const deltas = probe.previous.flatMap(before => {
          const after = current.find(block => block.key === before.key);
          return after ? [after.rect.top - before.rect.top] : [];
        }).sort((a, b) => a - b);
        probe.maxReverse = Math.max(probe.maxReverse, deltas[Math.floor(deltas.length / 2)] ?? 0);
        for (let index = 1; index < current.length; index += 1) {
          probe.maxOverlap = Math.max(probe.maxOverlap, current[index - 1].rect.bottom - current[index].rect.top);
        }
        probe.previous = current;
        probe.frames += 1;
        requestAnimationFrame(() => setTimeout(sample, 0));
      };
      requestAnimationFrame(() => setTimeout(sample, 0));
    });
    const box = await page.locator(".transcript").boundingBox();
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    for (let step = 0; step < 230; step += 1) {
      await page.mouse.wheel(0, 120);
      await frames(page, 1);
    }
    // Include lease release and its geometry commit, not just active scrolling.
    await frames(page, 40);
    const result = await page.evaluate(() => {
      const probe = window.__measurementCommitProbe;
      probe.active = false;
      return { frames: probe.frames, maxReverse: probe.maxReverse, maxOverlap: probe.maxOverlap,
        rows: document.querySelector(".transcript")?.dataset.transcriptRowCount };
    });
    assert(Number(result.rows) >= 400 && result.frames > 200, `${label}: native fixture measures a sustained loaded-history traversal`);
    assert(result.maxReverse <= 4, `${label}: sustained traversal and release retain the native reverse-displacement gate (${result.maxReverse.toFixed(1)}px)`);
    assert(result.maxOverlap <= 1, `${label}: measured blocks never overlap in a painted traversal frame (${result.maxOverlap.toFixed(1)}px)`);
  } finally { await page.close(); }
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
    await runColdExpansion(page, transcript, label);
    await runSustainedWheelTraversal(page, transcript, label);
    await jumpToTail(page);
    const final = await anchorSnapshot(page);
    assert(final.visible > 0 && final.distance <= 4, `${label}: final viewport is visibly covered at the native tail`);
    assert(errors.length === 0, `${label}: replay emits no page errors (${errors.length})`);
    await runNativeMeasurementCommit(browser, label);
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
