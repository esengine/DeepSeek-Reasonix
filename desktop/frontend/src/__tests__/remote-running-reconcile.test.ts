// Run: tsx src/__tests__/remote-running-reconcile.test.ts
//
// The remote session reuses this reducer; these cases pin the reconciliation
// contract that keeps the remote pill from spinning forever:
//   - a failed submit rolls the optimistic running flag back (send_failed)
//   - losing the serve connection mid-turn stops the pill (turn_interrupted)
//   - provider_unreachable surfaces as one self-replacing notice and clears
//     on the next turn

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

const submit = { type: "user" as const, text: "你好", seq: 1, submissionId: "remote-1" };
const event = (e: Partial<WireEvent> & { kind: WireEvent["kind"] }) =>
  reducer(initialState, { type: "event", e: e as WireEvent });

// --- a failed submit rolls running back and marks the bubble failed ---
{
  const started = reducer(initialState, submit);
  eq(started.running, true, "optimistic submit sets running");
  const rolled = reducer(started, { type: "send_failed", submissionId: "remote-1", error: "Send failed: refused" });
  eq(rolled.running, false, "send_failed clears running");
  eq(rolled.turnActive, false, "send_failed clears turnActive");
  eq(rolled.items.some((it) => it.kind === "user" && it.failed), true, "send_failed marks the bubble failed");
  eq(rolled.items.some((it) => it.kind === "notice"), true, "send_failed adds a notice");
  // A stale send_failed for another submission must not clobber a live turn.
  const live = reducer(started, { type: "event", e: { kind: "turn_started" } as WireEvent });
  const untouched = reducer(live, { type: "send_failed", submissionId: "remote-999", error: "x" });
  eq(untouched.running, true, "stale send_failed leaves a live turn running");
}

// --- losing the connection mid-turn stops the pill ---
{
  const running = event({ kind: "turn_started" });
  eq(running.running, true, "turn_started sets running");
  const interrupted = reducer(running, { type: "turn_interrupted" });
  eq(interrupted.running, false, "turn_interrupted clears running");
  eq(interrupted.turnActive, false, "turn_interrupted clears turnActive");
  eq(interrupted.cancellable, false, "turn_interrupted clears cancellable");
  eq(interrupted.items.some((it) => it.kind === "notice"), true, "turn_interrupted adds a notice");
  // Idempotent on an idle transcript: no phantom notice.
  const idle = reducer(initialState, { type: "turn_interrupted" });
  eq(idle.items.length, initialState.items.length, "turn_interrupted is a no-op when idle");
}

// --- provider_unreachable: one self-replacing notice, cleared next turn ---
{
  const running = event({ kind: "turn_started" });
  const first = event({ kind: "provider_unreachable", text: "dial tcp 127.0.0.1:38211: connect: connection refused" }) as typeof running;
  const withNotice = reducer(running, { type: "event", e: { kind: "provider_unreachable", text: "dial tcp 127.0.0.1:38211: connect: connection refused", retryAttempt: 1, retryMax: 10 } as WireEvent });
  const count = withNotice.items.filter((it) => it.id === "provider-unreachable").length;
  eq(count, 1, "provider_unreachable adds exactly one notice");
  const again = reducer(withNotice, { type: "event", e: { kind: "provider_unreachable", text: "still down", retryAttempt: 2, retryMax: 10 } as WireEvent });
  eq(again.items.filter((it) => it.id === "provider-unreachable").length, 1, "repeat provider_unreachable replaces the notice");
  const nextTurn = reducer(again, { type: "event", e: { kind: "turn_started" } as WireEvent });
  eq(nextTurn.items.some((it) => it.id === "provider-unreachable"), false, "next turn clears the channel notice");
  void first;
}

// --- remote /status reconciliation maps onto backend_status ---
{
  const running = event({ kind: "turn_started" });
  const idleNow = reducer(running, {
    type: "backend_status",
    running: false,
    cancellable: false,
    snapshotAt: Date.now(),
  });
  eq(idleNow.running, false, "idle /status snapshot clears running");
  eq(idleNow.turnActive, false, "idle /status snapshot clears turnActive");
  const busyAgain = reducer(idleNow, {
    type: "backend_status",
    running: true,
    snapshotAt: Date.now(),
  });
  eq(busyAgain.running, true, "running /status snapshot restores running");
}

if (failed > 0) {
  process.stdout.write(`\n${failed} FAILED, ${passed} passed\n`);
  process.exit(1);
}
process.stdout.write(`\n${passed} passed, 0 failed\n`);
