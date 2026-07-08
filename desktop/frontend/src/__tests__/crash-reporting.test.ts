// Run: tsx src/__tests__/crash-reporting.test.ts

import {
  buildCrashPayload,
  buildPerformancePayload,
  formatPerformanceContext,
  globalCrashReportReason,
  installPerformancePressureMonitor,
  normalizeCrashError,
  parseReportedPerf,
  performanceLabelForReason,
  serializeReportedPerf,
  shouldRecordEventLoopLagSample,
  shouldPromptForPerformanceLabel,
  shouldReportGlobalCrashEvent,
  shouldRecordLongTaskSample,
  topFrameFromStack,
  type PerformanceSnapshot,
} from "../lib/crash";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\ncrash reporting");

const err = new TypeError("invalid argument");
err.stack = "TypeError: invalid argument\n    at submit (src/App.tsx:12:3)";
const payload = buildCrashPayload("unhandledrejection", err, "component stack");

eq(normalizeCrashError("boom"), { errorType: "string", errorMessage: "boom" }, "normalizes string reasons");
eq(topFrameFromStack(err.stack), "at submit (src/App.tsx:12:3)", "extracts top app frame");
eq(payload.kind, "exception", "unhandled rejection is a nonfatal exception kind");
eq(payload.source, "frontend.global", "global handler payload identifies source");
eq(payload.errorType, "TypeError", "captures error type");
eq(payload.componentStack, "component stack", "captures component stack");
eq(payload.message.includes("[unhandledrejection]"), true, "keeps human-readable message");
eq(shouldReportGlobalCrashEvent({ defaultPrevented: false }), true, "reports unhandled global events by default");
eq(shouldReportGlobalCrashEvent({ defaultPrevented: true }), false, "ignores global events already handled by a filter");
eq(
  shouldReportGlobalCrashEvent({ defaultPrevented: false, message: "ResizeObserver loop limit exceeded" }),
  false,
  "ignores Chromium ResizeObserver loop limit notices",
);
eq(
  shouldReportGlobalCrashEvent({
    defaultPrevented: false,
    message: "ResizeObserver loop completed with undelivered notifications.",
  }),
  false,
  "ignores Chromium ResizeObserver undelivered notification notices",
);
eq(
  shouldReportGlobalCrashEvent({ defaultPrevented: false, error: new Error("ResizeObserver loop limit exceeded") }),
  false,
  "ignores ResizeObserver notices delivered through ErrorEvent.error",
);
eq(
  shouldReportGlobalCrashEvent({
    defaultPrevented: false,
    message: "",
    error: new Error("ResizeObserver loop limit exceeded"),
  }),
  false,
  "checks ErrorEvent.error when ErrorEvent.message is empty",
);
eq(
  shouldReportGlobalCrashEvent({
    defaultPrevented: false,
    message: "Uncaught Error",
    error: new Error("ResizeObserver loop limit exceeded"),
  }),
  false,
  "checks ErrorEvent.error when ErrorEvent.message is a wrapper",
);
eq(
  globalCrashReportReason({
    defaultPrevented: false,
    message: "Script error.",
    filename: "wails://wails/assets/index-abc123.js",
    lineno: 42,
    colno: 7,
  }),
  "Script error.\nfilename=wails://wails/assets/index-abc123.js lineno=42 colno=7",
  "adds script location to opaque window.error messages",
);
eq(
  globalCrashReportReason({
    defaultPrevented: false,
    message: "Script error.",
  }),
  "Script error.",
  "keeps opaque script errors bare when WebView provides no location",
);

const perf: PerformanceSnapshot = {
  reason: "event loop lag 1300ms",
  uptimeMs: 42_000,
  visibility: "visible",
  focused: true,
  online: true,
  hardwareConcurrency: 10,
  deviceMemoryGb: 16,
  jsHeap: { usedMb: 700, totalMb: 780, limitMb: 900, usagePercent: 77.7 },
  eventLoopLag: { currentMs: 1300, maxMs: 1300, avgMs: 220, samples: 6 },
  longTasks: {
    count: 3,
    totalMs: 1800,
    maxMs: 900,
    recent: [
      { startMs: 40_000, durationMs: 900 },
      { startMs: 41_000, durationMs: 500 },
    ],
  },
  connection: { effectiveType: "4g", rttMs: 50, downlinkMbps: 20, saveData: false },
};
const perfPayload = buildPerformancePayload(perf);
eq(perfPayload.kind, "performance", "performance pressure reports use performance kind");
eq(perfPayload.source, "frontend.performance", "performance pressure reports identify source");
eq(perfPayload.label, "performance.lag", "performance pressure reports partition by stable pressure label");
eq(perfPayload.errorType, "PerformancePressure", "performance pressure reports use a stable error type");
eq(perfPayload.errorMessage.includes("1300"), false, "performance fingerprint message avoids dynamic durations");
eq(perfPayload.label.includes("1300"), false, "performance fingerprint label avoids dynamic durations");
eq(formatPerformanceContext(perf).includes("long tasks: 3"), true, "formats long task context");
eq(perfPayload.message.includes("event loop lag 1300ms"), true, "payload message keeps lag context");
eq(performanceLabelForReason("long task 900ms"), "performance.longtask", "labels long task pressure");
eq(performanceLabelForReason("js heap 87% of limit"), "performance.heap", "labels heap pressure");
eq(shouldRecordLongTaskSample(14_000, 900, 15_000), false, "ignores startup long tasks before grace ends");
eq(shouldRecordLongTaskSample(16_000, 40, 15_000), false, "ignores short long-task observer entries");
eq(shouldRecordLongTaskSample(16_000, 900, 15_000), true, "records post-grace long tasks");
eq(shouldRecordLongTaskSample(60_000, 900, 15_000, true, 20_000), false, "ignores long tasks while the window is hidden");
eq(shouldRecordLongTaskSample(23_000, 900, 15_000, false, 20_000), false, "ignores long tasks immediately after visibility resumes");
eq(shouldRecordLongTaskSample(26_000, 900, 15_000, false, 20_000), true, "records long tasks after the visibility resume grace period");
eq(shouldRecordLongTaskSample(570_000, 92, 15_000, false, 20_000, false), false, "ignores long tasks while unfocused");
eq(shouldRecordEventLoopLagSample(true, 60_000), false, "ignores event-loop lag while the window is hidden");
eq(shouldRecordEventLoopLagSample(false, 3_000), false, "ignores event-loop lag immediately after visibility resumes");
eq(shouldRecordEventLoopLagSample(false, 6_000), true, "records event-loop lag after the visibility resume grace period");
eq(shouldRecordEventLoopLagSample(false, 60_000, false), false, "ignores event-loop lag while unfocused");

eq(shouldPromptForPerformanceLabel(false, 11 * 60_000, false), true, "prompts an unhandled label past cooldown while visible");
eq(shouldPromptForPerformanceLabel(true, 11 * 60_000, false), false, "suppresses an already reported or dismissed label");
eq(shouldPromptForPerformanceLabel(false, 5 * 60_000, false), false, "respects the prompt cooldown window");
eq(shouldPromptForPerformanceLabel(false, 11 * 60_000, true), false, "never prompts while the window is hidden");
eq(shouldPromptForPerformanceLabel(false, 11 * 60_000, false, false), false, "never prompts while unfocused");

{
  let interval: (() => void) | undefined;
  let now = 0;
  let promptPainted = false;
  const previousWindow = (globalThis as any).window;
  const previousDocument = (globalThis as any).document;
  const previousPerformance = (globalThis as any).performance;
  const previousPerformanceObserver = (globalThis as any).PerformanceObserver;
  (globalThis as any).performance = { now: () => now };
  (globalThis as any).window = {
    runtime: {},
    setInterval: (cb: () => void) => {
      interval = cb;
      return 1;
    },
  };
  (globalThis as any).document = {
    visibilityState: "visible",
    hasFocus: () => true,
    addEventListener: () => {},
    getElementById: () => {
      promptPainted = true;
      return null;
    },
  };
  (globalThis as any).PerformanceObserver = undefined;
  installPerformancePressureMonitor();
  now = 26_000;
  interval?.();
  eq(promptPainted, false, "first post-grace event-loop tick primes without reporting startup backlog");
  (globalThis as any).window = previousWindow;
  (globalThis as any).document = previousDocument;
  (globalThis as any).performance = previousPerformance;
  (globalThis as any).PerformanceObserver = previousPerformanceObserver;
}

const reportedPerf = serializeReportedPerf(new Set(["performance.lag"]), "abc123");
eq([...parseReportedPerf(reportedPerf, "abc123")], ["performance.lag"], "round-trips reported labels for the same build");
eq([...parseReportedPerf(reportedPerf, "def456")], [], "re-surfaces reported labels on a new build");
eq([...parseReportedPerf(null, "abc123")], [], "tolerates missing storage");
eq([...parseReportedPerf("{not json", "abc123")], [], "tolerates corrupt storage");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
