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
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error("transcript browser preview did not become ready");
}

async function settleFrames(page, count = 6) {
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
  const activeLabel = await page.locator(".project-tree__topic--active .project-tree__topic-label").textContent().catch(() => "");
  if (!activeLabel?.includes(label)) await page.click(`.project-tree__topic-main:has-text("${label}")`);
  await page.waitForFunction(({ label, marker }) => {
    const active = document.querySelector(".project-tree__topic--active .project-tree__topic-label")?.textContent ?? "";
    const transcript = document.querySelector(".transcript");
    return active.includes(label)
      && transcript instanceof HTMLElement
      && transcript.dataset.transcriptHydrating === "false"
      && transcript.textContent?.includes(marker)
      && !document.querySelector(".transcript-navigation-overlay");
  }, { label, marker }, { timeout: 30_000 });
  await settleFrames(page);
  return page.locator(".transcript");
}

async function snapshot(page) {
  return page.evaluate(() => {
    const element = document.querySelector(".transcript");
    if (!(element instanceof HTMLElement)) return null;
    const viewport = element.getBoundingClientRect();
    const blocks = [...element.querySelectorAll("[data-transcript-block-key]")];
    const visible = blocks
      .filter((block) => {
        const rect = block.getBoundingClientRect();
        return rect.height > 0 && rect.bottom > viewport.top && rect.top < viewport.bottom;
      })
      .map((block) => ({
        key: block.dataset.transcriptBlockKey,
        top: block.getBoundingClientRect().top - viewport.top,
      }));
    const projection = element.querySelector(".transcript__projection");
    return {
      generation: Number(element.dataset.transcriptGeneration),
      intent: element.dataset.transcriptIntent,
      top: element.scrollTop,
      height: element.scrollHeight,
      clientHeight: element.clientHeight,
      distance: element.scrollHeight - element.scrollTop - element.clientHeight,
      rows: Number(element.dataset.transcriptRowCount),
      blocks: Number(element.dataset.transcriptBlockCount),
      mode: projection?.getAttribute("data-transcript-render-mode"),
      completed: Number(projection?.getAttribute("data-transcript-completed-blocks")),
      mounted: Number(projection?.getAttribute("data-transcript-mounted-blocks")),
      domBlocks: blocks.length,
      uniqueBlocks: new Set(blocks.map((block) => block.dataset.transcriptBlockKey)).size,
      visible,
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
  await settleFrames(page);
}

async function runGeometryFixture(page) {
  const transcript = await loadFixture(page, "bench:geometry-blocks", "Geometry contract fixture complete.");
  const state = await snapshot(page);
  assert(state?.blocks === 1, "heterogeneous geometry fixture projects one semantic turn block");
  assert(state.rows > state.blocks, `one turn may contain many presentation rows (${state.rows} rows)`);
  assert(state.domBlocks === 1 && state.uniqueBlocks === 1, "row density does not split or duplicate the stable block identity");
  assert(state.visible.length > 0, "the semantic block covers the native viewport");
  assert(await transcript.locator("[data-transcript-block-phase=completed]").count() === 1, "completed fixture has one completed block phase");
}

async function runWindowedFixture(page) {
  const transcript = await loadFixture(page, "bench:windowed-1000t", "Windowed turn 1000");
  await jumpToTail(page);
  let state = await snapshot(page);
  assert(state.completed > 100, `long fixture crosses the 100-turn boundary (${state.completed} completed blocks)`);
  assert(state.mode === "windowed", "long fixture uses the TanStack window adapter");
  assert(state.mounted <= 40 && state.domBlocks <= 40, `800px window mounts at most 40 completed blocks (${state.domBlocks})`);
  assert(state.uniqueBlocks === state.domBlocks, "every mounted block key is unique");
  assert(state.visible.length > 0, "tail paint has visible block coverage");
  assert(await transcript.locator("[data-transcript-resident-tail=true] > [data-transcript-block-key]").count() >= 2,
    "the newest two completed blocks stay in the resident tail");

  const box = await transcript.boundingBox();
  if (!box) throw new Error("windowed transcript has no viewport box");
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  for (let step = 0; step < 12; step += 1) {
    await page.mouse.wheel(0, -480);
    await settleFrames(page, 2);
    state = await snapshot(page);
    assert(state.visible.length > 0, `upward traversal frame ${step + 1} retains mounted viewport coverage`);
    assert(state.domBlocks <= 40, `upward traversal frame ${step + 1} stays under the 40-block mount cap (${state.domBlocks})`);
  }
  assert(state.intent === "reader", "native wheel transfers the viewport to reader intent");

  await page.evaluate(() => {
    window.__readerGestureWrites = [];
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => window.__readerGestureWrites.push(write);
  });
  await page.mouse.down();
  await transcript.evaluate((element) => {
    const first = [...element.querySelectorAll("[data-transcript-block-key]")].find((block) => {
      const rect = block.getBoundingClientRect();
      const viewport = element.getBoundingClientRect();
      return rect.bottom > viewport.top && rect.top < viewport.bottom;
    });
    if (first instanceof HTMLElement) first.style.paddingBottom = "240px";
  });
  await settleFrames(page, 8);
  const heldWrites = await page.evaluate(() => window.__readerGestureWrites ?? []);
  assert(heldWrites.filter((write) => write.outcome === "accepted").length === 0,
    "held pointer gesture admits zero accepted programmatic writes");
  await page.mouse.up();
  await settleFrames(page, 4);
  await page.evaluate(() => {
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = undefined;
    window.__readerGestureWrites = undefined;
  });

  for (let attempt = 0; attempt < 6; attempt += 1) {
    await page.mouse.wheel(0, 360);
    await settleFrames(page, 2);
  }
  // End the input-ownership window explicitly before injecting a layout
  // mutation. The assertion below then tests reader-anchor correction rather
  // than racing the wheel quiet-period policy.
  await page.mouse.down();
  await page.mouse.up();
  await settleFrames(page, 2);
  const anchorBefore = await page.evaluate(() => {
    const element = document.querySelector(".transcript");
    if (!(element instanceof HTMLElement)) return null;
    const viewport = element.getBoundingClientRect();
    const mounted = [...element.querySelectorAll(".transcript__window-item")];
    const visible = mounted.find((item) => item.getBoundingClientRect().bottom > viewport.top + 1);
    if (!(visible instanceof HTMLElement)) return null;
    const block = visible;
    const prior = mounted.filter((item) => item.getBoundingClientRect().bottom <= visible.getBoundingClientRect().top + 1).at(-1);
    return {
      key: block?.getAttribute("data-transcript-block-key"),
      top: visible.getBoundingClientRect().top - viewport.top,
      priorKey: prior?.getAttribute("data-transcript-block-key"),
    };
  });
  assert(anchorBefore?.key, "reader fixture exposes a stable logical anchor block");
  if (anchorBefore.priorKey) {
    await page.evaluate((priorKey) => {
      const element = [...document.querySelectorAll("[data-transcript-block-key]")]
        .find((block) => block.getAttribute("data-transcript-block-key") === priorKey);
      if (element instanceof HTMLElement) element.style.paddingBottom = `${element.getBoundingClientRect().height + 180}px`;
    }, anchorBefore.priorKey);
    await settleFrames(page, 10);
    const anchorAfter = await page.evaluate((key) => {
      const transcript = document.querySelector(".transcript");
      const block = [...document.querySelectorAll("[data-transcript-block-key]")]
        .find((candidate) => candidate.getAttribute("data-transcript-block-key") === key);
      if (!(transcript instanceof HTMLElement) || !(block instanceof HTMLElement)) return null;
      return block.getBoundingClientRect().top - transcript.getBoundingClientRect().top;
    }, anchorBefore.key);
    assert(anchorAfter != null && Math.abs(anchorAfter - anchorBefore.top) <= 4,
      `reader anchor drift stays within 4px after an earlier block remeasure (${anchorAfter == null ? "missing" : Math.abs(anchorAfter - anchorBefore.top).toFixed(1)}px)`);
  } else {
    assert(true, "reader anchor is the first mounted overscan block; no earlier measured block can perturb it");
  }

  const currentTop = await transcript.evaluate((element) => element.scrollTop);
  await page.mouse.wheel(0, 160 - currentTop);
  await settleFrames(page, 4);
  const prependAnchor = await transcript.evaluate((element) => {
    const viewport = element.getBoundingClientRect();
    const visible = [...element.querySelectorAll("[data-transcript-block-key]")].find((block) => {
      const rect = block.getBoundingClientRect();
      return rect.height > 0 && rect.bottom > viewport.top && rect.top < viewport.bottom;
    });
    return visible ? {
      key: visible.getAttribute("data-transcript-block-key"),
      top: visible.getBoundingClientRect().top - viewport.top + element.scrollTop,
    } : null;
  });
  assert(prependAnchor?.key, "history prepend captures the old first-page block before the boundary request");
  const beforePrepend = await snapshot(page);
  await page.evaluate((anchor) => {
    window.__prependWrites = [];
    window.__prependProbe = { anchor, blankFrames: 0, active: true };
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => {
      window.__prependWrites.push(write);
    };
    const sample = () => {
      const probe = window.__prependProbe;
      const element = document.querySelector(".transcript");
      if (!probe?.active || !(element instanceof HTMLElement)) return;
      const viewport = element.getBoundingClientRect();
      const visible = [...element.querySelectorAll("[data-transcript-block-key]")].filter((block) => {
        const rect = block.getBoundingClientRect();
        return rect.height > 0 && rect.bottom > viewport.top && rect.top < viewport.bottom;
      });
      if (visible.length === 0) probe.blankFrames += 1;
      requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
  }, prependAnchor);
  for (let attempt = 0; attempt < 8; attempt += 1) {
    await page.mouse.wheel(0, -100_000);
    await settleFrames(page, 2);
    const current = await snapshot(page);
    if (current.blocks > beforePrepend.blocks) break;
  }
  await page.waitForFunction((count) => Number(document.querySelector(".transcript")?.getAttribute("data-transcript-block-count")) > count,
    beforePrepend.blocks, { timeout: 15_000 });
  await page.waitForFunction((anchor) => {
    const transcript = document.querySelector(".transcript");
    const block = [...document.querySelectorAll("[data-transcript-block-key]")]
      .find((candidate) => candidate.getAttribute("data-transcript-block-key") === anchor.key);
    return transcript instanceof HTMLElement && block instanceof HTMLElement
      && Math.abs(block.getBoundingClientRect().top - transcript.getBoundingClientRect().top - anchor.top) <= 4;
  }, prependAnchor, { timeout: 15_000 });
  await settleFrames(page, 2);
  const prepend = await page.evaluate(() => {
    const probe = window.__prependProbe;
    if (probe) probe.active = false;
    const transcript = document.querySelector(".transcript");
    const block = probe?.anchor?.key
      ? [...document.querySelectorAll("[data-transcript-block-key]")].find((candidate) => candidate.getAttribute("data-transcript-block-key") === probe.anchor.key)
      : null;
    const top = transcript instanceof HTMLElement && block instanceof HTMLElement
      ? block.getBoundingClientRect().top - transcript.getBoundingClientRect().top
      : null;
    const writes = window.__prependWrites ?? [];
    const mountedKeys = [...document.querySelectorAll("[data-transcript-block-key]")]
      .map((candidate) => candidate.getAttribute("data-transcript-block-key"));
    const protectedCount = document.querySelector(".transcript-shell")?.getAttribute("data-protected-blocks");
    window.__prependProbe = undefined;
    window.__prependWrites = undefined;
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = undefined;
    return { ...probe, top, writes, mountedKeys, protectedCount };
  });
  if (!prepend.anchor?.key || prepend.top == null) {
    throw new Error(`history prepend lost its captured block: ${JSON.stringify(prepend)}`);
  }
  assert(true, "history prepend retains the captured block identity");
  assert(Math.abs(prepend.top - prepend.anchor.top) <= 4,
    `history prepend preserves the block-local offset within 4px (${Math.abs(prepend.top - prepend.anchor.top).toFixed(1)}px)`);
  assert(prepend.blankFrames === 0, "history prepend produces zero blank viewport frames");
  const prependCorrections = prepend.writes.filter((write) => write.outcome === "accepted"
    && (write.owner === "history-prepend" || write.owner === "restore"));
  assert(prependCorrections.length <= 2,
    `history prepend uses at most one geometry retry (${JSON.stringify(prependCorrections.map(({ owner, transaction, geometryRevision, requestedOffset, acceptedOffset }) => ({ owner, transaction, geometryRevision, requestedOffset, acceptedOffset })))})`);

  await page.evaluate(() => {
    window.__questionWrites = [];
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => window.__questionWrites.push(write);
    const markers = [...document.querySelectorAll('.jump-item[data-loaded="true"]')];
    const marker = markers.find((candidate) => candidate.getAttribute("data-turn") !== markers.at(-1)?.getAttribute("data-turn"));
    const rail = document.querySelector(".jump-scroll");
    if (!(marker instanceof HTMLElement) || !(rail instanceof HTMLElement)) throw new Error("loaded question marker missing");
    const turn = Number(marker.dataset.turn);
    const total = Number(rail.getAttribute("aria-valuemax"));
    const rect = rail.getBoundingClientRect();
    rail.dispatchEvent(new MouseEvent("mousedown", {
      bubbles: true,
      cancelable: true,
      button: 0,
      clientX: rect.left + rect.width / 2,
      clientY: rect.top + ((turn + 0.5) / total) * rect.height,
    }));
  });
  await page.waitForFunction(() => (window.__questionWrites ?? []).some((write) => write.owner === "question-jump" && write.outcome === "accepted"),
    undefined, { timeout: 15_000 });
  await settleFrames(page, 4);
  const questionWrites = await page.evaluate(() => {
    const writes = (window.__questionWrites ?? []).filter((write) => write.owner === "question-jump" && write.outcome === "accepted");
    window.__questionWrites = undefined;
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = undefined;
    return writes;
  });
  assert(questionWrites.length === 1, `question jump performs exactly one accepted physical write (${questionWrites.length})`);

  await jumpToTail(page);
  await page.evaluate(() => {
    window.__tailGrowthWrites = [];
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => window.__tailGrowthWrites.push(write);
    const resident = document.querySelector("[data-transcript-resident-tail=true]");
    if (resident instanceof HTMLElement) {
      const growth = document.createElement("div");
      growth.style.height = "320px";
      growth.setAttribute("data-tail-growth-fixture", "true");
      resident.append(growth);
    }
  });
  await settleFrames(page, 12);
  const tailGrowth = await page.evaluate(() => {
    const element = document.querySelector(".transcript");
    const writes = window.__tailGrowthWrites ?? [];
    window.__tailGrowthWrites = undefined;
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = undefined;
    return {
      distance: element.scrollHeight - element.scrollTop - element.clientHeight,
      writes: writes.filter((write) => write.owner === "tail-follow" && write.outcome === "accepted").length,
    };
  });
  assert(tailGrowth.distance <= 4, `resident-tail growth settles within 4px of the native bottom (${tailGrowth.distance}px)`);
  assert(tailGrowth.writes <= 2, `tail growth coalesces physical writes (${tailGrowth.writes})`);
}

async function runSessionReplacement(page) {
  const before = await snapshot(page);
  const oldKeys = await page.locator(".transcript [data-transcript-block-key]").evaluateAll((blocks) => blocks.map((block) => block.getAttribute("data-transcript-block-key")));
  await loadFixture(page, "bench:small-6t", "ASYNC LAYOUT EXPANSION COMPLETE");
  const small = await snapshot(page);
  assert(small.generation > before.generation, "session replacement advances the surface generation");
  assert(small.visible.length > 0, "replacement surface has visible content on its first committed paint");
  const ghosts = await page.locator("[data-transcript-block-key]").evaluateAll((blocks, keys) => blocks.filter((block) => keys.includes(block.getAttribute("data-transcript-block-key"))).length, oldKeys);
  assert(ghosts === 0, "replacement surface contains zero ghost blocks from the old session");
  await loadFixture(page, "bench:windowed-1000t", "Windowed turn 1000");
  const restored = await snapshot(page);
  assert(restored.generation > small.generation, "returning to the long session creates a fresh generation");
  assert(restored.visible.length > 0 && restored.domBlocks <= 40, "restored windowed surface is covered and bounded on first paint");
}

async function runSafetyFixture(page) {
  const cdp = await page.context().newCDPSession(page);
  const heap = async () => {
    await cdp.send("HeapProfiler.collectGarbage");
    return (await cdp.send("Runtime.getHeapUsage")).usedSize;
  };
  const samples = [];
  const baseline = [];
  for (let cycle = 0; cycle < 12; cycle += 1) {
    await loadFixture(page, "bench:windowed-1000t", "Windowed turn 1000");
    await jumpToTail(page);
    await loadFixture(page, "bench:small-6t", "ASYNC LAYOUT EXPANSION COMPLETE");
    baseline.push({ cycle, releasedHeap: await heap(), releasedDom: await cdp.send("Memory.getDOMCounters") });
  }
  process.stdout.write(`  METRIC session-switch-baseline ${JSON.stringify(baseline)}\n`);
  for (let cycle = 0; cycle < 24; cycle += 1) {
    const scenario = cycle % 3;
    const windowStart = performance.now();
    await loadFixture(page, "bench:windowed-1000t", "Windowed turn 1000");
    await jumpToTail(page);
    const windowInteractiveMs = performance.now() - windowStart;
    const windowed = await snapshot(page);
    const windowHeap = await heap();
    if (scenario) {
      await page.locator(".transcript").hover();
      await page.mouse.wheel(0, -600);
      await settleFrames(page, 4);
      if (scenario === 1) await page.mouse.down();
    }
    if (scenario === 2) {
      const point = await page.evaluate(() => {
        const element = document.querySelector(".transcript"), viewport = element.getBoundingClientRect();
        const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
        for (let node = walker.nextNode(); node; node = walker.nextNode()) {
          if (node.textContent.trim().length < 8) continue;
          const range = document.createRange(); range.setStart(node, 0); range.setEnd(node, 8);
          const rect = range.getBoundingClientRect();
          if (rect.width > 20 && rect.top > viewport.top + 4 && rect.bottom < viewport.bottom - 4) return { x: rect.left + 1, y: rect.top + rect.height / 2, end: rect.right - 1 };
        }
        throw new Error("selection fixture has no visible text range");
      });
      await page.mouse.move(point.x, point.y); await page.mouse.down();
      await page.mouse.move(point.end, point.y, { steps: 4 });
      await settleFrames(page, 2);
    }
    const started = await page.evaluate((cycle) => {
      const element = document.querySelector(".transcript");
      const viewport = element.getBoundingClientRect();
      const block = [...element.querySelectorAll("[data-transcript-block-key]")].find((node) => node.getBoundingClientRect().bottom > viewport.top);
      const action = [...block.querySelectorAll("button:not(:disabled)")].find((button) => button.getBoundingClientRect().height > 0);
      if (cycle !== 2) action?.focus({ preventScroll: true });
      window.__safetyProbe = { block, action, focusHost: document.activeElement, top: block.getBoundingClientRect().top, text: document.getSelection()?.toString(), writes: [] };
      window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => window.__safetyProbe.writes.push(write);
      const start = performance.now();
      Object.defineProperty(element, "scrollHeight", { configurable: true, get: () => NaN });
      element.dispatchEvent(new Event("scroll"));
      return start;
    }, scenario);
    await page.waitForFunction(() => {
      const projection = document.querySelector(".transcript__projection");
      return projection?.dataset.transcriptRenderMode === "full"
        && projection.dataset.transcriptMountedBlocks === projection.dataset.transcriptCompletedBlocks;
    });
    const interactiveMs = await page.evaluate((start) => performance.now() - start, started);
    await settleFrames(page, 8);
    await page.evaluate(() => { const element = document.querySelector(".transcript"); delete element.scrollHeight; element.dispatchEvent(new Event("scroll")); });
    await settleFrames(page, 8);
    const full = await snapshot(page);
    const retained = await page.evaluate(() => {
      const probe = window.__safetyProbe;
      return { identity: probe.block.isConnected, focus: Boolean(probe.action) && document.activeElement === probe.focusHost,
        hasSelection: Boolean(probe.text), selection: document.getSelection()?.toString() === probe.text,
        drift: Math.abs(probe.block.getBoundingClientRect().top - probe.top),
        writes: probe.writes.filter((write) => write.outcome === "accepted").length };
    });
    assert(full.mode === "full" && full.visible.length > 0 && full.domBlocks === full.completed, "fault recovery remains full, mounted, and visibly covered until generation replacement");
    assert(interactiveMs <= 1000 && retained.focus, `paged full presentation is focusable within 1s (${interactiveMs.toFixed(1)}ms; ${JSON.stringify(retained)})`);
    assert(retained.identity && retained.drift <= 4, `safety preserves native identity and reader offset (${retained.drift}px)`);
    if (scenario === 2) assert(retained.hasSelection && retained.selection, "safety preserves an actual native drag selection");
    if (scenario) assert(retained.writes === 0, "held safety transition accepts zero program writes");
    else assert(full.distance <= 4, `safety tail remains within 4px (${full.distance}px)`);
    const fullHeap = await heap();
    await page.evaluate(() => { delete window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__; delete window.__safetyProbe; });
    await page.mouse.up();
    await loadFixture(page, "bench:small-6t", "ASYNC LAYOUT EXPANSION COMPLETE");
    samples.push({ cycle, loadedTurns: full.completed, windowMounted: windowed.domBlocks, fullMounted: full.domBlocks, windowInteractiveMs, interactiveMs, windowHeap, fullHeap, releasedHeap: await heap(), releasedDom: await cdp.send("Memory.getDOMCounters") });
  }
  process.stdout.write(`  METRIC safety-paged-render ${JSON.stringify(samples)}\n`);
  await cdp.detach();
}

const preview = await startPreviewServer(frontendDir, port);
let browser;
try {
  await waitForServer();
  browser = await chromium.launch({
    headless: true,
    ...(process.env.PLAYWRIGHT_EXECUTABLE_PATH ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH } : {}),
  });
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 }, deviceScaleFactor: 1 });
  const pageErrors = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  await page.goto(url, { waitUntil: "domcontentloaded" });
  await page.waitForFunction(() => !document.querySelector(".startup-splash"), undefined, { timeout: 30_000 });
  await runGeometryFixture(page);
  await runWindowedFixture(page);
  await runSessionReplacement(page);
  await runSafetyFixture(page);
  assert(pageErrors.length === 0, `browser replay emits no page errors (${pageErrors.length})`);
  process.stdout.write("\ntranscript kernel browser gate passed\n");
} finally {
  await browser?.close();
  await preview.close();
}
