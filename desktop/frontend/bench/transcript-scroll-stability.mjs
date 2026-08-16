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
  await page.evaluate(() => {
    const element = document.querySelector(".transcript");
    if (!(element instanceof HTMLElement)) return;
    element.scrollTop = element.scrollHeight;
    element.dispatchEvent(new Event("scroll"));
  });
  await page.waitForFunction(() => {
    const element = document.querySelector(".transcript");
    return element instanceof HTMLElement
      && element.dataset.scrollMode === "tail-follow"
      && element.scrollHeight - element.scrollTop - element.clientHeight <= 1;
  });
  await page.click('.project-tree__topic-main:has-text("bench:tools-38t")');
  await page.waitForFunction(() => document.querySelector(".project-tree__topic--active .project-tree__topic-label")?.textContent?.includes("bench:tools-38t"));
  await page.waitForFunction(() => document.querySelector(".transcript")?.textContent?.includes("pkg-41/mod.go"), undefined, { timeout: 30_000 });
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
  const dockOpen = await measureDockCrop();
  assert(dockOpen.ok, "tail-follow dock check can see the chat and a user bubble");
  assert(dockOpen.workspaceOpen === true, "bench starts with the workspace dock open");
  assert(dockOpen.fromBottom <= 1, `dock-open check stays on the tail without scrolling up (${dockOpen.fromBottom})`);
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
  if (process.platform === "darwin" && (!nativeThumbProbe || nativeThumbProbe.gutter <= 1)) {
    // macOS Chromium inherits system overlay scrollbars, so there is no
    // pointer-addressable native gutter. Linux/Windows CI still exercises the
    // complete thumb ownership and measurement-freeze path below.
    process.stdout.write("  SKIP  native thumb drag (host uses overlay scrollbars)\n");
  } else {
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
  }

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
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.wheel(0, -240);
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
    reachedBottom = await transcript.evaluate((element) => element.scrollHeight - element.scrollTop - element.clientHeight <= 1);
  }
  assert(reachedBottom, "repeated downward wheels reach the physical bottom through measurement churn (#8657)");
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
  await page.mouse.move(stormBox.x + stormBox.width / 2, stormBox.y + stormBox.height / 2);
  await page.mouse.wheel(0, -240);
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
    stormReached = await stormTranscript.evaluate((element) => element.scrollHeight - element.scrollTop - element.clientHeight <= 1);
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

  process.stdout.write("\ntranscript scroll stability browser gate passed\n");
} finally {
  await browser?.close();
  preview.kill("SIGTERM");
}
