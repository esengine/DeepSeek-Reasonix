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
const bottomTimeout = Number(process.env.REASONIX_TRANSCRIPT_BOTTOM_TIMEOUT ?? 30_000);

function assert(condition, message) {
  if (!condition) throw new Error(message);
  process.stdout.write(`  PASS  ${message}\n`);
}

async function clickIfPresent(page, selector) {
  // Keep optional-control discovery and activation in one browser task so a
  // state update cannot unmount the element between separate Playwright calls.
  return page.evaluate((target) => {
    const element = document.querySelector(target);
    if (!(element instanceof HTMLElement)) return false;
    element.click();
    return true;
  }, selector);
}

async function waitForVisibleSelectionStart(page, { preferHighest, wheelDelta, timeout = 15_000 } = {}) {
  const box = await page.locator(".transcript").boundingBox();
  if (box) await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  const delta = wheelDelta ?? (preferHighest ? -400 : 400);
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const points = await page.evaluate((prefer) => {
      const transcript = document.querySelector(".transcript");
      if (!transcript) return null;
      const viewport = transcript.getBoundingClientRect();
      const candidates = [...transcript.querySelectorAll("[data-transcript-selectable]")]
        .map((element) => ({
          element,
          turn: Number(element.textContent?.match(/\bbench turn (\d+):/)?.[1] ?? -1),
          rect: element.getBoundingClientRect(),
        }))
        .filter(({ turn, rect }) => turn >= 0 && rect.height > 0 && rect.bottom > viewport.top && rect.top < viewport.bottom)
        .sort((left, right) => (prefer ? right.turn - left.turn : left.turn - right.turn));
      const candidate = candidates[0];
      if (!candidate) return null;
      const walker = document.createTreeWalker(candidate.element, NodeFilter.SHOW_TEXT);
      const rects = [];
      for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        if (!node.textContent?.trim()) continue;
        const range = document.createRange();
        range.selectNodeContents(node);
        rects.push(...range.getClientRects());
      }
      const start = rects.find((rect) => rect.width > 8 && rect.bottom > viewport.top && rect.top < viewport.bottom) ?? candidate.rect;
      if (!start) return null;
      const startX = Math.min(start.right - 4, start.left + Math.max(start.width * 0.45, 60));
      const y = (Math.max(start.top, viewport.top) + Math.min(start.bottom, viewport.bottom)) / 2;
      return {
        start: { x: startX, y },
        activate: { x: Math.min(start.right - 2, startX + 30), y },
        edge: { x: startX, y: prefer ? viewport.top + 2 : viewport.bottom - 2 },
        anchorTurn: candidate.turn,
      };
    }, preferHighest);
    if (points) return points;
    // Wheel, do not assign scrollTop: a pinned tail-follow snaps programmatic
    // writes back to the native bottom before Virtuoso can mount a user row.
    await page.mouse.wheel(0, delta);
    await page.waitForTimeout(80);
  }
  const diag = await page.evaluate(() => {
    const transcript = document.querySelector(".transcript");
    const viewport = transcript?.getBoundingClientRect();
    return {
      rows: document.querySelectorAll(".transcript__row").length,
      selectables: document.querySelectorAll("[data-transcript-selectable]").length,
      turnHits: [...document.querySelectorAll("[data-transcript-selectable]")].filter((el) => /\bbench turn \d+:/.test(el.textContent || "")).length,
      scrollTop: transcript?.scrollTop ?? null,
      scrollHeight: transcript?.scrollHeight ?? null,
      clientHeight: transcript?.clientHeight ?? null,
      mode: transcript?.dataset.scrollMode ?? null,
      viewport: viewport ? { top: viewport.top, bottom: viewport.bottom } : null,
    };
  });
  throw new Error(`no visible bench-turn selectable (${JSON.stringify(diag)})`);
}

async function findVisibleTurnTarget(page, turnPredicate) {
  return page.evaluate((predicate) => {
    const transcript = document.querySelector(".transcript");
    if (!transcript) return null;
    const viewport = transcript.getBoundingClientRect();
    const root = [...transcript.querySelectorAll("[data-transcript-selectable]")].find((element) => {
      const rect = element.getBoundingClientRect();
      const turn = Number(element.textContent?.match(/\bbench turn (\d+):/)?.[1] ?? -1);
      return (predicate.min != null ? turn >= predicate.min : turn <= predicate.max)
        && rect.height > 0 && rect.bottom > viewport.top && rect.top < viewport.bottom;
    });
    if (!root) return null;
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    const textRects = [];
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      if (!node.textContent?.trim()) continue;
      const range = document.createRange();
      range.selectNodeContents(node);
      textRects.push(...range.getClientRects());
    }
    const rect = textRects.find((candidate) =>
      candidate.width > 8
      && candidate.height > 0
      && candidate.bottom > viewport.top
      && candidate.top < viewport.bottom
    );
    if (!rect) return null;
    const visibleLeft = Math.max(rect.left, viewport.left);
    const visibleRight = Math.min(rect.right, viewport.right);
    if (visibleRight - visibleLeft < 4) return null;
    const width = visibleRight - visibleLeft;
    return {
      x: visibleLeft + width * 0.7,
      probeX: visibleLeft + width * 0.35,
      y: (Math.max(rect.top, viewport.top) + Math.min(rect.bottom, viewport.bottom)) / 2,
    };
  }, turnPredicate);
}

async function waitForLogicalSelection(page, timeout = 8_000) {
  return page.waitForFunction(
    () => document.querySelector(".transcript")?.dataset.scrollMode === "selection"
      && document.querySelectorAll(".transcript-selection-overlay__rect").length > 0
      && (document.getSelection()?.isCollapsed ?? true),
    undefined,
    { timeout },
  ).then(() => true, () => false);
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

async function runSelectionTableRepaintGeometry(page) {
  await page.click('.project-tree__topic-main:has-text("bench:selection-table")');
  await page.waitForFunction(() => (
    document.querySelector(".project-tree__topic--active .project-tree__topic-label")?.textContent?.includes("bench:selection-table")
      && [...document.querySelectorAll("strong")].some((element) => element.textContent?.includes("SELECTION REPAINT TARGET"))
  ), undefined, { timeout: 30_000 });
  await page.waitForFunction(() => !document.querySelector(".transcript-navigation-overlay"), undefined, { timeout: 30_000 });

  const target = page.locator("strong", { hasText: "SELECTION REPAINT TARGET" });
  await target.scrollIntoViewIfNeeded();
  await page.waitForTimeout(300);
  const targetBox = await target.boundingBox();
  assert(targetBox != null, "selection-table exposes the strong-text multi-click target");

  const initial = await page.evaluate(() => {
    const targetElement = [...document.querySelectorAll("strong")].find((element) => element.textContent?.includes("SELECTION REPAINT TARGET"));
    const transcript = document.querySelector(".transcript");
    const table = targetElement?.closest("table");
    const row = targetElement?.closest("tr");
    const host = document.querySelector('.transcript-selection-action[data-surface="transcript"]');
    if (!targetElement || !transcript || !table || !row || !host) return null;
    const abort = new AbortController();
    const rect = (element) => {
      const value = element.getBoundingClientRect();
      return { top: value.top, left: value.left, right: value.right, bottom: value.bottom };
    };
    const capture = (event) => {
      const currentHost = document.querySelector('.transcript-selection-action[data-surface="transcript"]');
      const selection = document.getSelection();
      window.__selectionTableProbe.samples.push({
        event,
        scrollTop: transcript.scrollTop,
        scrollHeight: transcript.scrollHeight,
        clientHeight: transcript.clientHeight,
        table: rect(table),
        row: rect(row),
        target: rect(targetElement),
        hostCount: document.querySelectorAll('.transcript-selection-action[data-surface="transcript"]').length,
        hostStable: currentHost === host,
        hostState: currentHost?.getAttribute("data-state") ?? null,
        selectionRects: selection?.rangeCount
          ? [...selection.getRangeAt(selection.rangeCount - 1).getClientRects()].map((value) => ({
              top: value.top,
              left: value.left,
              right: value.right,
              bottom: value.bottom,
            }))
          : [],
      });
    };
    window.__selectionTableProbe = { host, samples: [], capture, abort };
    document.addEventListener("pointerdown", () => capture("pointerdown"), { capture: true, signal: abort.signal });
    document.addEventListener("pointerup", () => capture("pointerup"), { capture: true, signal: abort.signal });
    document.addEventListener("selectionchange", () => capture("selectionchange"), { signal: abort.signal });
    capture("initial");
    return window.__selectionTableProbe.samples[0];
  });
  assert(initial != null, "selection-table geometry probe attaches to the settled transcript");
  assert(initial.scrollTop > 0, `selection-table target exercises a non-top transcript viewport (${initial.scrollTop})`);
  assert(initial.hostCount === 1 && initial.hostStable, "selection-table starts with one stable transcript action host");

  const clickIntervals = [400, 320, 180, 0];
  const clickPoint = { x: targetBox.x + targetBox.width / 2, y: targetBox.y + targetBox.height / 2 };
  await page.mouse.move(clickPoint.x, clickPoint.y);
  for (let index = 0; index < clickIntervals.length; index += 1) {
    await page.mouse.down({ button: "left", clickCount: index + 1 });
    await page.waitForTimeout(30);
    await page.mouse.up({ button: "left", clickCount: index + 1 });
    await page.evaluate(async (click) => {
      await new Promise((resolve) => requestAnimationFrame(resolve));
      window.__selectionTableProbe.capture(`click-${click}-raf-1`);
      await new Promise((resolve) => requestAnimationFrame(resolve));
      window.__selectionTableProbe.capture(`click-${click}-raf-2`);
    }, index + 1);
    if (clickIntervals[index] > 0) await page.waitForTimeout(clickIntervals[index]);
  }

  await page.keyboard.press("Escape");
  await page.evaluate(async () => {
    document.getSelection()?.removeAllRanges();
    await new Promise((resolve) => requestAnimationFrame(resolve));
    await new Promise((resolve) => requestAnimationFrame(resolve));
    window.__selectionTableProbe.capture("settled");
  });
  const result = await page.evaluate(() => {
    const probe = window.__selectionTableProbe;
    probe.abort.abort();
    const samples = probe.samples;
    const baseline = samples[0];
    const maxDelta = (field) => Math.max(...samples.map((sample) => Math.max(
      Math.abs(sample[field].top - baseline[field].top),
      Math.abs(sample[field].left - baseline[field].left),
    )));
    const settled = samples.at(-1);
    delete window.__selectionTableProbe;
    return {
      samples: samples.length,
      maxTableDelta: maxDelta("table"),
      maxRowDelta: maxDelta("row"),
      maxTargetDelta: maxDelta("target"),
      scrollStable: samples.every((sample) => (
        sample.scrollTop === baseline.scrollTop
          && sample.scrollHeight === baseline.scrollHeight
          && sample.clientHeight === baseline.clientHeight
      )),
      hostStable: samples.every((sample) => sample.hostCount === 1 && sample.hostStable),
      observedOpen: samples.some((sample) => sample.hostState === "open"),
      settledState: settled.hostState,
      settledButtonDisabled: document.querySelector('.transcript-selection-action[data-surface="transcript"] button')?.disabled,
      settledButtonTabIndex: document.querySelector('.transcript-selection-action[data-surface="transcript"] button')?.tabIndex,
    };
  });
  assert(result.samples >= 13, `selection-table records pointer, selection, and frame geometry (${result.samples} samples)`);
  assert(result.hostStable, "four native multi-clicks retain one transcript action host identity");
  assert(result.observedOpen, "native multi-click selection opens the transcript action host");
  assert(result.scrollStable, "native multi-click selection preserves scrollTop/scrollHeight/clientHeight");
  assert(result.maxTableDelta <= 0.5, `table top/left stay within 0.5px (${result.maxTableDelta})`);
  assert(result.maxRowDelta <= 0.5, `table row top/left stay within 0.5px (${result.maxRowDelta})`);
  assert(result.maxTargetDelta <= 0.5, `strong target top/left stay within 0.5px (${result.maxTargetDelta})`);
  assert(
    result.settledState === "closed" && result.settledButtonDisabled && result.settledButtonTabIndex === -1,
    "settled action host is closed, disabled, and outside the tab order",
  );
}

const preview = await startPreviewServer(frontendDir, port);

let browser;
try {
  await waitForServer();
  browser = await chromium.launch({
    headless: true,
    ...(process.env.PLAYWRIGHT_EXECUTABLE_PATH ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH } : {}),
  });
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
  await runSelectionTableRepaintGeometry(page);
  await page.click('.project-tree__topic-main:has-text("bench:tools-38t")');
  await page.waitForFunction(() => (
    document.querySelector(".project-tree__topic--active .project-tree__topic-label")?.textContent?.includes("bench:tools-38t")
      && document.querySelector(".transcript")?.textContent?.includes("pkg-41/mod.go")
  ), undefined, { timeout: 30_000 });
  await page.waitForFunction(
    () => !document.querySelector(".transcript-navigation-overlay"),
    undefined,
    { timeout: 30_000 },
  );
  await page.waitForTimeout(300);
  // The tool-dense bench mock serves a deterministic 1k-entry selection
  // window. Verify that it contains the full contract range before dragging;
  // paging farther back would intentionally evict its newest edge.
  const earliestLoadedTurn = () => page.evaluate(() => {
    const turns = [...document.querySelectorAll('.jump-item[data-loaded="true"]')]
      .map((marker) => Number(marker.getAttribute("data-turn")))
      .filter(Number.isFinite);
    return turns.length > 0 ? Math.min(...turns) : null;
  });
  const requiredFirstTurn = 18;
  const firstLoadedTurn = await earliestLoadedTurn();
  assert(firstLoadedTurn !== null && firstLoadedTurn <= requiredFirstTurn,
    `selection fixture preloads a 20+ turn range (${firstLoadedTurn})`);
  await clickIfPresent(page, ".transcript__jump-bottom");
  try {
    await page.waitForFunction(() => {
      const transcript = document.querySelector(".transcript");
      return transcript && transcript.scrollHeight - transcript.scrollTop - transcript.clientHeight <= 1;
    }, undefined, { timeout: bottomTimeout });
  } catch (error) {
    const bottomState = await page.evaluate(() => {
      const transcript = document.querySelector(".transcript");
      if (!transcript) return null;
      return {
        distance: transcript.scrollHeight - transcript.scrollTop - transcript.clientHeight,
        scrollTop: transcript.scrollTop,
        scrollHeight: transcript.scrollHeight,
        clientHeight: transcript.clientHeight,
        mode: transcript.dataset.scrollMode,
        rows: transcript.querySelectorAll(".transcript__row").length,
        padding: getComputedStyle(transcript).padding,
        viewportRect: transcript.querySelector('[data-viewport-type="element"]')?.getBoundingClientRect().toJSON(),
        listRect: transcript.querySelector(".transcript__virtual-sizer")?.getBoundingClientRect().toJSON(),
        lastRowRect: [...transcript.querySelectorAll(".transcript__row")].at(-1)?.getBoundingClientRect().toJSON(),
        transcriptRect: transcript.getBoundingClientRect().toJSON(),
      };
    });
    throw new Error(`jump-bottom did not settle at the native tail (${JSON.stringify(bottomState)})`, { cause: error });
  }
  // Tool-dense fixtures often land the native tail on non-selectable tool
  // rows. Wheel off the pinned tail until a mounted "bench turn N" row is
  // visible before beginning the logical selection gesture.
  let points = await waitForVisibleSelectionStart(page, { preferHighest: true, wheelDelta: -80 });
  assert(points != null, "bench transcript exposes a selectable visible message");
  assert(points.anchorTurn === 38, `selection starts from the final loaded question (${points.anchorTurn})`);

  let downState = null;
  for (let attempt = 0; attempt < 5; attempt += 1) {
    await page.mouse.move(points.start.x, points.start.y);
    await page.mouse.down();
    downState = await page.evaluate(({ x, y }) => ({
      mode: document.querySelector(".transcript")?.dataset.scrollMode,
      target: document.elementFromPoint(x, y)?.outerHTML.slice(0, 300),
    }), points.start);
    if (downState.mode === "selection") break;
    await page.mouse.up();
    await page.waitForTimeout(100);
    points = await waitForVisibleSelectionStart(page, { preferHighest: true });
  }
  assert(downState.mode === "selection", `primary pointerdown transfers scroll ownership to selection (${downState.mode}; ${downState.target})`);
  await page.evaluate(() => {
    window.__transcriptProgrammaticWrites = [];
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => {
      window.__transcriptProgrammaticWrites.push(write);
    };
  });
  await page.mouse.move(points.activate.x, points.activate.y, { steps: 6 });
  await page.waitForTimeout(50);
  // Cross at least one mounted row before relying on logical edge scrolling.
  // Small wheel steps expose a neighbouring selectable without skipping the
  // 20-turn target range on fast Chromium/Windows runners.
  for (let index = 0; index < 3; index += 1) {
    await page.mouse.wheel(0, -160);
    await page.mouse.move(points.edge.x, points.edge.y, { steps: 4 });
    await page.waitForTimeout(60);
  }
  await page.mouse.move(points.edge.x, points.edge.y);
  const neutralPoint = await page.evaluate(() => {
    const rect = document.querySelector(".transcript")?.getBoundingClientRect();
    return rect ? { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 } : null;
  });
  assert(neutralPoint != null, "deep logical drag keeps the transcript mounted");
  const focusTargetTurn = Math.max(0, points.anchorTurn - 20);
  const findLogicalFocusPoint = () => page.evaluate((targetTurn) => {
    const transcript = document.querySelector(".transcript");
    if (!transcript) return null;
    const viewport = transcript.getBoundingClientRect();
    const root = [...transcript.querySelectorAll("[data-transcript-selectable]")].find((element) => {
      const rect = element.getBoundingClientRect();
      const turn = element.textContent?.match(/\bbench turn (\d+):/);
      return turn && Number(turn[1]) <= targetTurn
        && rect.height > 0 && rect.bottom > viewport.top && rect.top < viewport.bottom;
    });
    if (!root) return null;
    const rect = root.getBoundingClientRect();
    return {
      x: Math.min(rect.right - 2, rect.left + 8),
      y: (Math.max(rect.top, viewport.top) + Math.min(rect.bottom, viewport.bottom)) / 2,
    };
  }, focusTargetTurn);
  let logicalFocusPoint = null;
  for (let index = 0; index < 600 && !logicalFocusPoint; index += 1) {
    logicalFocusPoint = await findLogicalFocusPoint();
    if (!logicalFocusPoint) await page.waitForTimeout(50);
  }
  await page.mouse.move(neutralPoint.x, neutralPoint.y);
  await page.waitForTimeout(100);
  const focusFailure = logicalFocusPoint ? null : await page.evaluate(() => {
    const transcript = document.querySelector(".transcript");
    const viewport = transcript?.getBoundingClientRect();
    const list = transcript?.querySelector('[data-testid="virtuoso-item-list"]');
    const mountedRows = [...(transcript?.querySelectorAll(".transcript__row") ?? [])];
    return {
      top: transcript?.scrollTop,
      height: transcript?.scrollHeight,
      mode: transcript?.dataset.scrollMode,
      hydrating: transcript?.dataset.transcriptHydrating,
      totalRows: transcript?.dataset.transcriptRowCount,
      readerVisualGuard: transcript?.dataset.transcriptReaderVisualGuard ?? null,
      listRect: list ? { top: list.getBoundingClientRect().top, bottom: list.getBoundingClientRect().bottom } : null,
      mountedRows: mountedRows.slice(0, 6).map((row) => ({
        index: row.dataset.index,
        key: row.dataset.rowKey,
        top: row.getBoundingClientRect().top,
        bottom: row.getBoundingClientRect().bottom,
      })),
      visibleTurns: [...(transcript?.querySelectorAll("[data-transcript-selectable]") ?? [])]
        .filter((row) => {
          const rect = row.getBoundingClientRect();
          return viewport && rect.bottom > viewport.top && rect.top < viewport.bottom;
        })
        .map((row) => row.textContent?.match(/\bbench turn (\d+):/)?.[1] ?? null),
      writeOwners: [...new Set((window.__transcriptProgrammaticWrites ?? []).map((write) => write.owner))],
    };
  });
  assert(logicalFocusPoint != null,
    `deep logical drag settles over a visible 20+ turn target${focusFailure ? ` (${JSON.stringify(focusFailure)})` : ""}`);
  // Rows can shift between measuring the focus target and delivering the
  // pointer move. Re-derive the coordinates on every attempt so the caret
  // ends on a mounted selectable row and the overlay actually paints.
  let overlayPainted = false;
  for (let attempt = 0; attempt < 5 && !overlayPainted; attempt += 1) {
    await page.mouse.move(logicalFocusPoint.x + 24, logicalFocusPoint.y, { steps: 4 });
    await page.mouse.move(logicalFocusPoint.x, logicalFocusPoint.y, { steps: 8 });
    overlayPainted = await page.waitForFunction(
      () => document.querySelectorAll(".transcript-selection-overlay__rect").length > 0,
      undefined,
      { timeout: 3_000 },
    ).then(() => true, () => false);
    if (!overlayPainted) logicalFocusPoint = (await findLogicalFocusPoint()) ?? logicalFocusPoint;
  }
  assert(overlayPainted, "cross-page drag paints the logical selection overlay");

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
  assert(during.mode === "selection", `cross-page drag remains owned by selection (${during.mode})`);
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
  const beforeCopy = await page.evaluate(() => ({
    overlayRects: document.querySelectorAll(".transcript-selection-overlay__rect").length,
    scrollTop: document.querySelector(".transcript")?.scrollTop ?? 0,
  }));
  assert(beforeCopy.overlayRects > 0, "logical overlay restores after selected rows scroll out and back in");

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
  const copiedTurnValues = [...copied.matchAll(/bench turn (\d+):/g)].map((match) => Number(match[1]));
  const copiedTurns = copiedTurnValues.length;
  assert(copiedTurns >= 20, `logical copy resolves a 20+ turn frozen snapshot (${copiedTurns} turns: ${copiedTurnValues.join(",")})`);

  await page.waitForTimeout(100);
  const after = await page.evaluate(() => {
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = undefined;
    return {
      collapsed: document.getSelection()?.isCollapsed ?? true,
      rows: document.querySelectorAll(".transcript__row").length,
      overlayRects: document.querySelectorAll(".transcript-selection-overlay__rect").length,
      scrollTop: document.querySelector(".transcript")?.scrollTop ?? 0,
    };
  });
  assert(after.collapsed, "logical copy leaves no synthetic browser Range behind");
  assert(after.overlayRects === 0, "successful copy clears the logical overlay");
  assert(
    Math.abs(after.scrollTop - beforeCopy.scrollTop) <= 1,
    `copy cleanup preserves the selection viewport (${beforeCopy.scrollTop} -> ${after.scrollTop})`,
  );
  assert(
    during.rows <= Math.ceil(after.rows * 1.1) + 2,
    `logical selection keeps the virtual DOM bounded (${after.rows} normal → ${during.rows} selecting)`,
  );
  const selectionHeapBaseline = await retainedHeap();

  await page.waitForFunction(() => document.querySelector(".transcript")?.dataset.scrollMode === "manual", undefined, { timeout: 10_000 });
  await page.mouse.up();
  // Same contract as the upward drag: start mid-text on an early mounted
  // turn, then land on a target at least 21 turns later. Programmatic
  // scrollTop=0 does not unpin tail-follow or load older virtual pages.
  const transcriptBox = await page.locator(".transcript").boundingBox();
  if (transcriptBox) await page.mouse.move(transcriptBox.x + transcriptBox.width / 2, transcriptBox.y + transcriptBox.height / 2);
  const topDeadline = Date.now() + 15_000;
  let forwardPoints = null;
  while (Date.now() < topDeadline && (!forwardPoints || forwardPoints.anchorTurn > 17)) {
    await clickIfPresent(page, ".transcript__older");
    await page.mouse.wheel(0, -700);
    await page.waitForTimeout(80);
    try {
      forwardPoints = await waitForVisibleSelectionStart(page, { preferHighest: false, wheelDelta: -400, timeout: 1_200 });
    } catch {
      forwardPoints = null;
    }
  }
  assert(forwardPoints != null, "settled reverse selection leaves a viewport where forward selection can start");
  assert(forwardPoints.anchorTurn <= 17, `forward drag must start early enough to cover 20 turns (${forwardPoints.anchorTurn})`);
  await page.evaluate(() => {
    window.__logicalClipboardText = null;
  });
  let forwardDownMode = null;
  for (let attempt = 0; attempt < 5; attempt += 1) {
    await page.mouse.move(forwardPoints.start.x, forwardPoints.start.y);
    await page.mouse.down();
    forwardDownMode = await page.locator(".transcript").getAttribute("data-scroll-mode");
    if (forwardDownMode === "selection") break;
    await page.mouse.up();
    await page.waitForTimeout(100);
    forwardPoints = await waitForVisibleSelectionStart(page, {
      preferHighest: false,
      wheelDelta: -80,
      timeout: 1_200,
    });
  }
  assert(forwardDownMode === "selection",
    `forward pointerdown transfers scroll ownership to selection (${forwardDownMode})`);
  await page.evaluate(() => {
    window.__transcriptProgrammaticWrites = [];
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => {
      window.__transcriptProgrammaticWrites.push(write);
    };
  });
  await page.mouse.move(forwardPoints.activate.x, forwardPoints.activate.y, { steps: 6 });
  const forwardTargetTurn = forwardPoints.anchorTurn + 21;
  let forwardFocus = null;
  for (let index = 0; index < 80 && !forwardFocus; index += 1) {
    forwardFocus = await findVisibleTurnTarget(page, { min: forwardTargetTurn });
    if (!forwardFocus) {
      await page.mouse.wheel(0, 250);
      await page.mouse.move(forwardPoints.edge.x, forwardPoints.edge.y, { steps: 4 });
      await page.waitForTimeout(50);
    }
  }
  assert(forwardFocus != null, "downward logical drag settles over a visible 20+ turn target");
  let forwardPromoted = false;
  for (let attempt = 0; attempt < 5 && !forwardPromoted; attempt += 1) {
    // Stay inside a real text client rect. The selectable row is a full-width
    // block, so its geometric center can be empty padding on slower layouts;
    // moving there does not extend the browser Range and cannot promote the
    // cross-row gesture to the logical selection owner.
    await page.mouse.move(forwardFocus.probeX, forwardFocus.y, { steps: 4 });
    await page.mouse.move(forwardFocus.x, forwardFocus.y, { steps: 8 });
    forwardPromoted = await waitForLogicalSelection(page, 3_000);
    if (!forwardPromoted) forwardFocus = (await findVisibleTurnTarget(page, { min: forwardTargetTurn })) ?? forwardFocus;
  }
  const forwardDuring = await page.evaluate(() => ({
    mode: document.querySelector(".transcript")?.dataset.scrollMode,
    rows: document.querySelectorAll(".transcript__row").length,
    overlayRects: document.querySelectorAll(".transcript-selection-overlay__rect").length,
    nativeCollapsed: document.getSelection()?.isCollapsed ?? true,
    owners: [...new Set((window.__transcriptProgrammaticWrites ?? []).map((write) => write.owner))],
  }));
  assert(forwardPromoted && forwardDuring.mode === "selection",
    `downward cross-page drag also promotes to logical selection (${JSON.stringify(forwardDuring)})`);
  assert(forwardDuring.owners.every((owner) => owner === "selection-edge-scroll"), "forward logical gesture preserves scroll ownership");
  await page.mouse.up();
  await page.keyboard.press(process.platform === "darwin" ? "Meta+C" : "Control+C");
  await page.waitForFunction(() => typeof window.__logicalClipboardText === "string", undefined, { timeout: 30_000 });
  const forwardCopiedTurns = await page.evaluate(() => (window.__logicalClipboardText.match(/bench turn /g) ?? []).length);
  assert(forwardCopiedTurns >= 20, `forward logical copy resolves a 20+ turn frozen snapshot (${forwardCopiedTurns} turns)`);
  await page.waitForFunction(() => document.querySelectorAll(".transcript-selection-overlay__rect").length === 0);
  const forwardAfterRows = await page.locator(".transcript__row").count();
  assert(
    forwardDuring.rows <= Math.ceil(forwardAfterRows * 1.1) + 2,
    `forward logical selection also keeps the virtual DOM bounded (${forwardAfterRows} normal → ${forwardDuring.rows} selecting)`,
  );
  const retainedSelectionBytes = Math.max(0, (await retainedHeap()) - selectionHeapBaseline);
  assert(retainedSelectionBytes <= 2 * 1024 * 1024, `cleared logical selection retains at most 2MiB (${(retainedSelectionBytes / 1024 / 1024).toFixed(2)}MiB)`);
  await page.evaluate(() => { window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = undefined; });
} finally {
  await browser?.close();
  await preview.close();
}
