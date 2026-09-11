import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import { installDesktopHostStub } from "./desktopHostStub";
import { installPerformancePressureMonitor } from "../lib/crash";
import type { ProcessDiagnosticsSnapshot, RendererProfileResult } from "../lib/processDiagnostics";

const dom = new JSDOM("<!doctype html><body></body>", { url: "http://localhost" });
let now = 0;
let observe!: (list: { getEntries(): unknown[] }) => void;
let completeProfile!: (value: RendererProfileResult) => void;
let completeProcesses!: (value: ProcessDiagnosticsSnapshot) => void;
let cancelled = 0;
let heapRequests = 0;
const reports: string[] = [];
const copies: string[] = [];
Object.defineProperty(dom.window.document, "visibilityState", { value: "visible" });
dom.window.document.hasFocus = () => true;
dom.window.setInterval = () => 1;
Object.assign(globalThis, {
  window: dom.window, document: dom.window.document, performance: { now: () => now },
  PerformanceObserver: class { constructor(callback: typeof observe) { observe = callback; } observe() {} },
});
installDesktopHostStub({ ReportCrash: (_kind: string, payload: string) => { reports.push(payload); } }, {
  clipboardWrites: copies,
  processDiagnostics: () => new Promise((resolve) => { completeProcesses = resolve; }),
  performance: {
    captureRendererProfile: () => new Promise((resolve) => { completeProfile = resolve; }),
    cancelRendererProfile: async () => { cancelled++; },
    exportHeapSnapshot: async () => { heapRequests++; return { status: "cancelled" }; },
  },
});
installPerformancePressureMonitor();
now = 20_000;
observe({ getEntries: () => [{ startTime: 20_000, duration: 900, name: "self" }] });
const host = document.getElementById("performance-report-prompt");
assert.ok(host, "base report appears before either diagnostic promise settles");
const copy = host.querySelector<HTMLButtonElement>(".performance-report__copy")!;
const send = host.querySelector<HTMLButtonElement>(".performance-report__send")!;
const flush = async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); };
await flush();
completeProfile({ status: "captured", durationMs: 5000, frames: [{ label: "render (app.js:10)", samples: 4, selfMs: 40 }] });
await flush();
assert.equal(host.querySelector(".performance-report__copy"), copy, "enrichment preserves button identity");
copy.click(); send.click();
await flush();
assert.match(copies[0], /render \(app.js:10\)/);
assert.match(reports[0], /CPU profile after trigger: captured/);
assert.equal(heapRequests, 0, "heap capture never happens automatically");
host.querySelector<HTMLButtonElement>(".performance-report__dismiss")!.click();
assert.equal(cancelled, 0, "closing a completed report cannot cancel a later capture");
completeProcesses({ scope: "electron", samples: [] });
await flush();
assert.equal(document.getElementById("performance-report-prompt"), null, "late diagnostics never recreate a dismissed prompt");
dom.window.close();
console.log("performance report enrichment tests passed");
