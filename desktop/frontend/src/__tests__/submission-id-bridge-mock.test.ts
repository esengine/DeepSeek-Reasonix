// Run: tsx src/__tests__/submission-id-bridge-mock.test.ts

import { app, onEvent } from "../lib/bridge";
import type { WireEvent } from "../lib/types";

let passed = 0;
let failed = 0;

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

console.log("\nsubmission id browser mock contract");

const events: WireEvent[] = [];
const off = onEvent((event) => events.push(event));
const submit = app.SubmitToTabWithID("mock-correlation-tab", "mock correlation turn", "opaque-correlation");
await new Promise((resolve) => setTimeout(resolve, 0));

const started = events.find((event) => event.kind === "turn_started");
eq(started?.tabId, "mock-correlation-tab", "mock TurnStarted keeps the submitted tab scope");
eq(started?.submissionId, "opaque-correlation", "mock TurnStarted carries the local submission correlation");

await app.CancelTab("mock-correlation-tab");
await submit;
off();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
