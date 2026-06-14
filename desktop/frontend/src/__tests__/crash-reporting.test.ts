// Run: tsx src/__tests__/crash-reporting.test.ts

import { buildCrashPayload, normalizeCrashError, topFrameFromStack } from "../lib/crash";

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

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
