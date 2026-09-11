import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import { installDesktopHostStub } from "./desktopHostStub";
import { installPerformancePressureMonitor } from "../lib/crash";
import type { RendererProfileResult } from "../lib/processDiagnostics";

const dom = new JSDOM("<!doctype html><body></body>", { url: "http://localhost" });
let now = 0;
let wall = 1_000_000;
const oldDateNow = Date.now;
Date.now = () => wall;
let observe!: (list: { getEntries(): unknown[] }) => void;
const captures: { id?: string; finish(value: RendererProfileResult): void }[] = [];
const cancelled: (string | undefined)[] = [];
Object.defineProperty(dom.window.document, "visibilityState", { value: "visible" });
dom.window.document.hasFocus = () => true;
dom.window.setInterval = () => 1;
Object.assign(globalThis, {
  window: dom.window, document: dom.window.document, performance: { now: () => now },
  PerformanceObserver: class { constructor(callback: typeof observe) { observe = callback; } observe() {} },
});
installDesktopHostStub({}, { performance: {
  captureRendererProfile: (id) => new Promise((finish) => { captures.push({ id, finish }); }),
  cancelRendererProfile: async (id) => { cancelled.push(id); },
} });
const flush = async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); };
try {
  installPerformancePressureMonitor();
  now = 20_000;
  observe({ getEntries: () => [{ startTime: now, duration: 900, name: "self" }] });
  await flush();
  const staleDismiss = document.querySelector<HTMLButtonElement>(".performance-report__dismiss")!;
  now += 620_000;
  wall += 620_000;
  observe({ getEntries: () => [{ startTime: now, duration: 900, name: "self" }] });
  await flush();
  assert.equal(captures.length, 2);
  assert.ok(captures[0].id && captures[1].id && captures[0].id !== captures[1].id);
  staleDismiss.click();
  assert.ok(document.getElementById("performance-report-prompt"), "stale actions cannot close a newer report");
  assert.deepEqual(cancelled, []);
  document.querySelector<HTMLButtonElement>(".performance-report__dismiss")!.click();
  assert.deepEqual(cancelled, [captures[1].id], "dismissal cancels only its own capture");
  captures[0].finish({ status: "captured", frames: [] });
  captures[1].finish({ status: "cancelled" });
  await flush();
  assert.equal(document.getElementById("performance-report-prompt"), null);
  console.log("performance report cancellation identity tests passed");
} finally {
  for (const capture of captures) capture.finish({ status: "cancelled" });
  Date.now = oldDateNow;
  dom.window.close();
}
