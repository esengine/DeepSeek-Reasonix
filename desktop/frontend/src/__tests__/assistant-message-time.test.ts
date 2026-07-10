// Run: tsx src/__tests__/assistant-message-time.test.ts

import { initialState, reducer } from "../lib/useController";

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

console.log("\nassistant message time");

const realNow = Date.now;
try {
  Date.now = () => 1_000;
  let state = reducer(initialState, { type: "event", e: { kind: "turn_started" } });
  const streaming = state.items.find((item) => item.kind === "assistant");
  eq(streaming?.kind === "assistant" ? streaming.createdAt : undefined, 1_000, "assistant bubble records start time");

  Date.now = () => 3_500;
  state = reducer(state, { type: "event", e: { kind: "message", text: "done" } });
  const done = state.items.find((item) => item.kind === "assistant");
  eq(done?.kind === "assistant" ? done.completedAt : undefined, 3_500, "completed assistant records finish time");
} finally {
  Date.now = realNow;
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

