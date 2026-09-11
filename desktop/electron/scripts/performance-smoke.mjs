// Real Electron verification of bounded diagnostic capture and report enrichment.
import assert from "node:assert/strict";
import { readFileSync, statSync, mkdirSync } from "node:fs";
import { join, resolve } from "node:path";
import { performanceFixture } from "./performance-fixture.mjs";

const fixture = await performanceFixture(true, { archiveWorker: true, benchmark: true });
try {
  const { page, app, temp } = fixture;
  assert.equal(await page.evaluate(async () => (await fetch(location.href)).headers.get("Document-Policy")), null);
  const initial = await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].webContents.debugger.isAttached());
  assert.equal(initial, false, "ordinary monitoring does not attach a profiler");
  // Exercise the production startup/visibility grace period before a deliberate
  // long task in the disposable document.
  await page.waitForTimeout(16000);
  await page.evaluate(() => {
    window.fixtureLongTasks = [];
    new PerformanceObserver((entries) => { window.fixtureLongTasks.push(...entries.getEntries().map((entry) => entry.duration)); }).observe({ entryTypes: ["longtask"] });
    // A DevTools Runtime.evaluate call is not a normal renderer task. Schedule
    // work through the event loop so the browser's long-task observer sees it.
    setTimeout(() => window.diagnosticFixture.burn(950), 0);
  });
  try { await page.locator("#performance-report-prompt").waitFor(); }
  catch (error) {
    console.error(await page.evaluate(() => ({ focused: document.hasFocus(), visibility: document.visibilityState, longTasks: window.fixtureLongTasks })));
    throw error;
  }
  const copy = page.locator(".performance-report__copy").first();
  await page.evaluate(() => window.diagnosticFixture.run(5500));
  try {
    await page.waitForFunction(() => document.querySelector(".performance-report__body")?.textContent.includes("CPU profile after trigger: captured"), null, { timeout: 12000 });
  } catch (error) {
    console.error(await page.evaluate(() => ({ focused: document.hasFocus(), status: document.querySelector(".performance-report__body")?.textContent.match(/CPU profile after trigger:[^\n]*/)?.[0] })));
    throw error;
  }
  const report = await page.locator(".performance-report__body").innerText();
  assert.match(report, /samples: .*workload\.js/);
  assert.equal(await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].webContents.debugger.isAttached()), false);
  await page.evaluate(() => Object.defineProperty(navigator, "clipboard", { value: { writeText: async (text) => { window.fixtureCopiedText = text; } }, configurable: true }));
  await copy.click();
  await page.waitForFunction(() => window.fixtureCopiedText?.includes("CPU profile after trigger: captured"));
  const result = await page.evaluate(() => window.reasonixDesktop.native.exportHeapSnapshot());
  assert.equal(result.status, "saved");
  const heap = join(temp, "fixture.heapsnapshot");
  assert.ok(statSync(heap).size > 1000);
  assert.ok(JSON.parse(readFileSync(heap, "utf8")).snapshot);
  const artifacts = resolve(import.meta.dirname, "../artifacts/performance");
  mkdirSync(artifacts, { recursive: true });
  await page.screenshot({ path: join(artifacts, "diagnostic-report.png") });
  console.log("PASS: no eager profiling; automatic bounded capture; ASAR Worker frames; live copy; debugger released; local heap snapshot");
} finally { await fixture.close(); }
