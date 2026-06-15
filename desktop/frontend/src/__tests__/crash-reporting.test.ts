// Run: tsx src/__tests__/crash-reporting.test.ts

import {
  buildCrashPayload,
  buildPerformancePayload,
  formatPerformanceContext,
  normalizeCrashError,
  performanceLabelForReason,
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

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
