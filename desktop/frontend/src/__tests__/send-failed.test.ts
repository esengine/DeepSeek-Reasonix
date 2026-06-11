// Run: tsx src/__tests__/send-failed.test.ts

import { initialState, reducer } from "../lib/useController";
import type { WireEvent } from "../lib/types";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\nsend failure feedback");

const sent = reducer({ ...initialState }, { type: "user", text: "hello", seq: 0 });
eq(sent.items.length, 1, "submit appends the user bubble immediately");
eq(sent.items[0].kind === "user" && sent.items[0].text, "hello", "bubble carries the submitted text");
eq(sent.running, true, "submit marks the turn running");
eq(sent.pendingUser, "hello", "submit tracks the optimistic bubble");

const confirmed = reducer(sent, { type: "event", e: { kind: "text", text: "hi" } as WireEvent });
eq(confirmed.items.filter((it) => it.kind === "user").length, 1, "first backend event confirms without duplicating");
eq(confirmed.pendingUser, undefined, "confirmation clears the pending marker");

const failedState = reducer(sent, { type: "send_failed", error: "Send failed: bridge unavailable" });
const failedBubble = failedState.items.find((it) => it.kind === "user");
eq(failedBubble?.kind === "user" && failedBubble.failed, true, "send_failed marks the bubble failed");
const notice = failedState.items[failedState.items.length - 1];
eq(notice.kind, "notice", "send_failed appends a notice");
eq(notice.kind === "notice" && notice.level, "warn", "the notice is a warning");
eq(failedState.running, false, "send_failed stops the running indicator");
eq(failedState.pendingUser, undefined, "send_failed clears the pending marker");

const lateFailure = reducer(confirmed, { type: "send_failed", error: "Send failed: late" });
eq(lateFailure, confirmed, "send_failed after backend confirmation is a no-op");

const unsent = reducer(sent, { type: "unsend" });
eq(unsent.pendingUser, undefined, "unsend clears the pending marker");
eq(unsent.discardTurn, true, "unsend discards the in-flight turn");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
