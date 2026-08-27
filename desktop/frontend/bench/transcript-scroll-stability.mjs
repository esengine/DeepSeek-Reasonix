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
const maxFrameGapMs = Number(process.env.REASONIX_TRANSCRIPT_MAX_FRAME_GAP_MS ?? 250);
const p95FrameGapMs = Number(process.env.REASONIX_TRANSCRIPT_P95_FRAME_GAP_MS ?? 80);
const maxLongTaskMs = Number(process.env.REASONIX_TRANSCRIPT_MAX_LONG_TASK_MS ?? 250);
const totalLongTaskMs = Number(process.env.REASONIX_TRANSCRIPT_TOTAL_LONG_TASK_MS ?? 750);
const nativeThumbReplay = process.platform === "linux"
  && process.env.REASONIX_TRANSCRIPT_NATIVE_THUMB === "1";

function assert(condition, message) {
  if (!condition) throw new Error(message);
  process.stdout.write(`  PASS  ${message}\n`);
}

async function moveToOuterReaderGutter(page, transcript, announce = true) {
  const deadline = Date.now() + 5_000;
  let point = null;
  while (!point && Date.now() < deadline) {
    point = await transcript.evaluate((element) => {
      const rect = element.getBoundingClientRect();
      const rows = [...element.querySelectorAll(".transcript__row")];
      for (const row of rows) {
        const rowRect = row.getBoundingClientRect();
        const visibleTop = Math.max(rect.top, rowRect.top);
        const visibleBottom = Math.min(rect.bottom, rowRect.bottom);
        if (visibleBottom - visibleTop < 2) continue;
        const y = visibleTop + (visibleBottom - visibleTop) / 2;
        // Rows reserve 32px of inline padding outside every tool/code child.
        // Prefer the left edge because the question navigator can cover the
        // right edge, and require hit-testing to resolve to the row itself.
        for (const x of [rowRect.left + 16, rowRect.right - 16]) {
          if (document.elementFromPoint(x, y) === row) return { x, y };
        }
      }
      return null;
    });
    if (!point) await page.waitForTimeout(16);
  }
  if (announce) assert(point != null, "outer reader wheel target resolves to visible row padding");
  else if (point == null) throw new Error("outer reader wheel target is unavailable during traversal");
  await page.mouse.move(point.x, point.y);
}

async function waitForStableTranscriptGeometry(
  page,
  { timeout = 15_000, frames = 8, requireTail = false } = {},
) {
  await page.evaluate(({ timeout, frames, requireTail }) => new Promise((resolve, reject) => {
    const startedAt = performance.now();
    let previous = null;
    let stableFrames = 0;
    let lastSample = null;
    const sample = () => {
      const element = document.querySelector(".transcript");
      if (element instanceof HTMLElement) {
        const current = {
          mode: element.dataset.scrollMode,
          height: element.scrollHeight,
          top: element.scrollTop,
          clientHeight: element.clientHeight,
        };
        lastSample = { ...current, distance: current.height - current.top - current.clientHeight, stableFrames };
        const unchanged = previous != null
          && current.mode === previous.mode
          && Math.abs(current.height - previous.height) <= 0.5
          && Math.abs(current.top - previous.top) <= 0.5
          && current.clientHeight === previous.clientHeight;
        stableFrames = unchanged ? stableFrames + 1 : 0;
        previous = current;
        const distance = current.height - current.top - current.clientHeight;
        const expectedPosition = !requireTail || (current.mode === "tail-follow" && distance <= 4);
        if (expectedPosition && stableFrames >= frames) {
          resolve();
          return;
        }
      } else {
        previous = null;
        stableFrames = 0;
      }
      if (performance.now() - startedAt >= timeout) {
        reject(new Error(`transcript geometry did not stay stable for ${frames} frames: ${JSON.stringify(lastSample)}`));
        return;
      }
      requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
  }), { timeout, frames, requireTail });
}

async function openGeometryContractFixture(page) {
  await page.click('.project-tree__topic-main:has-text("bench:geometry-229")');
  await page.waitForFunction(
    () => document.querySelector(".transcript")?.textContent?.includes("Geometry contract fixture complete."),
    undefined,
    { timeout: 30_000 },
  );
  await waitForStableTranscriptGeometry(page, { timeout: 30_000, requireTail: true });
  return page.locator(".transcript");
}

async function runGeometryContractTraversal(page, label) {
  const transcript = page.locator(".transcript");
  const fixtureShape = await transcript.evaluate((element) => ({
    totalRows: Number.parseInt(element.dataset.transcriptRowCount ?? "0", 10),
    clientHeight: element.clientHeight,
  }));
  assert(
    fixtureShape.totalRows >= 220 && fixtureShape.totalRows <= 240,
    `${label}: sanitized fixture stays near 229 rows (${fixtureShape.totalRows})`,
  );
  await page.evaluate(() => {
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => {
      window.__geometryContractProbe?.writes.push(write);
    };
  });
  await transcript.evaluate(() => {
    const compact = {};
    window.__geometryContractProbe = { active: true, frames: [], compact, allRows: {}, writes: [] };
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => window.__geometryContractProbe?.writes.push(write);
    const sample = () => {
      const probe = window.__geometryContractProbe;
      const element = document.querySelector(".transcript");
      if (!probe?.active || !(element instanceof HTMLElement)) return;
      const viewport = element.getBoundingClientRect();
      const mounted = [...element.querySelectorAll(".transcript__row")];
      const visible = mounted
        .filter((row) => {
          const rect = row.getBoundingClientRect();
          return rect.bottom > viewport.top && rect.top < viewport.bottom;
        })
        .sort((left, right) => left.getBoundingClientRect().top - right.getBoundingClientRect().top);
      const first = visible[0];
      const firstVisibleIndex = first instanceof HTMLElement
        ? Number.parseInt(first.dataset.logicalIndex ?? first.dataset.index ?? "", 10)
        : Number.NaN;
      probe.frames.push({
        top: element.scrollTop,
        height: element.scrollHeight,
        estimatedTotal: element.dataset.transcriptEstimatedTotal,
        firstVisibleIndex: Number.isFinite(firstVisibleIndex) ? firstVisibleIndex : null,
        firstVisibleTop: first instanceof HTMLElement ? first.getBoundingClientRect().top - viewport.top : null,
        occupied: visible.length > 0,
        visibleRows: visible.slice(0, 8).map((row) => ({
          index: row.dataset.logicalIndex ?? row.dataset.index,
          key: row.dataset.rowKey,
          kind: row.dataset.rowKind,
          variant: row.dataset.transcriptLayoutVariant,
          height: row.getBoundingClientRect().height,
          top: row.getBoundingClientRect().top - viewport.top,
        })),
      });
      for (const row of mounted) {
        if (!(row instanceof HTMLElement)) continue;
        const variant = row.dataset.transcriptLayoutVariant ?? "";
        const index = Number.parseInt(row.dataset.logicalIndex ?? row.dataset.index ?? "", 10);
        const estimated = Number.parseFloat(row.dataset.estimatedSize ?? row.dataset.staticEstimate ?? "");
        const measured = Number.parseFloat(row.dataset.knownSize ?? "") || row.getBoundingClientRect().height;
        if (!Number.isFinite(index) || !Number.isFinite(estimated) || !Number.isFinite(measured)) continue;
        const key = `${index}:${variant}`;
        const error = Math.abs(measured - estimated);
        const delta = measured - estimated;
        const rowSample = {
          index,
          key: row.dataset.rowKey,
          preview: row.textContent?.slice(0, 48),
          kind: row.dataset.rowKind,
          variant,
          estimated,
          measured,
          error,
          delta,
        };
        const previousAll = probe.allRows[key];
        if (!previousAll || error > previousAll.error) probe.allRows[key] = rowSample;
        if (variant === "static" || variant === "text-flow" || variant.endsWith("-expanded")) continue;
        const previous = compact[key];
        if (!previous || error > previous.error) compact[key] = rowSample;
      }
      requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
  });
  await moveToOuterReaderGutter(page, transcript);
  let reachedTop = false;
  for (let attempt = 0; attempt < 240 && !reachedTop; attempt += 1) {
    // Virtuoso recycles the row under a fixed pointer coordinate. Re-resolve
    // the outer reader target so every synthetic wheel is delivered through
    // the same capture path as a real continuous gesture.
    await moveToOuterReaderGutter(page, transcript, false);
    await page.mouse.wheel(0, -80);
    await page.waitForTimeout(60);
    reachedTop = await transcript.evaluate((element) => element.scrollTop <= 1);
  }
  assert(reachedTop, `${label}: first upward traversal reaches the physical top`);
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  const probe = await transcript.evaluate(() => {
    const current = window.__geometryContractProbe ?? { frames: [], compact: {}, writes: [] };
    current.active = false;
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = undefined;
    window.__geometryContractProbe = undefined;
    return current;
  });
  const occupied = probe.frames.every((frame) => frame.occupied);
  let maxReversePx = 0;
  let maxReverseRows = 0;
  let worstReversePair = null;
  let reverseRun = 0;
  let maxReverseRun = 0;
  let maxHeightDrop = 0;
  for (let index = 1; index < probe.frames.length; index += 1) {
    const previous = probe.frames[index - 1];
    const current = probe.frames[index];
    maxReversePx = Math.max(maxReversePx, current.top - previous.top);
    maxHeightDrop = Math.max(maxHeightDrop, previous.height - current.height);
    if (previous.firstVisibleIndex != null && current.firstVisibleIndex != null) {
      const reverseRows = current.firstVisibleIndex - previous.firstVisibleIndex;
      if (reverseRows > maxReverseRows) {
        maxReverseRows = reverseRows;
        worstReversePair = { previous, current };
      }
      const visualReverse = current.firstVisibleIndex > previous.firstVisibleIndex
        || (current.firstVisibleIndex === previous.firstVisibleIndex
          && previous.firstVisibleTop != null
          && current.firstVisibleTop != null
          && current.firstVisibleTop < previous.firstVisibleTop - 2);
      reverseRun = visualReverse ? reverseRun + 1 : 0;
      maxReverseRun = Math.max(maxReverseRun, reverseRun);
    }
  }
  const compactRows = Object.values(probe.compact);
  const reasoningRows = compactRows.filter((row) => row.variant === "reasoning-summary");
  const reasoningError = Math.max(0, ...reasoningRows.map((row) => row.error));
  const otherCompactError = Math.max(0, ...compactRows.filter((row) => row.variant !== "reasoning-summary").map((row) => row.error));
  const forbiddenWrites = probe.writes.filter((write) => write.owner === "recovery" || write.owner === "anchor-compensation");
  const largestOverestimates = Object.values(probe.allRows)
    .filter((row) => row.delta < 0)
    .sort((left, right) => left.delta - right.delta)
    .slice(0, 8);
  const largestUnderestimates = Object.values(probe.allRows)
    .filter((row) => row.delta > 0)
    .sort((left, right) => right.delta - left.delta)
    .slice(0, 8);
  assert(occupied, `${label}: every sampled frame keeps a mounted row in the viewport`);
  assert(maxReverseRows <= 1, `${label}: first visible row never flashes downward by more than one row (${maxReverseRows}; ${JSON.stringify(worstReversePair)}; reasoning error ${reasoningError.toFixed(2)}; compact error ${otherCompactError.toFixed(2)}; height drop ${maxHeightDrop.toFixed(2)}; over ${JSON.stringify(largestOverestimates)}; under ${JSON.stringify(largestUnderestimates)}; writes ${JSON.stringify(probe.writes.slice(-8))})`);
  assert(maxReverseRun <= 1, `${label}: no visible reverse displacement persists beyond one frame (${maxReverseRun}; physical anchor adjustment ${maxReversePx.toFixed(2)}px)`);
  assert(maxHeightDrop < fixtureShape.clientHeight / 2, `${label}: list height never contracts by half a viewport (${maxHeightDrop.toFixed(2)}px)`);
  assert(reasoningRows.length === 31, `${label}: traversal measures all 31 folded reasoning rows (${reasoningRows.length})`);
  assert(reasoningError <= 2, `${label}: folded reasoning estimate error stays within 2px (${reasoningError.toFixed(2)}px)`);
  assert(otherCompactError <= 8, `${label}: other compact estimate errors stay within 8px (${otherCompactError.toFixed(2)}px)`);
  assert(forbiddenWrites.length === 0, `${label}: normal traversal emits zero recovery/anchor-compensation writes (${forbiddenWrites.length})`);
  return { reasoningError, otherCompactError, totalRows: fixtureShape.totalRows };
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

const packageManager = process.platform === "win32" ? "pnpm.cmd" : "pnpm";
const preview = spawn(packageManager, ["exec", "vite", "preview", "--port", String(port), "--strictPort", "--host", "127.0.0.1"], {
  cwd: frontendDir,
  stdio: "ignore",
  shell: process.platform === "win32",
});

let browser;
try {
  await waitForServer();
  browser = await chromium.launch({
    // Chromium's headless compositor reserves the native scrollbar gutter on
    // Linux but does not expose its thumb to pointer input. CI runs this replay
    // in Xvfb so the real browser-owned thumb remains draggable; other hosts
    // keep the deterministic headless path and their platform-specific gates.
    headless: !nativeThumbReplay,
    ...(process.env.PLAYWRIGHT_EXECUTABLE_PATH ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH } : {}),
  });
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  // Both 1.27.0 field reports keep completed working steps expanded. Apply the
  // preference before the app mounts so every long-session, native-thumb, and
  // measurement-churn assertion exercises the larger virtual row model.
  await page.addInitScript(() => localStorage.setItem("reasonix-process-fold", "expanded"));
  await page.goto(url, { waitUntil: "domcontentloaded" });
  await page.waitForFunction(() => !document.querySelector(".startup-splash"), undefined, { timeout: 30_000 });
  await page.click('.project-tree__topic-main:has-text("bench:small-6t")');
  await page.waitForFunction(() => document.querySelectorAll(".transcript__row").length > 4, undefined, { timeout: 30_000 });
  await page.waitForFunction(() => document.querySelector(".transcript")?.textContent?.includes("Asynchronously hydrated verification appendix"), undefined, { timeout: 30_000 });
  await page.evaluate(() => new Promise((resolve) => {
    let frames = 8;
    const settle = () => frames-- <= 0 ? resolve() : requestAnimationFrame(settle);
    requestAnimationFrame(settle);
  }));
  const hydrationTranscript = page.locator(".transcript");
  const hydrationBox = await hydrationTranscript.boundingBox();
  assert(hydrationBox != null, "async hydration exposes the transcript viewport");
  const hydrationStart = await hydrationTranscript.evaluate((element) => ({
    top: element.scrollTop,
    max: element.scrollHeight - element.clientHeight,
  }));
  assert(hydrationStart.max > 0, `async hydration fixture is scrollable (${hydrationStart.max}px)`);
  await page.mouse.move(hydrationBox.x + hydrationBox.width / 2, hydrationBox.y + hydrationBox.height / 2);
  await page.mouse.wheel(0, hydrationStart.top < hydrationStart.max / 2 ? 360 : -360);
  await page.waitForFunction(
    (startTop) => {
      const element = document.querySelector(".transcript");
      return element instanceof HTMLElement
        && element.dataset.scrollMode === "manual"
        && Math.abs(element.scrollTop - startTop) > 1;
    },
    hydrationStart.top,
    { timeout: 5_000 },
  );
  await page.evaluate(() => {
    const element = document.querySelector(".transcript");
    if (!(element instanceof HTMLElement)) return;
    element.scrollTop = Math.max(0, element.scrollHeight - element.clientHeight * 2.5);
    element.dispatchEvent(new Event("scroll"));
  });
  await page.waitForFunction(
    () => document.querySelector(".transcript")?.dataset.scrollMode === "manual",
    undefined,
    { timeout: 5_000 },
  );
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  const hydrationAnchor = await page.evaluate(() => {
    const element = document.querySelector(".transcript");
    if (!(element instanceof HTMLElement)) return null;
    const viewport = element.getBoundingClientRect();
    const visible = [...element.querySelectorAll(".transcript__row")]
      .filter((row) => row.getBoundingClientRect().bottom > viewport.top && row.getBoundingClientRect().top < viewport.bottom)
      .sort((left, right) => left.getBoundingClientRect().top - right.getBoundingClientRect().top);
    const anchor = visible.find((row) => row.getBoundingClientRect().top >= viewport.top) ?? visible[0];
    return anchor ? { key: anchor.dataset.rowKey, offset: anchor.getBoundingClientRect().top - viewport.top } : null;
  });
  assert(hydrationAnchor?.key, "async hydration starts from a stable manual-reading anchor");
  await page.waitForFunction(() => document.querySelector(".transcript")?.textContent?.includes("ASYNC LAYOUT EXPANSION COMPLETE"), undefined, { timeout: 30_000 });
  await page.waitForFunction((anchor) => {
    const element = document.querySelector(".transcript");
    if (!(element instanceof HTMLElement)) return false;
    const row = [...element.querySelectorAll(".transcript__row")].find((candidate) => candidate.dataset.rowKey === anchor.key);
    return Boolean(row && Math.abs((row.getBoundingClientRect().top - element.getBoundingClientRect().top) - anchor.offset) <= 8);
  }, hydrationAnchor, { timeout: 5_000 });
  const hydratedState = await page.evaluate((anchor) => {
    const element = document.querySelector(".transcript");
    if (!(element instanceof HTMLElement)) return { occupied: false, anchorOffset: null };
    const viewport = element.getBoundingClientRect();
    const rows = [...element.querySelectorAll(".transcript__row")];
    const anchored = rows.find((row) => row.dataset.rowKey === anchor.key);
    return { occupied: rows.some((row) => {
      const rect = row.getBoundingClientRect();
      return rect.bottom > viewport.top && rect.top < viewport.bottom;
    }), anchorOffset: anchored ? anchored.getBoundingClientRect().top - viewport.top : null };
  }, hydrationAnchor);
  assert(hydratedState.occupied, "async full-content hydration keeps a transcript row in the viewport");
  assert(
    hydratedState.anchorOffset != null && Math.abs(hydratedState.anchorOffset - hydrationAnchor.offset) <= 8,
    `async hydration preserves the manual-reading anchor (${hydrationAnchor.offset} → ${hydratedState.anchorOffset})`,
  );
  await page.evaluate(() => new Promise((resolve) => {
    let frames = 12;
    const settle = () => frames-- <= 0 ? resolve() : requestAnimationFrame(settle);
    requestAnimationFrame(settle);
  }));
  await moveToOuterReaderGutter(page, hydrationTranscript);
  // Full Markdown hydration can legitimately expand this synthetic document
  // to ~30k CSS px. Keep driving the same outer-reader gesture until the
  // physical tail is reached instead of assuming the old underestimated tree.
  let previousDownwardTop = -1;
  for (let attempt = 0; attempt < 128; attempt += 1) {
    await moveToOuterReaderGutter(page, hydrationTranscript, false);
    await page.mouse.wheel(0, 10_000);
    const position = await hydrationTranscript.evaluate((element) => ({
      top: element.scrollTop,
      atBottom: element.scrollHeight - element.scrollTop - element.clientHeight <= 1,
    }));
    if (position.atBottom) break;
    // A hydration resize intentionally guards the reader's previous extent
    // for one intent burst. Start a fresh burst after the 180ms idle lease
    // when that boundary is reached, matching a real user's next wheel turn.
    const stalled = Math.abs(position.top - previousDownwardTop) <= 1;
    previousDownwardTop = position.top;
    await page.waitForTimeout(stalled ? 220 : 16);
  }
  await page.waitForFunction(() => {
    const element = document.querySelector(".transcript");
    return element instanceof HTMLElement
      && element.dataset.scrollMode === "tail-follow"
      && element.scrollHeight - element.scrollTop - element.clientHeight <= 1;
  });
  // Rapid A→B→A switches reproduce the report where a callback from the
  // previous session landed on the newly mounted scrollport. The last topic
  // owns the surface and opens at its physical tail.
  await page.evaluate(() => {
    const topic = (label) => [...document.querySelectorAll(".project-tree__topic-main")]
      .find((candidate) => candidate.textContent?.includes(label));
    topic("bench:reported-long-turn")?.click();
    topic("bench:tools-38t")?.click();
    topic("bench:reported-long-turn")?.click();
  });
  await page.waitForFunction(
    () => document.querySelector(".project-tree__topic--active .project-tree__topic-label")?.textContent?.includes("bench:reported-long-turn"),
    undefined,
    { timeout: 30_000 },
  );
  await page.waitForFunction(
    () => document.querySelector(".transcript")?.textContent?.includes("Reported long turn complete."),
    undefined,
    { timeout: 30_000 },
  );
  // Tail ownership uses the shared 4px native-bottom threshold because Linux
  // and WebView2 can quantize a fractional layout to different integer scroll
  // extents. Require eight stable frames inside that product threshold instead
  // of accepting one transient exact-bottom sample.
  await waitForStableTranscriptGeometry(page, { timeout: 30_000, requireTail: true });
  assert(true, "rapid A→B→A switching leaves the reported long-turn session at its physical bottom");
  await openGeometryContractFixture(page);
  await runGeometryContractTraversal(page, "DPR 1 first visit");
  await page.click('.project-tree__topic-main:has-text("bench:small-6t")');
  await page.waitForFunction(
    () => document.querySelector(".project-tree__topic--active .project-tree__topic-label")?.textContent?.includes("bench:small-6t"),
    undefined,
    { timeout: 30_000 },
  );
  await openGeometryContractFixture(page);
  await runGeometryContractTraversal(page, "DPR 1 A→B→A revisit");
  await page.click('.project-tree__topic-main:has-text("bench:tools-38t")');
  await page.waitForFunction(() => document.querySelector(".project-tree__topic--active .project-tree__topic-label")?.textContent?.includes("bench:tools-38t"));
  await page.waitForFunction(() => document.querySelector(".transcript")?.textContent?.includes("pkg-41/mod.go"), undefined, { timeout: 30_000 });
  await page.waitForFunction(() => !document.querySelector(".transcript-navigation-overlay"), undefined, { timeout: 30_000 });
  const markdownVisibility = await page.evaluate(() => {
    const row = document.querySelector(".transcript__row");
    if (!(row instanceof HTMLElement)) return { inside: null, outside: null };
    const mount = (parent) => {
      const host = document.createElement("div");
      host.className = "md";
      const probe = document.createElement("p");
      host.append(probe);
      parent.append(host);
      const value = getComputedStyle(probe).contentVisibility;
      host.remove();
      return value;
    };
    return { inside: mount(row), outside: mount(document.body) };
  });
  assert(
    markdownVisibility.inside === "visible",
    `mounted transcript markdown stays measurable (${markdownVisibility.inside})`,
  );
  assert(
    markdownVisibility.outside === "auto",
    `markdown outside the transcript still culls with content-visibility (${markdownVisibility.outside})`,
  );

  const transcript = page.locator(".transcript");
  const box = await transcript.boundingBox();
  assert(box != null, "bench exposes the Virtuoso transcript viewport");
  assert(await page.locator('[data-virtuoso-scroller="true"]').count() === 1, "Transcript is backed by React Virtuoso");

  await page.evaluate(() => {
    window.__reasonixQuestionJumpWrites = [];
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => {
      if (write.owner === "jump") window.__reasonixQuestionJumpWrites.push(write);
    };
  });
  const questionRail = page.locator(".jump-scroll");
  const questionRailBox = await questionRail.boundingBox();
  assert(questionRailBox != null, "long transcript exposes the question navigator rail");
  const questionTargetPoint = await page.evaluate(() => {
    const rail = document.querySelector(".jump-scroll");
    if (!(rail instanceof HTMLElement)) return null;
    const railRect = rail.getBoundingClientRect();
    const marker = [...rail.querySelectorAll(".jump-item")].find((item) => {
      const rect = item.getBoundingClientRect();
      const middle = rect.top + rect.height / 2;
      return middle >= railRect.top && middle <= railRect.bottom;
    });
    if (!(marker instanceof HTMLElement)) return null;
    const rect = marker.getBoundingClientRect();
    return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 };
  });
  assert(questionTargetPoint != null, "question navigator exposes an earlier visible marker");
  const staleSelectionMode = await page.evaluate(() => {
    const target = document.querySelector("[data-transcript-selectable]");
    const transcript = document.querySelector(".transcript");
    if (!(target instanceof HTMLElement) || !(transcript instanceof HTMLElement)) return "missing";
    const rect = target.getBoundingClientRect();
    target.dispatchEvent(new PointerEvent("pointerdown", {
      bubbles: true,
      cancelable: true,
      button: 0,
      pointerId: 9013,
      clientX: rect.left + 4,
      clientY: rect.top + 4,
    }));
    return transcript.dataset.scrollMode;
  });
  assert(staleSelectionMode === "selection", "lost WebView2 pointerup leaves selection owning transcript scroll");
  await page.mouse.click(
    questionTargetPoint.x,
    questionTargetPoint.y,
  );
  const questionJumpWrites = await page.evaluate(() => {
    const writes = window.__reasonixQuestionJumpWrites ?? [];
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = undefined;
    window.__reasonixQuestionJumpWrites = undefined;
    return writes;
  });
  assert(questionJumpWrites.length === 1, `question navigation clears stale selection and emits one indexed jump (${questionJumpWrites.length})`);
  await page.locator(".transcript__jump-bottom").waitFor({ state: "visible" });
  // The question jump itself is smooth. Wait for its geometry to settle before
  // clicking the current button: resolving a locator while React remounts the
  // moving control can otherwise call click() on a detached node, which never
  // reaches React's delegated handler.
  await waitForStableTranscriptGeometry(page, { timeout: 5_000, frames: 3 });
  await page.evaluate(() => {
    window.__reasonixJumpBottomWrites = [];
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => window.__reasonixJumpBottomWrites.push(write);
  });
  const jumpBottomClicked = await page.evaluate(() => {
    const button = document.querySelector(".transcript__jump-bottom");
    if (!(button instanceof HTMLButtonElement) || !button.isConnected) return false;
    button.click();
    return true;
  });
  assert(jumpBottomClicked, "question jump exposes a connected jump-bottom control");
  // The arbiter's rest threshold is 4px: a late sub-threshold remeasure can
  // legally rest at 2-4px from the extent without earning another write.
  try {
    await page.waitForFunction(() => {
      const element = document.querySelector(".transcript");
      return element instanceof HTMLElement
        && element.dataset.scrollMode === "tail-follow"
        && element.scrollHeight - element.scrollTop - element.clientHeight <= 4;
    });
  } catch (error) {
    const state = await page.evaluate(() => {
      const element = document.querySelector(".transcript");
      return element instanceof HTMLElement ? {
        mode: element.dataset.scrollMode,
        distance: element.scrollHeight - element.scrollTop - element.clientHeight,
        scrollTop: element.scrollTop,
        scrollHeight: element.scrollHeight,
        clientHeight: element.clientHeight,
        jumpVisible: Boolean(document.querySelector(".transcript__jump-bottom")),
        selectionCollapsed: document.getSelection()?.isCollapsed ?? null,
        writes: window.__reasonixJumpBottomWrites ?? [],
      } : null;
    });
    throw new Error(`question jump-bottom did not settle (${JSON.stringify(state)})`, { cause: error });
  } finally {
    await page.evaluate(() => {
      window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = undefined;
      window.__reasonixJumpBottomWrites = undefined;
    });
  }

  // Stay on the tail. Opening the workspace dock must not crop right-aligned
  // user bubbles — that is a width/padding bug, not the scroll-up overlap.
  const measureDockCrop = () => page.evaluate(() => {
    const layout = document.querySelector(".layout");
    const chat = document.querySelector(".chat-pane");
    const dock = document.querySelector(".workbench-dock");
    const scroller = document.querySelector(".transcript");
    const bubbles = [...document.querySelectorAll(".msg--user .msg__body")];
    const bubble = bubbles.at(-1);
    if (!(chat instanceof HTMLElement) || !(bubble instanceof HTMLElement) || !(scroller instanceof HTMLElement)) {
      return { ok: false };
    }
    const chatBox = chat.getBoundingClientRect();
    const bubbleBox = bubble.getBoundingClientRect();
    const dockBox = dock instanceof HTMLElement ? dock.getBoundingClientRect() : null;
    return {
      ok: true,
      workspaceOpen: Boolean(layout?.classList.contains("layout--workspace-open")),
      overflowChatRight: +(bubbleBox.right - chatBox.right).toFixed(2),
      overflowDock: dockBox ? +(bubbleBox.right - dockBox.left).toFixed(2) : null,
      fromBottom: +(scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight).toFixed(2),
    };
  });
  // Late hydration can momentarily detach the tail between the previous wait
  // and this sample; the crop contract only needs a converged viewport.
  await page.waitForFunction(() => {
    const element = document.querySelector(".transcript");
    return element instanceof HTMLElement
      && element.scrollHeight - element.scrollTop - element.clientHeight <= 4;
  });
  const dockOpen = await measureDockCrop();
  assert(dockOpen.ok, "tail-follow dock check can see the chat and a user bubble");
  assert(dockOpen.workspaceOpen === true, "bench starts with the workspace dock open");
  assert(dockOpen.fromBottom <= 4, `dock-open check stays on the tail without scrolling up (${dockOpen.fromBottom})`);
  assert(dockOpen.overflowChatRight <= 1, `user bubble stays inside the chat column with the dock open (${dockOpen.overflowChatRight})`);
  assert(
    dockOpen.overflowDock == null || dockOpen.overflowDock <= 1,
    `user bubble does not extend into the workspace dock (${dockOpen.overflowDock})`,
  );

  // Width changes remasure Virtuoso and can leave a few pixels off the
  // physical bottom. Keep the crop assertions tight; only the post-resize
  // stick-to-tail check gets this slack (CI saw 7px after collapse).
  const tailAfterResizePx = 16;
  const waitNearTailAfterResize = () => page.waitForFunction((limit) => {
    const scroller = document.querySelector(".transcript");
    return Boolean(scroller && scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight <= limit);
  }, tailAfterResizePx);

  const collapse = page.getByRole("button", { name: /Collapse workspace|收起工作区/ });
  if (await collapse.count()) {
    await collapse.click();
    await page.waitForFunction(() => !document.querySelector(".layout")?.classList.contains("layout--workspace-open"));
    await waitNearTailAfterResize();
    const dockClosed = await measureDockCrop();
    assert(dockClosed.ok && dockClosed.workspaceOpen === false, "workspace toggle collapses the dock");
    assert(dockClosed.fromBottom <= tailAfterResizePx, `collapsing the dock does not require scrolling up (${dockClosed.fromBottom})`);
    assert(dockClosed.overflowChatRight <= 1, `user bubble stays inside the chat column with the dock closed (${dockClosed.overflowChatRight})`);
    const expand = page.getByRole("button", { name: /Expand workspace|展开工作区/ });
    await expand.click();
    await page.waitForFunction(() => Boolean(document.querySelector(".layout")?.classList.contains("layout--workspace-open")));
    await waitNearTailAfterResize();
    const dockReopen = await measureDockCrop();
    assert(dockReopen.ok && dockReopen.workspaceOpen === true, "workspace toggle reopens the dock");
    assert(dockReopen.fromBottom <= tailAfterResizePx, `reopening the dock stays on the tail (${dockReopen.fromBottom})`);
    assert(dockReopen.overflowChatRight <= 1, `user bubble stays inside the chat column after reopening the dock (${dockReopen.overflowChatRight})`);
    assert(
      dockReopen.overflowDock == null || dockReopen.overflowDock <= 1,
      `user bubble still does not enter the dock after reopen (${dockReopen.overflowDock})`,
    );
  }

  // Start away from either edge via a real upward gesture: user intent claims
  // manual mode, which forbids tail writes for the rest of the section. The
  // previous teleport-based setup left tail-follow armed, so the arbiter's
  // legitimate pull-back raced the trusted wheel's async landing and the
  // settle probe could fire on the pull-back instead of the gesture.
  await moveToOuterReaderGutter(page, transcript);
  await page.mouse.wheel(0, -1600);
  // Playwright resolves mouse.wheel before Chromium finishes the trusted
  // event's native default scroll. Let it land before sampling anchors.
  await page.waitForTimeout(120);
  await page.waitForFunction(() => document.querySelector(".transcript")?.dataset.scrollMode === "manual", undefined, { timeout: 5_000 });
  await transcript.evaluate((element) => new Promise((resolve) => {
    let previousTop = element.scrollTop;
    let stableFrames = 0;
    const sample = () => {
      const currentTop = element.scrollTop;
      stableFrames = Math.abs(currentTop - previousTop) <= 0.5 ? stableFrames + 1 : 0;
      previousTop = currentTop;
      if (stableFrames >= 2) { resolve(); return; }
      requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
  }));
  const beforeGrowth = await transcript.evaluate((element) => {
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

  const gestureStart = beforeGrowth.top;
  await transcript.evaluate((element, grownKey) => {
    const above = [...element.querySelectorAll(".transcript__row")]
      .find((row) => row.dataset.rowKey === grownKey);
    if (above instanceof HTMLElement) {
      above.dataset.growthReplayBasePadding = above.style.paddingBottom;
      above.style.paddingBottom = `${Number.parseFloat(above.style.paddingBottom || "0") + 1200}px`;
    }
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
  }, beforeGrowth.grownKey);
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  const afterGrowth = await transcript.evaluate((element) => ({
    top: element.scrollTop,
    samples: window.__reasonixScrollSamples ?? [],
  }));
  assert(afterGrowth.top >= gestureStart - 2, `dynamic measurement never reverses an upward gesture into a multi-screen jump (${gestureStart} → ${afterGrowth.top})`);
  assert(afterGrowth.samples.every((sample) => sample.occupied), "dynamic measurement never exposes a blank transcript viewport");
  await transcript.evaluate((element, grownKey) => {
    const grown = [...element.querySelectorAll(".transcript__row")]
      .find((row) => row.dataset.rowKey === grownKey);
    if (!(grown instanceof HTMLElement)) return;
    grown.style.paddingBottom = grown.dataset.growthReplayBasePadding || "";
    delete grown.dataset.growthReplayBasePadding;
  }, beforeGrowth.grownKey);
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));

  // Replay the returned 146-row Windows failure with real DOM geometry. A
  // downward wheel lands while the tail extent briefly loses 2,500px and regains
  // it 50ms later. Native clamping may move for the collapsed frame, but the
  // rebound must restore the user's logical direction without blank recovery.
  await transcript.evaluate((element) => {
    element.dataset.extentReplayOverflowAnchor = element.style.overflowAnchor;
    element.style.overflowAnchor = "none";
    element.scrollTop = Math.max(0, element.scrollHeight - element.clientHeight - 500);
    element.dispatchEvent(new Event("scroll"));
  });
  await page.waitForTimeout(100);
  await transcript.evaluate((element) => new Promise((resolve) => {
    const style = document.createElement("style");
    style.id = "reader-extent-replay-style";
    style.textContent = '.transcript[data-extent-replay-expanded="true"] > [data-viewport-type="element"]::after { content: ""; display: block; height: 2500px; }';
    document.head.append(style);
    element.dataset.extentReplayExpanded = "true";
    let stableFrames = 0;
    let previousHeight = element.scrollHeight;
    const settle = () => {
      const currentHeight = element.scrollHeight;
      stableFrames = Math.abs(currentHeight - previousHeight) <= 0.5 ? stableFrames + 1 : 0;
      previousHeight = currentHeight;
      if (stableFrames >= 4) {
        resolve();
        return;
      }
      requestAnimationFrame(settle);
    };
    requestAnimationFrame(settle);
  }));
  await page.waitForTimeout(100);
  await moveToOuterReaderGutter(page, transcript);
  const beforeExtentReplay = await transcript.evaluate((element) => {
    const viewport = element.getBoundingClientRect();
    const rows = [...element.querySelectorAll(".transcript__row")];
    const visible = rows
      .filter((row) => row.getBoundingClientRect().bottom > viewport.top && row.getBoundingClientRect().top < viewport.bottom)
      .sort((left, right) => left.getBoundingClientRect().top - right.getBoundingClientRect().top);
    const anchor = visible.find((row) => row.getBoundingClientRect().top >= viewport.top) ?? visible[0];
    window.__readerExtentProbe = { writes: [], samples: [], done: false };
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => window.__readerExtentProbe.writes.push(write);
    const sample = () => {
      const current = document.querySelector(".transcript");
      if (!(current instanceof HTMLElement)) return;
      const rect = current.getBoundingClientRect();
      const occupied = [...current.querySelectorAll(".transcript__row")].some((row) => {
        const rowRect = row.getBoundingClientRect();
        return rowRect.bottom > rect.top && rowRect.top < rect.bottom;
      });
      window.__readerExtentProbe.samples.push({ top: current.scrollTop, height: current.scrollHeight, occupied });
      if (!window.__readerExtentProbe.done) requestAnimationFrame(sample);
    };
    element.addEventListener("wheel", () => requestAnimationFrame(() => {
      delete element.dataset.extentReplayExpanded;
      const collapsedTop = Math.max(0, element.scrollTop - 2_000);
      element.scrollTop = collapsedTop;
      setTimeout(() => {
        element.dataset.extentReplayExpanded = "true";
        // Chromium restores its own scroll anchor on macOS. Keep the returned
        // WebView2 trace's clamped landing so this replay exercises our guard.
        element.scrollTop = collapsedTop;
      }, 50);
    }), { capture: true, once: true });
    requestAnimationFrame(sample);
    return {
      top: element.scrollTop,
      anchorKey: anchor?.dataset.rowKey ?? null,
      anchorOffset: anchor ? anchor.getBoundingClientRect().top - viewport.top : null,
    };
  });
  assert(beforeExtentReplay.anchorKey, "extent replay starts from a visible logical anchor");
  await page.mouse.wheel(0, 133);
  await page.waitForTimeout(260);
  const afterExtentReplay = await transcript.evaluate((element, before) => {
    window.__readerExtentProbe.done = true;
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = undefined;
    element.style.overflowAnchor = element.dataset.extentReplayOverflowAnchor || "";
    delete element.dataset.extentReplayOverflowAnchor;
    const anchor = [...element.querySelectorAll(".transcript__row")]
      .find((row) => row.dataset.rowKey === before.anchorKey);
    return {
      top: element.scrollTop,
      anchorOffset: anchor ? anchor.getBoundingClientRect().top - element.getBoundingClientRect().top : null,
      writes: window.__readerExtentProbe.writes,
      samples: window.__readerExtentProbe.samples,
      mode: element.dataset.scrollMode,
    };
  }, beforeExtentReplay);
  const readerStabilityWrites = afterExtentReplay.writes.filter((write) => write.owner === "reader-stability");
  const preservedDirection = afterExtentReplay.top >= beforeExtentReplay.top - 2;
  assert(preservedDirection, preservedDirection
    ? `transient extent rebound cannot reverse a downward wheel (${beforeExtentReplay.top} → ${afterExtentReplay.top})`
    : `transient extent rebound cannot reverse a downward wheel (${beforeExtentReplay.top} → ${afterExtentReplay.top}; anchor=${beforeExtentReplay.anchorOffset}→${afterExtentReplay.anchorOffset}; mode=${afterExtentReplay.mode}; writes=${JSON.stringify(afterExtentReplay.writes)}; samples=${JSON.stringify(afterExtentReplay.samples)})`);
  assert(readerStabilityWrites.length === 1, readerStabilityWrites.length === 1
    ? "transient extent rebound uses one bounded reader-stability write"
    : `transient extent rebound uses one bounded reader-stability write (${readerStabilityWrites.length}; ${JSON.stringify(afterExtentReplay.samples)})`);
  assert(afterExtentReplay.writes.every((write) => write.owner !== "recovery"),
    "nonblank extent rebound never enters blank recovery");
  assert(afterExtentReplay.samples.every((sample) => sample.occupied),
    "transient extent rebound keeps mounted coverage in every sampled frame");
  await transcript.evaluate((element) => {
    delete element.dataset.extentReplayExpanded;
    document.getElementById("reader-extent-replay-style")?.remove();
  });

  // Rapid direction changes are the exact user report. Sample every frame and
  // require that Virtuoso always maintains mounted coverage. Keep a real
  // Chromium long-task budget around 60 events so accidental synchronous
  // storage/layout work cannot return unnoticed.
  await page.evaluate(() => {
    const probe = {
      active: true,
      frameGaps: [],
      lastFrame: null,
      longTasks: [],
      observer: null,
    };
    if (PerformanceObserver.supportedEntryTypes?.includes("longtask")) {
      probe.observer = new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) probe.longTasks.push(entry.duration);
      });
      probe.observer.observe({ type: "longtask", buffered: false });
    }
    const sampleFrame = (now) => {
      if (!probe.active) return;
      if (probe.lastFrame != null) probe.frameGaps.push(now - probe.lastFrame);
      probe.lastFrame = now;
      requestAnimationFrame(sampleFrame);
    };
    window.__reasonixScrollPerfProbe = probe;
    requestAnimationFrame(sampleFrame);
  });
  const rapidDeltas = Array.from({ length: 10 }, () => [-700, -700, 480, -600, 520, -460]).flat();
  for (const delta of rapidDeltas) {
    await page.mouse.wheel(0, delta);
    await page.waitForTimeout(16);
  }
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  const perf = await page.evaluate(() => {
    const probe = window.__reasonixScrollPerfProbe;
    if (!probe) return null;
    probe.active = false;
    probe.observer?.disconnect();
    const gaps = [...probe.frameGaps].sort((left, right) => left - right);
    const percentileIndex = Math.max(0, Math.ceil(gaps.length * 0.95) - 1);
    return {
      frames: gaps.length,
      maxFrameGap: gaps.at(-1) ?? 0,
      p95FrameGap: gaps[percentileIndex] ?? 0,
      maxLongTask: Math.max(0, ...probe.longTasks),
      totalLongTask: probe.longTasks.reduce((sum, duration) => sum + duration, 0),
      longTasks: probe.longTasks.length,
    };
  });
  assert(perf && perf.frames >= 30, `rapid-scroll performance probe samples enough frames (${perf?.frames ?? 0})`);
  assert(perf.maxFrameGap <= maxFrameGapMs, `rapid-scroll maximum frame gap stays within budget (${perf.maxFrameGap.toFixed(1)}ms <= ${maxFrameGapMs}ms)`);
  assert(perf.p95FrameGap <= p95FrameGapMs, `rapid-scroll p95 frame gap stays within budget (${perf.p95FrameGap.toFixed(1)}ms <= ${p95FrameGapMs}ms)`);
  assert(perf.maxLongTask <= maxLongTaskMs, `rapid-scroll longest main-thread task stays within budget (${perf.maxLongTask.toFixed(1)}ms <= ${maxLongTaskMs}ms)`);
  assert(perf.totalLongTask <= totalLongTaskMs, `rapid-scroll total long-task time stays within budget (${perf.totalLongTask.toFixed(1)}ms <= ${totalLongTaskMs}ms; ${perf.longTasks} tasks)`);
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
  // Browser themes clamp the held thumb only after the pointer crosses the
  // track end, so the target below deliberately overshoots the visible gutter.
  await transcript.evaluate((element) => {
    element.scrollTop = 0;
    element.dispatchEvent(new Event("scroll"));
  });
  await page.waitForFunction(() => document.querySelector(".transcript__row"));
  let nativeThumbProbe = await transcript.evaluate(async (element) => {
    // The scroll-to-top event can leave a recycled Virtuoso node mounted or a
    // delayed layout owner can move the viewport again for a few frames.
    // Starting the drag in either state can hit the scrollbar track instead of
    // the top-positioned thumb, recycling the marked node before the geometry
    // freeze begins. Require both the physical top and the logical row identity
    // to settle before marking the probe.
    let stableFrames = 0;
    let previousIdentity = "";
    for (let frame = 0; frame < 120 && stableFrames < 4; frame += 1) {
      await new Promise((resolve) => requestAnimationFrame(resolve));
      if (element.scrollTop > 1) {
        element.scrollTop = 0;
        element.dispatchEvent(new Event("scroll"));
        stableFrames = 0;
        previousIdentity = "";
        continue;
      }
      const candidate = element.querySelector(".transcript__row");
      const identity = candidate instanceof HTMLElement
        ? `${candidate.dataset.rowKey ?? ""}:${candidate.dataset.knownSize ?? ""}`
        : "";
      stableFrames = identity && identity === previousIdentity ? stableFrames + 1 : 0;
      previousIdentity = identity;
    }
    const rect = element.getBoundingClientRect();
    const scaleX = rect.width / element.offsetWidth;
    const contentRight = rect.left + (element.clientLeft + element.clientWidth) * scaleX;
    const row = element.querySelector(".transcript__row");
    if (!(row instanceof HTMLElement)) return null;
    row.dataset.nativeScrollbarProbe = "true";
    return {
      x: Math.min(rect.right - 1, contentRight + Math.max(1, (rect.right - contentRight) / 2)),
      y: rect.top + 5,
      // Browser themes clamp the held thumb only after the pointer crosses
      // the track end, so the target deliberately overshoots the visible
      // gutter where a native thumb is available.
      bottomY: Math.min(window.innerHeight - 1, rect.bottom + Math.max(24, rect.height * 0.1)),
      scrollTop: element.scrollTop,
      rowKey: row.dataset.rowKey ?? "",
      knownSize: Number.parseFloat(row.dataset.knownSize || "0"),
      gutter: rect.right - contentRight,
      scrollHeight: element.scrollHeight,
    };
  });
  const nativeThumbDragSupported = process.platform !== "win32" || process.env.REASONIX_TRANSCRIPT_NATIVE_THUMB === "1";
  if (!nativeThumbDragSupported) {
    // Playwright's headless Chromium on Windows exposes the reserved gutter
    // width but does not expose a pointer-draggable native thumb. The actual
    // WebView2 path is covered by the Windows native smoke job; the geometry
    // lock itself is covered by transcript-native-scrollbar.test.ts.
    process.stdout.write("  SKIP  native thumb drag (headless Windows Chromium has no draggable native track)\n");
  } else if (process.platform === "darwin") {
    // Headless macOS Chromium does not expose a deterministic pointer-draggable
    // thumb: hosts with overlay scrollbars have no gutter, while hosts with a
    // reserved gutter accept pointer-down but do not reliably move the thumb.
    // Linux CI exercises the full drag/freeze path; Windows WebView2 coverage
    // is provided by the native smoke job.
    process.stdout.write("  SKIP  native thumb drag (headless macOS Chromium has no deterministic native track)\n");
  } else {
    assert(nativeThumbProbe && nativeThumbProbe.gutter > 1, `workbench exposes a native scrollbar gutter (${nativeThumbProbe?.gutter ?? 0}px)`);
    const trackTop = nativeThumbProbe.y - 5;
    const thumbCandidateOffsets = [4, 8, 12, 16, 20, 24, 28, 32];
    const thumbCandidateMotions = [];
    let nativeThumbY = null;
    for (const offset of thumbCandidateOffsets) {
      await transcript.evaluate((element) => {
        element.scrollTop = 0;
        element.dispatchEvent(new Event("scroll"));
      });
      await page.waitForFunction(() => (document.querySelector(".transcript")?.scrollTop ?? Number.POSITIVE_INFINITY) <= 1);
      const candidateY = trackTop + offset;
      await page.mouse.move(nativeThumbProbe.x, candidateY);
      await page.mouse.down();
      await page.waitForFunction(() => document.querySelector(".transcript")?.dataset.nativeScrollbarDrag === "true");
      await page.mouse.move(nativeThumbProbe.x, candidateY + 48, { steps: 2 });
      await page.waitForTimeout(150);
      const motion = await transcript.evaluate((element) => ({
        scrollTop: element.scrollTop,
        clientHeight: element.clientHeight,
      }));
      thumbCandidateMotions.push({ offset, scrollTop: motion.scrollTop });
      await page.mouse.up();
      await page.waitForFunction(() => document.querySelector(".transcript")?.dataset.nativeScrollbarDrag !== "true");
      // A 48px thumb move traverses several viewports in this fixture. A
      // track/button press moves at most one page and is not a valid drag hit.
      if (motion.scrollTop > motion.clientHeight * 2.5) {
        nativeThumbY = candidateY;
        break;
      }
    }
    assert(nativeThumbY !== null, `native scrollbar exposes a pointer-draggable thumb (${JSON.stringify(thumbCandidateMotions)})`);
    await transcript.evaluate((element) => {
      element.scrollTop = 0;
      element.dispatchEvent(new Event("scroll"));
    });
    nativeThumbProbe = await transcript.evaluate(async (element, input) => {
      let stableFrames = 0;
      let previousIdentity = "";
      for (let frame = 0; frame < 120 && stableFrames < 4; frame += 1) {
        await new Promise((resolve) => requestAnimationFrame(resolve));
        if (element.scrollTop > 1) {
          element.scrollTop = 0;
          element.dispatchEvent(new Event("scroll"));
          stableFrames = 0;
          previousIdentity = "";
          continue;
        }
        const candidate = element.querySelector(".transcript__row");
        const identity = candidate instanceof HTMLElement
          ? `${candidate.dataset.rowKey ?? ""}:${candidate.dataset.knownSize ?? ""}`
          : "";
        stableFrames = identity && identity === previousIdentity ? stableFrames + 1 : 0;
        previousIdentity = identity;
      }
      element.querySelectorAll('[data-native-scrollbar-probe="true"]').forEach((node) => {
        if (node instanceof HTMLElement) delete node.dataset.nativeScrollbarProbe;
      });
      const row = element.querySelector(".transcript__row");
      if (!(row instanceof HTMLElement)) return null;
      row.dataset.nativeScrollbarProbe = "true";
      return {
        ...input.probe,
        y: input.y,
        scrollTop: element.scrollTop,
        rowKey: row.dataset.rowKey ?? "",
        knownSize: Number.parseFloat(row.dataset.knownSize || "0"),
        scrollHeight: element.scrollHeight,
      };
    }, { probe: nativeThumbProbe, y: nativeThumbY });
    assert(nativeThumbProbe, "native scrollbar probe remains available after draggable-thumb discovery");
    assert(true, `native scrollbar exposes a pointer-draggable thumb (${Math.round(nativeThumbY - trackTop)}px from track start)`);
    assert(nativeThumbProbe.scrollTop <= 1, `native scrollbar probe starts at the physical top (${nativeThumbProbe.scrollTop}px)`);
    assert(nativeThumbProbe.knownSize > 0, `native scrollbar probe starts from a measured row (${nativeThumbProbe.knownSize}px)`);
    await page.mouse.move(nativeThumbProbe.x, nativeThumbProbe.y);
    await page.mouse.down();
    await page.waitForFunction(() => document.querySelector(".transcript")?.dataset.nativeScrollbarDrag === "true");
    const nativeDragStart = await transcript.evaluate((element) => {
      const row = element.querySelector('[data-native-scrollbar-probe="true"]');
      return {
        scrollTop: element.scrollTop,
        rowKey: row instanceof HTMLElement ? row.dataset.rowKey ?? "" : "",
      };
    });
    assert(nativeDragStart.scrollTop <= 1, `native thumb press keeps the viewport at the physical top (${nativeDragStart.scrollTop}px)`);
    assert(nativeDragStart.rowKey === nativeThumbProbe.rowKey, `native thumb press keeps the probed logical row (${nativeDragStart.rowKey || "missing"})`);
    await page.waitForFunction(({ knownSize, rowKey }) => {
      const row = document.querySelector('[data-native-scrollbar-probe="true"]');
      return row instanceof HTMLElement
        && row.dataset.rowKey === rowKey
        && row.style.height === `${knownSize}px`;
    }, { knownSize: nativeThumbProbe.knownSize, rowKey: nativeThumbProbe.rowKey });
    const nativeDragBaseline = await transcript.evaluate((element) => {
      const row = element.querySelector('[data-native-scrollbar-probe="true"]');
      return {
        knownSize: row instanceof HTMLElement ? Number.parseFloat(row.dataset.knownSize || "0") : 0,
        fixedHeight: row instanceof HTMLElement ? row.style.height : "",
        rowHeight: row instanceof HTMLElement ? row.getBoundingClientRect().height : 0,
        listHeight: element.querySelector('[data-testid="virtuoso-item-list"]')?.getBoundingClientRect().height ?? 0,
        scrollHeight: element.scrollHeight,
      };
    });
    assert(nativeDragBaseline?.knownSize > 0, `native thumb drag starts from a measured logical row (${nativeDragBaseline?.knownSize ?? 0}px)`);
    assert(nativeDragBaseline.fixedHeight === `${nativeDragBaseline.knownSize}px`, `native thumb drag fixes mounted row layout (${nativeDragBaseline.fixedHeight})`);
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
    assert(duringNativeThumbDrag.knownSize === nativeDragBaseline.knownSize, `native thumb drag freezes new row measurements (${duringNativeThumbDrag.knownSize}px)`);
    assert(duringNativeThumbDrag.fixedHeight === nativeDragBaseline.fixedHeight, `native thumb drag fixes mounted row layout (${duringNativeThumbDrag.fixedHeight})`);
    assert(Math.abs(duringNativeThumbDrag.scrollHeight - nativeDragBaseline.scrollHeight) <= 8, `native thumb drag keeps the physical scroll range stable (${nativeDragBaseline.scrollHeight} → ${duringNativeThumbDrag.scrollHeight}; row ${duringNativeThumbDrag.rowHeight}; list ${duringNativeThumbDrag.listHeight})`);
    await page.mouse.move(nativeThumbProbe.x, nativeThumbProbe.bottomY, { steps: 8 });
    await page.waitForFunction(() => {
      const element = document.querySelector(".transcript");
      return element && element.scrollHeight - element.scrollTop - element.clientHeight <= 1;
    });
    await page.mouse.up();
    await page.waitForFunction(() => document.querySelector(".transcript")?.dataset.nativeScrollbarDrag !== "true");
    await transcript.evaluate((element) => {
      const tail = [...element.querySelectorAll(".transcript__row")].at(-1);
      if (tail instanceof HTMLElement) tail.style.paddingBottom = `${Number.parseFloat(tail.style.paddingBottom || "0") + 900}px`;
    });
    assert(true, "native thumb release ends the measurement freeze");
    await page.waitForFunction(() => {
      const element = document.querySelector(".transcript");
      return element
        && element.dataset.scrollMode === "tail-follow"
        && element.scrollHeight - element.scrollTop - element.clientHeight <= 1;
    });
    assert(true, "native thumb release keeps the remeasured transcript at the physical bottom");
    const idleTail = await transcript.evaluate((element) => new Promise((resolve) => {
      const writes = [];
      const tops = [];
      let geometryStable = false;
      const stableWrites = [];
      let lastGeometry = null;
      window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => {
        writes.push(write);
        if (geometryStable) stableWrites.push(write);
      };
      let frames = 0;
      const sample = () => {
        tops.push(element.scrollTop);
        // Virtuoso can still be committing the +900px tail remeasure when the
        // window opens; a write against changing geometry is legitimate
        // convergence. Only writes after the geometry itself stops moving are
        // the oscillation this gate exists to catch.
        const geometry = `${element.scrollHeight}@${element.clientHeight}`;
        if (lastGeometry === null) lastGeometry = geometry;
        else if (geometry === lastGeometry && frames >= 10) geometryStable = true;
        else lastGeometry = geometry;
        frames += 1;
        if (frames < 30) {
          requestAnimationFrame(sample);
          return;
        }
        window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = undefined;
        resolve({ writes, stableWrites, tops, geometryStable });
      };
      requestAnimationFrame(sample);
    }));
    const idleTailRange = Math.max(...idleTail.tops) - Math.min(...idleTail.tops);
    assert(idleTail.geometryStable, "an idle expanded tail reaches stable geometry within the window");
    assert(idleTail.stableWrites.length === 0, `a stable idle tail emits no corrective scroll writes (${idleTail.stableWrites.length} of ${idleTail.writes.length}) ${JSON.stringify(idleTail.stableWrites)}`);
    assert(idleTailRange <= 1, `an idle expanded tail keeps stable native geometry (${idleTailRange}px)`);
  }

  // Explicit bottom owns the tail. Subsequent async growth must use Virtuoso's
  // autoscroll API and remain at the physical bottom without Reasonix scrollTop
  // correction loops.
  const jumpBottom = page.locator(".transcript__jump-bottom");
  // Re-measure and target the reserved row gutter inside the scrollport. The
  // native scrollbar gutter itself is browser-owned and can swallow this wheel
  // immediately after a thumb release.
  await moveToOuterReaderGutter(page, transcript);
  await page.mouse.wheel(0, -800);
  // Playwright resolves mouse.wheel before Chromium finishes the trusted
  // event's native default scroll. Let that input settle before sampling the
  // arbiter state or issuing another protocol command.
  await page.waitForTimeout(100);
  await page.waitForFunction(() => document.querySelector(".transcript")?.dataset.scrollMode === "manual");
  await jumpBottom.waitFor({ state: "visible" });
  await jumpBottom.click();
  await page.waitForFunction(() => {
    const element = document.querySelector(".transcript");
    return element
      && element.dataset.scrollMode === "tail-follow"
      && element.scrollHeight - element.scrollTop - element.clientHeight <= 1;
  });
  await transcript.evaluate((element) => new Promise((resolve) => {
    const growthFrames = new Set([2, 7, 12]);
    let frame = 0;
    const grow = () => {
      frame += 1;
      if (growthFrames.has(frame)) {
        const tail = [...element.querySelectorAll(".transcript__row")].at(-1);
        if (tail instanceof HTMLElement) {
          tail.style.paddingBottom = `${Number.parseFloat(tail.style.paddingBottom || "0") + 160}px`;
        }
      }
      if (frame < 14) requestAnimationFrame(grow);
      else resolve();
    };
    requestAnimationFrame(grow);
  }));
  await page.waitForFunction(() => {
    const element = document.querySelector(".transcript");
    return element && element.scrollHeight - element.scrollTop - element.clientHeight <= 1;
  });
  assert(true, "pinned multi-frame tail growth remains at the physical bottom");

  // ── #8657 residual: long session + measurement churn must still reach the
  // bottom. The v1.25.3 report: on very long sessions the user could never
  // scroll to the newest content — every approach was pulled back by
  // estimate-based recovery landings while ref-resolution patches kept
  // changing row heights. Reproduce the mechanics deterministically on the
  // 38-turn session: start mid-list in manual mode, churn heights of rows
  // above the viewport (async ref-resolution growth), and wheel downward
  // repeatedly. The user must reach the physical bottom, without a single
  // multi-screen upward snap and without any recovery-owned scroll write.
  await transcript.evaluate((element) => {
    element.scrollTop = Math.max(0, Math.floor((element.scrollHeight - element.clientHeight) / 2));
    element.dispatchEvent(new Event("scroll"));
  });
  await moveToOuterReaderGutter(page, transcript);
  await page.mouse.wheel(0, -240);
  await page.waitForTimeout(100);
  await page.waitForFunction(() => document.querySelector(".transcript")?.dataset.scrollMode === "manual", undefined, { timeout: 5_000 });
  await transcript.evaluate(() => {
    window.__reachBottomProbe = { writes: [], snaps: [], remounts: 0, done: false };
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => window.__reachBottomProbe.writes.push(write);
    // Re-query the scroller every frame: a remount replaces the element, and
    // sampling a detached node reads scrollTop 0.
    let scroller = document.querySelector(".transcript");
    let last = scroller?.scrollTop ?? 0;
    let displacedFrames = 0;
    const sample = () => {
      const element = document.querySelector(".transcript");
      if (element && element !== scroller) {
        window.__reachBottomProbe.remounts += 1;
        scroller = element;
        last = element.scrollTop;
      }
      if (!element) {
        if (!window.__reachBottomProbe.done) requestAnimationFrame(sample);
        return;
      }
      const top = element.scrollTop;
      // Only a displacement persisting 5+ frames is a pull-back; a one-frame
      // remount flash recovers by design (#8657).
      if (last - top > element.clientHeight * 2) displacedFrames += 1;
      else displacedFrames = 0;
      if (displacedFrames === 5) {
        window.__reachBottomProbe.snaps.push({ stuckAt: Math.round(top), droppedFrom: Math.round(last) });
      }
      last = top;
      if (!window.__reachBottomProbe.done) requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
    // Measurement churn: grow a random mounted row above the viewport every
    // ~90 ms, mimicking ref-resolution patches landing during the gesture.
    const churn = () => {
      if (window.__reachBottomProbe.done) return;
      const current = document.querySelector(".transcript");
      if (!(current instanceof HTMLElement)) {
        setTimeout(churn, 90);
        return;
      }
      const viewport = current.getBoundingClientRect();
      const above = [...current.querySelectorAll(".transcript__row")].filter((row) => row.getBoundingClientRect().bottom <= viewport.top);
      const row = above[Math.floor(Math.random() * above.length)];
      if (row instanceof HTMLElement) {
        row.style.paddingBottom = `${Number.parseFloat(row.style.paddingBottom || "0") + 160}px`;
      }
      setTimeout(churn, 90);
    };
    churn();
  });
  let reachedBottom = false;
  for (let attempt = 0; attempt < 40 && !reachedBottom; attempt += 1) {
    await page.mouse.wheel(0, 640);
    await page.waitForTimeout(50);
    reachedBottom = await transcript.evaluate((element) =>
      element.dataset.scrollMode === "tail-follow"
      && element.scrollHeight - element.scrollTop - element.clientHeight <= 1);
  }
  const reachState = reachedBottom ? null : await transcript.evaluate((element) => ({
    mode: element.dataset.scrollMode,
    distance: element.scrollHeight - element.scrollTop - element.clientHeight,
    top: element.scrollTop,
    height: element.scrollHeight,
    clientHeight: element.clientHeight,
    writes: window.__reachBottomProbe.writes.slice(-8),
  }));
  assert(reachedBottom, `repeated downward wheels reach the physical bottom through measurement churn (#8657)${reachState ? `: ${JSON.stringify(reachState)}` : ""}`);
  await page.waitForFunction(() => document.querySelector(".transcript")?.dataset.scrollMode === "tail-follow", undefined, { timeout: 5_000 });
  const reachProbe = await transcript.evaluate(() => {
    window.__reachBottomProbe.done = true;
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = undefined;
    return window.__reachBottomProbe;
  });
  assert(reachProbe.snaps.length === 0, `no persistent multi-screen pull-back while wheeling down (${JSON.stringify(reachProbe.snaps.slice(0, 3))}; ${reachProbe.remounts} remount(s))`);
  assert(
    reachProbe.writes.every((write) => write.owner !== "recovery"),
    `zero recovery-owned scroll writes during the reach-bottom gesture (${reachProbe.writes.length} writes)`,
  );
  // The tail holds while churn continues underneath the pinned view.
  await page.evaluate(() => new Promise((resolve) => setTimeout(resolve, 400)));
  const tailAfterChurn = await transcript.evaluate((element) => element.scrollHeight - element.scrollTop - element.clientHeight);
  assert(tailAfterChurn <= 1, `tail-follow holds at the newest content while churn continues (${tailAfterChurn}px)`);

  // ── #8657 end-to-end: a real ref-resolution patch storm on a 40-turn
  // session. Opening bench:storm-40t resolves ~24 ref-replaced fields at a
  // paced interval (~3s of history_items_patch invalidations). The pre-fix
  // chain turned every patch into a keyed remount at scroll idle, collapsing
  // the measured size tree back to estimates and stranding the view several
  // screens up — the user could never reach the bottom. Drive repeated
  // downward wheels WITH periodic pauses (each pause is a scroll idle, the
  // exact moment the old chain remounted) and require: bottom reached, no
  // multi-screen upward snap, zero recovery-owned writes, tail holds.
  await page.click('.project-tree__topic-main:has-text("bench:storm-40t")');
  await page.waitForFunction(
    () => document.querySelector(".project-tree__topic--active .project-tree__topic-label")?.textContent?.includes("bench:storm-40t"),
    undefined,
    { timeout: 30_000 },
  );
  await page.waitForFunction(
    () => document.querySelector(".transcript")?.textContent?.includes("storm turn 40"),
    undefined,
    { timeout: 30_000 },
  );
  await page.waitForFunction(() => document.querySelectorAll(".transcript__row").length > 2, undefined, { timeout: 30_000 });
  await page.evaluate(() => new Promise((resolve) => {
    let frames = 6;
    const settle = () => frames-- <= 0 ? resolve() : requestAnimationFrame(settle);
    requestAnimationFrame(settle);
  }));
  const stormTranscript = page.locator(".transcript");
  const stormBox = await stormTranscript.boundingBox();
  assert(stormBox != null, "storm session exposes the transcript viewport");
  await stormTranscript.evaluate((element) => {
    element.scrollTop = Math.max(0, Math.floor((element.scrollHeight - element.clientHeight) / 2));
    element.dispatchEvent(new Event("scroll"));
  });
  // Use reserved row padding, not a nested tool/code scroller or the
  // browser-owned native scrollbar gutter, for explicit reader ownership.
  await moveToOuterReaderGutter(page, stormTranscript);
  await page.mouse.wheel(0, -240);
  await page.waitForTimeout(100);
  await page.waitForFunction(() => document.querySelector(".transcript")?.dataset.scrollMode === "manual", undefined, { timeout: 5_000 });
  await stormTranscript.evaluate(() => {
    window.__stormProbe = { writes: [], snaps: [], remounts: 0, done: false };
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => window.__stormProbe.writes.push(write);
    // Re-query the scroller every frame: a remount (blank-watchdog rebuild)
    // replaces the element, and sampling a detached node reads scrollTop 0.
    let scroller = document.querySelector(".transcript");
    let last = scroller?.scrollTop ?? 0;
    let displacedFrames = 0;
    const sample = () => {
      const element = document.querySelector(".transcript");
      if (element && element !== scroller) {
        window.__stormProbe.remounts += 1;
        scroller = element;
        last = element.scrollTop;
      }
      if (!element) {
        if (!window.__stormProbe.done) requestAnimationFrame(sample);
        return;
      }
      const top = element.scrollTop;
      // The product contract is "never LEFT several screens above": a
      // one-frame flash during a genuine watchdog remount recovers; only a
      // displacement that persists for 5+ frames is a pull-back (#8657).
      if (last - top > element.clientHeight * 2) displacedFrames += 1;
      else displacedFrames = 0;
      if (displacedFrames === 5) {
        window.__stormProbe.snaps.push({ stuckAt: Math.round(top), droppedFrom: Math.round(last) });
      }
      last = top;
      if (!window.__stormProbe.done) requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
  });
  let stormReached = false;
  for (let attempt = 0; attempt < 60 && !stormReached; attempt += 1) {
    await page.mouse.wheel(0, 640);
    await page.waitForTimeout(60);
    // Every sixth gesture, pause into scroll idle — the moment the pre-fix
    // chain fired its revision-driven remount mid-approach.
    if (attempt % 6 === 5) await page.waitForTimeout(500);
    stormReached = await stormTranscript.evaluate((element) =>
      element.dataset.scrollMode === "tail-follow"
      && element.scrollHeight - element.scrollTop - element.clientHeight <= 1);
  }
  assert(stormReached, "repeated downward wheels reach the physical bottom through the ref-resolution storm (#8657)");
  await page.waitForFunction(() => document.querySelector(".transcript")?.dataset.scrollMode === "tail-follow", undefined, { timeout: 5_000 });
  // The storm keeps resolving after the user lands; the tail must hold.
  await page.waitForFunction(
    () => document.querySelector(".transcript")?.textContent?.includes("storm-40-FINAL"),
    undefined,
    { timeout: 30_000 },
  );
  await page.waitForTimeout(600);
  const stormTail = await stormTranscript.evaluate((element) => element.scrollHeight - element.scrollTop - element.clientHeight);
  assert(stormTail <= 1, `tail-follow holds at the newest content until the storm fully resolves (${stormTail}px)`);
  const stormProbe = await stormTranscript.evaluate(() => {
    window.__stormProbe.done = true;
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = undefined;
    return window.__stormProbe;
  });
  assert(
    stormProbe.snaps.length === 0,
    `no persistent multi-screen pull-back during the storm approach (${JSON.stringify(stormProbe.snaps.slice(0, 3))}; ${stormProbe.remounts} watchdog remount(s))`,
  );
  assert(
    stormProbe.writes.every((write) => write.owner !== "recovery"),
    `zero recovery-owned scroll writes through the storm (${stormProbe.writes.length} writes)`,
  );

  // ── Reduced-motion tail stability (#9028/#9089). Windows reports
  // prefers-reduced-motion: reduce whenever "Animate controls and elements
  // inside windows" is off — the default on many machines. The global CSS
  // reset then collapses every transition to ~0ms, layout churn lands one
  // whole step per frame, and 1.28.0's tail writer chased each step through
  // scrollToIndex against a stale size tree: a sustained per-frame flicker
  // loop with the view never reaching the bottom (#9028 captured 340 no-op
  // tail writes in 11s). The tail writer must stay calm through churn and
  // still land on the physical bottom, with no OS setting involved.
  const reducedPage = await browser.newPage({ viewport: { width: 1280, height: 800 }, reducedMotion: "reduce" });
  await reducedPage.addInitScript(() => localStorage.setItem("reasonix-process-fold", "expanded"));
  await reducedPage.goto(url, { waitUntil: "domcontentloaded" });
  await reducedPage.waitForFunction(() => !document.querySelector(".startup-splash"), undefined, { timeout: 30_000 });
  assert(
    await reducedPage.evaluate(() => matchMedia("(prefers-reduced-motion: reduce)").matches),
    "reduced-motion emulation is active",
  );
  await reducedPage.click('.project-tree__topic-main:has-text("bench:reported-long-turn")');
  await reducedPage.waitForFunction(
    () => document.querySelector(".transcript")?.textContent?.includes("Reported long turn complete."),
    undefined,
    { timeout: 30_000 },
  );
  // Let the open-time hydration burst converge, then require several stable
  // frames before watching the idle tail. A single bottom sample can race a
  // late markdown/row remeasurement on loaded CI runners and contaminate the
  // idle probe with legitimate convergence motion.
  await waitForStableTranscriptGeometry(reducedPage, { timeout: 30_000, requireTail: true });
  const reducedIdle = await reducedPage.evaluate(() => new Promise((resolve) => {
    const element = document.querySelector(".transcript");
    const writes = [];
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = (write) => writes.push(write);
    const tops = [];
    let frames = 0;
    const sample = () => {
      if (element instanceof HTMLElement) tops.push(element.scrollTop);
      frames += 1;
      if (frames < 180) { requestAnimationFrame(sample); return; }
      window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = undefined;
      let reversals = 0;
      let lastDirection = 0;
      for (let i = 1; i < tops.length; i += 1) {
        const delta = tops[i] - tops[i - 1];
        if (Math.abs(delta) <= 2) continue;
        const direction = Math.sign(delta);
        if (lastDirection !== 0 && direction !== lastDirection) reversals += 1;
        lastDirection = direction;
      }
      const live = document.querySelector(".transcript");
      resolve({
        reversals,
        tailWrites: writes.filter((write) => write.owner === "tail-follow").length,
        finalDistance: live instanceof HTMLElement
          ? live.scrollHeight - live.scrollTop - live.clientHeight
          : Number.NaN,
      });
    };
    requestAnimationFrame(sample);
  }));
  assert(
    reducedIdle.reversals <= 2,
    `reduced-motion idle tail does not ping-pong (${reducedIdle.reversals} direction reversals in 180 frames; ${reducedIdle.tailWrites} writes; ${reducedIdle.finalDistance}px from bottom)`,
  );
  // The #9028 pathology was a sustained loop: ~340 tail writes in 11s (about
  // 2 writes every 3 frames, forever). Late fixture hydration can still land
  // a legitimate convergence burst inside the window, so gate on an order of
  // magnitude below the pathology instead of near-zero.
  assert(
    reducedIdle.tailWrites < 30,
    `reduced-motion idle tail never enters a sustained write loop (${reducedIdle.tailWrites} tail writes in 180 frames)`,
  );
  assert(
    reducedIdle.finalDistance <= 4,
    `reduced-motion tail rests on the physical bottom (${reducedIdle.finalDistance}px)`,
  );
  // The #9089 gesture: wheel up, wheel back to the bottom, release, idle.
  const reducedTranscript = reducedPage.locator(".transcript");
  const reducedBottomTop = await reducedTranscript.evaluate((element) => element.scrollTop);
  await moveToOuterReaderGutter(reducedPage, reducedTranscript);
  await reducedPage.mouse.wheel(0, -900);
  await reducedPage.waitForFunction((bottomTop) => {
    const element = document.querySelector(".transcript");
    return element instanceof HTMLElement
      && element.dataset.scrollMode === "manual"
      && element.scrollTop < bottomTop - 1
      && element.scrollHeight - element.scrollTop - element.clientHeight > 4;
  }, reducedBottomTop, { timeout: 5_000 });
  let reducedReachedBottom = false;
  for (let attempt = 0; attempt < 20 && !reducedReachedBottom; attempt += 1) {
    await reducedPage.mouse.wheel(0, 640);
    await reducedPage.waitForTimeout(50);
    reducedReachedBottom = await reducedTranscript.evaluate((element) =>
      element.dataset.scrollMode === "tail-follow"
      && element.scrollHeight - element.scrollTop - element.clientHeight <= 4);
  }
  assert(reducedReachedBottom, "reduced-motion repeated downward wheels return to the physical bottom (#9089)");
  const reducedReturn = await reducedPage.evaluate(() => new Promise((resolve) => {
    const element = document.querySelector(".transcript");
    const tops = [];
    let frames = 0;
    const sample = () => {
      if (element instanceof HTMLElement) tops.push(element.scrollTop);
      frames += 1;
      if (frames < 120) { requestAnimationFrame(sample); return; }
      let movingFrames = 0;
      let reversals = 0;
      let lastDirection = 0;
      for (let i = 1; i < tops.length; i += 1) {
        const delta = tops[i] - tops[i - 1];
        if (Math.abs(delta) <= 2) continue;
        movingFrames += 1;
        const direction = Math.sign(delta);
        if (lastDirection !== 0 && direction !== lastDirection) reversals += 1;
        lastDirection = direction;
      }
      const live = document.querySelector(".transcript");
      resolve({
        movingFrames,
        reversals,
        finalDistance: live instanceof HTMLElement
          ? live.scrollHeight - live.scrollTop - live.clientHeight
          : Number.NaN,
      });
    };
    requestAnimationFrame(sample);
  }));
  assert(
    reducedReturn.reversals <= 2,
    `reduced-motion return-to-bottom does not ping-pong (${reducedReturn.reversals} reversals in 120 frames)`,
  );
  assert(
    reducedReturn.movingFrames < 12,
    `reduced-motion return-to-bottom idles without sustained self-scrolling (${reducedReturn.movingFrames} moving frames in 120)`,
  );
  assert(
    reducedReturn.finalDistance <= 4,
    `reduced-motion return-to-bottom rests on the physical bottom (${reducedReturn.finalDistance}px)`,
  );
  await reducedPage.close();

  process.stdout.write("\ntranscript scroll stability browser gate passed\n");
} finally {
  await browser?.close();
  preview.kill("SIGTERM");
}
