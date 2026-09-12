import assert from "node:assert/strict";
import { mkdir, mkdtemp, writeFile, copyFile, rm } from "node:fs/promises";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { build, preview, loadConfigFromFile } from "vite";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
process.env.PLAYWRIGHT_BROWSERS_PATH = !process.env.PLAYWRIGHT_BROWSERS_PATH || process.env.PLAYWRIGHT_BROWSERS_PATH === ".pw-browsers"
  ? path.join(root, ".pw-browsers")
  : process.env.PLAYWRIGHT_BROWSERS_PATH;
// Playwright reads PLAYWRIGHT_BROWSERS_PATH at module evaluation.
const { chromium, webkit, _electron } = await import("playwright");
const outDir = await mkdtemp(path.join(tmpdir(), "reasonix-chat-build-"));
const evidence = process.env.REASONIX_CHAT_EVIDENCE ?? process.env.REASONIX_LAYOUT_ARTIFACTS ?? path.join(tmpdir(), "reasonix-chat-evidence");
await mkdir(evidence, { recursive: true });
const loaded = await loadConfigFromFile({ command: "build", mode: "production" }, path.join(root, "vite.config.ts"));
const config = loaded.config;
await build({ ...config, configFile: false, root, logLevel: "error",
  plugins: config.plugins.filter(plugin => !["archive-hidden-sourcemaps", "keep-dist-placeholder"].includes(plugin?.name)),
  build: { ...config.build, outDir, sourcemap: false,
    rolldownOptions: { ...config.build.rolldownOptions, input: path.join(root, "bench/chat-transcript.html") } } });
const server = await preview({ configFile: false, root, logLevel: "error", build: { outDir }, preview: { host: "127.0.0.1", port: 0 } });
const address = server.httpServer.address();
const reports = [];
try {
  for (const [name, engine] of Object.entries(process.env.CHAT_BROWSER === "electron" ? { electron: _electron } : process.env.CHAT_BROWSER === "webkit" ? { webkit } : process.env.CHAT_BROWSER === "chromium" ? { chromium } : { chromium, webkit })) {
    let electronApp;
    const url = `http://127.0.0.1:${address.port}/bench/chat-transcript.html?mock=1`;
    if (name === "electron") {
      const main = path.join(outDir, "main.cjs");
      await copyFile(path.join(root, "bench/transcript-layout-electron.cjs"), main);
      electronApp = await engine.launch({ executablePath: createRequire(path.join(root, "../electron/package.json"))("electron"), args: [main], env: { ...process.env, REASONIX_LAYOUT_URL: url } });
    }
    const nativeThumb = process.env.REASONIX_TRANSCRIPT_NATIVE_THUMB === "1";
    const browser = electronApp ? undefined : await engine.launch({ headless: !nativeThumb });
    const report = { browser: name, complete: false, samples: [], errors: [],
      version: browser?.version() ?? await electronApp.evaluate(() => process.versions.electron),
      platform: process.platform, arch: process.arch };
    reports.push(report);
    try {
      const page = electronApp ? await electronApp.firstWindow() : await browser.newPage({ viewport: { width: 1280, height: 900 } });
      const errors = report.errors;
      await page.addInitScript(() => { window.chatWrites = []; window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = write => { window.chatWrites.push(write); if (window.chatWrites.length > 100) window.chatWrites.shift(); }; });
      await page.addInitScript(() => {
        window.chatMetrics = { inputs: [], tasks: [], longTaskSupported: PerformanceObserver.supportedEntryTypes.includes("longtask") };
        if (PerformanceObserver.supportedEntryTypes.includes("longtask")) new PerformanceObserver(list => window.chatMetrics.tasks.push(...list.getEntries().map(entry => entry.duration))).observe({ type: "longtask" });
        document.addEventListener("keydown", event => {
          if (!event.target.matches("textarea.composer__input")) return;
          const start = performance.now(); requestAnimationFrame(() => requestAnimationFrame(() => window.chatMetrics.inputs.push(performance.now() - start)));
        }, true);
      });
      page.on("pageerror", error => { errors.push(error.message); console.error(error.stack); });
      page.on("console", message => { if (/Maximum update depth|ResizeObserver loop/.test(message.text())) errors.push(message.text()); });
      await page.goto(url);
      await page.locator(".chat-column .md h3").last().waitFor();
      await page.evaluate(() => document.fonts.ready);
      const scroll = page.locator(".chat-flow-scroll");
      const frame = () => page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      const navigate = async key => {
        const mark = page.locator(`[data-nav-turn="${key}"]`);
        await mark.focus(); await mark.press("Enter");
      };
      const bottom = () => scroll.evaluate(el => el.scrollHeight - el.scrollTop - el.clientHeight);
      await frame();
      await page.waitForFunction(() => { const el = document.querySelector(".chat-flow-scroll"); return el.scrollHeight - el.scrollTop - el.clientHeight <= 24; }, null, { timeout: 5000 });
      assert.ok(await bottom() <= 24, "initially follows latest");
      const rail = page.locator('.dsh-TurnNavigator-frame');
      await rail.waitFor();
      assert.equal(await page.locator('[data-nav-turn]').count(), 20, 'rail lists loaded turns only');
      const activeMark = page.locator('[data-nav-turn][aria-current="true"]');
      assert.equal(await activeMark.evaluate(el => getComputedStyle(el, '::before').backgroundColor),
        await page.locator('.chat-surface').evaluate(el => {
          const probe = document.createElement('span');
          probe.style.backgroundColor = 'var(--accent)'; el.append(probe);
          const value = getComputedStyle(probe).backgroundColor; probe.remove(); return value;
        }), 'active rail mark uses the v1.38.7 accent');
      const railBox = await rail.boundingBox();
      await page.mouse.move(railBox.x + 18, railBox.y + 196);
      await page.locator('.dsh-TurnNavigator-preview').waitFor();
      assert.match(await page.locator('.dsh-TurnNavigator-preview').innerText(), /Question 20/);
      assert.equal(await page.locator('[data-nav-turn="u19"]').getAttribute('data-preview-distance'), '0', 'hovered mark owns the accent');
      assert.equal(await page.locator('[data-nav-turn="u18"]').getAttribute('data-preview-distance'), '1', 'adjacent mark uses the first v1.38.7 fade');
      assert.equal(await page.locator('[data-nav-turn="u17"]').getAttribute('data-preview-distance'), '2', 'second adjacent mark uses the second v1.38.7 fade');
      await page.screenshot({ path: path.join(evidence, `${name}-turn-navigation.png`) });
      await page.mouse.click(railBox.x + 18, railBox.y + 196);
      await frame();
      assert.equal(await scroll.getAttribute('data-scroll-mode'), 'reader', 'rail click enters reading intent');
      await navigate('u0'); await frame();
      assert.equal(await page.locator('[data-nav-turn="u0"]').getAttribute('aria-current'), 'true', 'keyboard navigation updates the active tick');
      await page.locator('.chat-to-bottom').click(); await frame();
      if (nativeThumb) {
        await scroll.evaluate(el => {
          window.chatThumbEvents = [];
          for (const type of ["pointerdown", "pointerup", "scroll"]) el.addEventListener(type, event => window.chatThumbEvents.push({ type, top: el.scrollTop, target: event.target === el }));
        });
        const track = await scroll.evaluate(el => { const box = el.getBoundingClientRect();
          const gutter = el.offsetWidth - el.clientWidth;
          const arrow = gutter;
          const trackHeight = Math.max(1, box.height - 2 * arrow);
          const thumbHeight = Math.max(gutter, trackHeight * el.clientHeight / el.scrollHeight);
          return {
            x: box.right - gutter / 2,
            top: box.top,
            bottom: box.bottom,
            before: el.scrollTop,
            gutter,
            thumbHeight,
            thumbCenter: box.bottom - arrow - thumbHeight / 2,
          }; });
        report.nativeThumb = { track };
        // This coordinate-based variant requires an exposed native gutter (for
        // example headed Linux/Xvfb). A hidden macOS overlay can select text
        // instead; that must never count as a successful scrollbar drag.
        assert.ok(track.gutter > 0, "native scrollbar gutter unavailable; run the native-thumb variant in an isolated host with visible scrollbars");
        // Chromium's Linux scrollbar reserves an arrow-button-sized region at
        // each end. Start in the computed thumb center instead of the bottom
        // arrow, then drag toward the middle of the track.
        await page.mouse.move(track.x, track.thumbCenter); await page.mouse.down();
        await page.mouse.move(track.x, (track.top + track.bottom) / 2, { steps: 20 }); await page.mouse.up();
        report.nativeThumb = { track, after: await scroll.evaluate(el => ({ top: el.scrollTop, mode: el.dataset.scrollMode, events: window.chatThumbEvents })) };
        await page.screenshot({ path: path.join(evidence, `${name}-native-thumb.png`) });
        await page.waitForFunction(before => document.querySelector(".chat-flow-scroll").scrollTop < before - 100, track.before);
        assert.equal(await scroll.getAttribute("data-scroll-mode"), "reader", "native scrollbar dragging releases follow");
        await page.locator(".chat-to-bottom").click(); await frame();
      }
      await scroll.hover();
      const beforeWheel = await scroll.evaluate(el => el.scrollTop);
      await page.mouse.wheel(0, -600);
      await page.waitForFunction(() => !document.querySelector(".chat-to-bottom").hidden);
      // WebKit dispatches wheel input before its native scroll animation ends.
      // Measure content-induced drift only after that user movement settles.
      await page.waitForFunction(before => document.querySelector(".chat-flow-scroll").scrollTop < before - 1, beforeWheel);
      await scroll.evaluate(el => new Promise(resolve => {
        let previous = el.scrollTop, stable = 0;
        const sample = () => {
          const current = el.scrollTop;
          stable = Math.abs(current - previous) < 0.1 ? stable + 1 : 0;
          previous = current;
          if (stable >= 6) resolve(); else requestAnimationFrame(sample);
        };
        requestAnimationFrame(sample);
      }));
      await frame();
      const anchor = await page.evaluate(() => {
        const el = document.querySelector(".chat-flow-scroll"), top = el.getBoundingClientRect().top;
        const row = [...document.querySelectorAll("[data-chat-anchor-key]")].find(row => row.childNodes.length && row.getBoundingClientRect().bottom > top + 1);
        return { key: row.dataset.chatAnchorKey, top: row.getBoundingClientRect().top };
      });
      for (let i = 0; i < 60; i++) { await page.evaluate(i => window.chatFixture.tick(i), i); await frame(); }
      const topAfter = await page.locator(`[data-chat-anchor-key="${anchor.key}"]`).evaluate(el => el.getBoundingClientRect().top);
      assert.ok(Math.abs(topAfter - anchor.top) <= 2, `stream anchor drift ${topAfter - anchor.top}`);
      await page.evaluate(() => window.chatFixture.prepend()); await frame();
      const topPrepended = await page.locator(`[data-chat-anchor-key="${anchor.key}"]`).evaluate(el => el.getBoundingClientRect().top);
      assert.ok(Math.abs(topPrepended - anchor.top) <= 2, `prepend anchor drift ${topPrepended - anchor.top}`);
      await page.locator(".chat-to-bottom").click(); await frame();
      await page.waitForFunction(() => { const el = document.querySelector(".chat-flow-scroll"); return el.scrollHeight - el.scrollTop - el.clientHeight <= 24; }, null, { timeout: 5000 });
      assert.ok(await bottom() <= 24, "return to latest resumes follow");
      await page.locator('[data-chat-anchor-key="a19"] .md p').first().waitFor();
      const selected = await page.locator('[data-chat-anchor-key="a19"] .md p').first().evaluate(el => {
        window.selectedChatParagraph = el;
        const range = document.createRange(); range.selectNodeContents(el);
        getSelection().removeAllRanges(); getSelection().addRange(range); return getSelection().toString();
      });
      await page.evaluate(() => window.chatFixture.settle()); await frame();
      assert.ok(await page.evaluate(() => window.selectedChatParagraph.isConnected), "settlement retains the selected paragraph host");
      assert.equal(await page.evaluate(() => getSelection().toString()), selected, "native selection survives stream settlement");
      await navigate("u19"); await frame();
      await page.locator('[data-chat-kind="process"] button').last().click();
      await page.locator(".chat-tool [data-disclosure-row]").last().click();
      await page.locator(".dsh-ToolRow-inspectButton").last().click();
      await page.locator('[role="dialog"]').waitFor();
      await page.screenshot({ path: path.join(evidence, `${name}-details.png`) });
      await page.keyboard.press("Escape");
      assert.equal(await page.locator('[role="dialog"]').count(), 0);
      assert.ok(await page.locator(".dsh-ToolRow-inspectButton").last().evaluate(el => el === document.activeElement), "drawer restores trigger focus after removing inert");
      const samples = report.samples;
      const input = page.locator("textarea.composer__input:not(.composer__input--measure)");
      const percentile = values => [...values].sort((a, b) => a - b)[Math.max(0, Math.ceil(values.length * 0.95) - 1)] ?? 0;
      for (const turns of [240, 1000]) {
        const started = Date.now();
        await page.evaluate(count => { window.chatMetrics.inputs = []; window.chatMetrics.tasks = []; window.chatFixture.reset(count); }, turns);
        await page.waitForFunction(() => document.querySelectorAll('[data-chat-kind="user"]').length === 60);
        const pages = [];
        for (let loaded = 60; loaded < turns; loaded += 60) {
          const pageStart = Date.now();
          await page.evaluate(() => window.chatFixture.older());
          await page.waitForFunction(count => document.querySelectorAll('[data-chat-kind="user"]').length === count, Math.min(turns, loaded + 60));
          await frame(); pages.push(Date.now() - pageStart);
        }
        await frame();
        const mountedMs = Date.now() - started;
        assert.equal(await page.locator(".transcript__window-item").count(), 0);
        await input.fill("");
        await page.evaluate(() => window.chatFixture.tick(0));
        for (let i = 0; i < 30; i++) { await page.evaluate(i => window.chatFixture.tick(i), i); await input.press("a"); }
        await frame();
        assert.equal(await input.inputValue(), "a".repeat(30));
        const metrics = await page.evaluate(() => window.chatMetrics);
        assert.ok(metrics.inputs.length >= 30);
        const inputP95 = percentile(metrics.inputs), longTaskMax = Math.max(0, ...metrics.tasks);
        samples.push({ turns, mountedMs, pages, inputP95, longTaskMax: metrics.longTaskSupported ? longTaskMax : null, ...metrics, dom: await page.locator("*").count() });
        assert.ok(inputP95 <= 200, `${turns} turns input P95 ${inputP95}`);
        assert.ok(longTaskMax <= 500, `${turns} turns longest task ${longTaskMax}`);
        await page.evaluate(() => window.chatFixture.settle());
      }
      await frame();
      await input.fill("");
      await navigate("u998"); await frame();
      await page.screenshot({ path: path.join(evidence, `${name}-chat.png`) });
      let expanded;
      if (process.env.CHAT_EXPANDED === "1") {
        const started = Date.now();
        await page.evaluate(() => {
          document.querySelectorAll('[data-chat-kind="process"] button[aria-expanded="false"]').forEach(button => button.click());
        }); await frame();
        await page.evaluate(() => document.querySelectorAll('.chat-reasoning [data-disclosure-row][aria-expanded="false"]').forEach(button => button.click()));
        await frame();
        const beforeTasks = await page.evaluate(() => window.chatMetrics.tasks.length);
        for (let turn = 0; turn < 1000; turn += 20) {
          await navigate(`u${turn}`);
          await page.waitForFunction(() => window.chatFixture.pending() === 0); await frame();
          await page.evaluate(() => document.querySelectorAll('.chat-code-fold button[aria-expanded="false"]').forEach(button => button.click()));
        }
        await page.locator('[data-chat-anchor-key="tool980"] .chat-tool [data-disclosure-row]').click();
        await page.locator('[data-chat-anchor-key="tool980"] .dsh-ToolRow-inspectButton').click();
        await page.locator(".chat-details__body > .btn").first().click(); await frame();
        expanded = { elapsedMs: Date.now() - started, dom: await page.locator("*").count(),
          tasks: await page.evaluate(start => window.chatMetrics.tasks.slice(start), beforeTasks),
          heap: await page.evaluate(() => performance.memory?.usedJSHeapSize) };
        await page.keyboard.press("Escape");
      }
      const soakSeconds = Number(process.env.CHAT_SOAK_SECONDS ?? 0);
      if (soakSeconds > 0) {
        await page.evaluate(() => window.chatFixture.reset(240)); await frame();
        for (let pageIndex = 0; pageIndex < 3; pageIndex++) { await page.evaluate(() => window.chatFixture.older()); await frame(); }
        const started = Date.now(); let cycles = 0;
        while (Date.now() - started < soakSeconds * 1000) {
          await page.evaluate(index => window.chatFixture.tick(index % 40), cycles);
          await input.press("s"); await input.press("Backspace");
          if (cycles % 10 === 0) {
            await navigate("u238");
            await page.locator('[data-chat-kind="process"][data-chat-turn="u238"] > button').click();
          }
          if (cycles % 10 === 5) { await scroll.hover(); await page.mouse.wheel(0, -120); }
          if (cycles % 20 === 19) { await page.evaluate(() => window.chatFixture.switchSession()); await frame(); }
          cycles++;
        }
        await page.evaluate(() => window.chatFixture.settle()); await frame();
        report.soak = { elapsedMs: Date.now() - started, cycles, dom: await page.locator("*").count() };
        assert.equal(await input.inputValue(), "", "continuous streaming never loses or duplicates real input");
      }
      const switches = [];
      await page.evaluate(() => window.chatFixture.reset(60)); await frame();
      const cdp = name === "chromium" ? await page.context().newCDPSession(page) : undefined;
      const heap = async () => { if (!cdp) return undefined; await cdp.send("HeapProfiler.collectGarbage"); return (await cdp.send("Runtime.getHeapUsage")).usedSize; };
      const baseline = await heap();
      for (let index = 0; index < 20; index++) {
        const duration = await page.evaluate(() => new Promise(resolve => {
          const start = performance.now(); window.chatFixture.switchSession(); requestAnimationFrame(() => requestAnimationFrame(() => resolve(performance.now() - start)));
        })); switches.push(duration);
      }
      const released = await heap();
      const heapGrowth = released === undefined ? undefined : released - baseline;
      assert.ok(percentile(switches) <= 300, `session switch P95 ${percentile(switches)}`);
      if (heapGrowth !== undefined) assert.ok(heapGrowth <= 20 * 1024 * 1024, `released heap growth ${heapGrowth}`);
      await page.waitForFunction(() => window.chatFixture.pending() === 0);
      await frame(); await frame();
      // IntersectionObserver can admit the final visible parse after a transient
      // zero-pending snapshot. Require bounded quiescence first; a layout loop
      // can never satisfy this condition and fails the five-second deadline.
      await page.waitForFunction(() => {
        const last = window.chatWrites.at(-1);
        const stamp = `${last?.generation}:${last?.transaction}`;
        if (window.chatIdle?.stamp !== stamp || window.chatFixture.pending()) window.chatIdle = { stamp, at: performance.now() };
        return performance.now() - window.chatIdle.at >= 250;
      }, null, { timeout: 5000 });
      const writesBeforeIdle = await page.evaluate(() => window.chatWrites.at(-1)?.transaction);
      await page.waitForTimeout(1000);
      const writesAfterIdle = await page.evaluate(() => window.chatWrites.at(-1)?.transaction);
      assert.equal(writesAfterIdle, writesBeforeIdle, "settled layout queue converges");
      await page.evaluate(() => window.chatFixture.weather());
      await page.locator('[data-chat-anchor-key="weather-final"] table').waitFor();
      const presented = page.locator('.presented-files');
      await presented.waitFor();
      assert.equal(await presented.locator('.presented-file').count(), 2, 'trusted present result renders one card per file');
      assert.match(await presented.innerText(), /shanghai-weather\.html/);
      assert.match(await presented.innerText(), /weather-notes\.md/);
      await presented.scrollIntoViewIfNeeded();
      await page.screenshot({ path: path.join(evidence, `${name}-presented-files.png`) });
      await frame();
      await scroll.hover();
      await page.mouse.wheel(0, -500);
      await frame();
      const processToggle = page.locator('.chat-process');
      assert.equal(await processToggle.getAttribute('aria-expanded'), 'false', 'recovered weather turn folds');
      assert.equal(await page.locator('.chat-tool').count(), 0, 'collapsed process does not mount tool bodies');
      assert.match(await processToggle.innerText(), /4/);
      await page.screenshot({ path: path.join(evidence, `${name}-weather-collapsed.png`) });
      await processToggle.click();
      await page.locator('[data-chat-anchor-key="weather-search"]').scrollIntoViewIfNeeded();
      const weatherRows = await page.locator('.chat-tool [data-disclosure-row]').evaluateAll(rows => rows.map(row => ({ height: row.getBoundingClientRect().height, text: row.textContent })));
      assert.ok(weatherRows.every(row => row.height <= 32), 'tools use compact single-line rows');
      assert.ok(weatherRows.some(row => row.text.includes('获取并核对上海天气')), 'description replaces line counts');
      assert.equal(await page.locator('.dsh-ContextInjectionRow-body').count(), 0, 'permission record starts collapsed');
      await page.screenshot({ path: path.join(evidence, `${name}-weather-expanded.png`) });
      await page.locator('[data-chat-anchor-key="weather-bash"] [data-disclosure-row]').click();
      await page.locator('[data-terminal]').waitFor();
      await page.screenshot({ path: path.join(evidence, `${name}-weather-terminal.png`) });
      await scroll.hover();
      await page.mouse.wheel(0, -500);
      await frame();
      await page.locator('[data-chat-anchor-key="weather-search"] [data-disclosure-row]').click();
      await page.locator('[data-web="search"]').waitFor();
      assert.equal(await page.locator('[data-web="search"] a').count(), 1, 'web sources retain safe host links');
      report.weatherRows = weatherRows;
      assert.deepEqual(errors, []);
      Object.assign(report, { complete: true, expanded, switches, switchP95: percentile(switches), heapGrowth, anchorDrift: topAfter - anchor.top, prependDrift: topPrepended - anchor.top });
      console.log(JSON.stringify(report));
    } catch (error) {
      report.failure = String(error); throw error;
    } finally { await browser?.close(); await electronApp?.close(); }
  }
} finally { await server.httpServer.close(); await writeFile(path.join(evidence, `${process.env.CHAT_BROWSER ?? "browsers"}-results.json`), JSON.stringify(reports, null, 2)); await rm(outDir, { recursive: true, force: true }); }
