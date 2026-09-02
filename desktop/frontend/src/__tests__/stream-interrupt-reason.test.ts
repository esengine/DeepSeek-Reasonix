// Run: tsx src/__tests__/stream-interrupt-reason.test.ts

import { initialState, reducer } from "../lib/useController";

type ReducerState = ReturnType<typeof reducer>;

let passed = 0;
let failed = 0;

function ok(cond: boolean, label: string, detail?: unknown) {
  if (cond) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}${detail === undefined ? "" : `: ${JSON.stringify(detail)}`}\n`);
    failed += 1;
  }
}

function noticeTexts(state: ReducerState): Array<{ level: string; text: string }> {
  return state.items
    .filter((it): it is Extract<typeof it, { kind: "notice" }> => it.kind === "notice")
    .map((it) => ({ level: it.level, text: it.text }));
}

function interruptedTurnWithDiscard(reason?: string): ReducerState {
  let state = reducer(initialState, { type: "user", text: "hello", seq: 0, submissionId: "stream-reason" });
  state = reducer(state, { type: "event", e: { kind: "turn_started" } });
  state = reducer(state, { type: "event", e: { kind: "stream_attempt", streamAttempt: { id: "sa-1", action: "begin", attempt: 1, max: 6 } } });
  if (reason) {
    state = reducer(state, { type: "event", e: { kind: "stream_attempt", streamAttempt: { id: "sa-1", action: "discard", attempt: 1, max: 6, reason } } });
  }
  return reducer(state, { type: "event", e: { kind: "turn_done", status: "interrupted" } });
}

console.log("\ninterrupted turn surfaces the last stream failure reason (#9560)");

{
  const state = interruptedTurnWithDiscard("idle_timeout");
  const notices = noticeTexts(state);
  ok(notices.some((n) => n.level === "info" && n.text.includes("interrupted")), "generic interrupted notice stays");
  const reason = notices.find((n) => n.level === "warn" && n.text.includes("idle timeout"));
  ok(Boolean(reason), "warn notice explains the idle timeout", notices);
  ok(notices.filter((n) => n.level === "warn").length === 1, "exactly one reason notice");
}

{
  const state = interruptedTurnWithDiscard("connection_reset");
  const notices = noticeTexts(state);
  ok(notices.some((n) => n.level === "warn" && n.text.includes("reset")), "connection_reset maps to a readable reason", notices);
}

{
  const state = interruptedTurnWithDiscard(undefined);
  const notices = noticeTexts(state);
  ok(!notices.some((n) => n.level === "warn" && n.text.includes("connection")), "a stop with no recorded failure adds no reason notice", notices);
}

{
  // A discard from an earlier turn must not leak into a later interrupted turn.
  let state = reducer(initialState, { type: "user", text: "one", seq: 0, submissionId: "stream-one" });
  state = reducer(state, { type: "event", e: { kind: "turn_started" } });
  state = reducer(state, { type: "event", e: { kind: "stream_attempt", streamAttempt: { id: "sa-1", action: "begin", attempt: 1, max: 6 } } });
  state = reducer(state, { type: "event", e: { kind: "stream_attempt", streamAttempt: { id: "sa-1", action: "discard", attempt: 1, max: 6, reason: "idle_timeout" } } });
  state = reducer(state, { type: "event", e: { kind: "turn_done", status: "completed" } });
  state = reducer(state, { type: "user", text: "two", seq: 5, submissionId: "stream-two" });
  state = reducer(state, { type: "event", e: { kind: "turn_started" } });
  state = reducer(state, { type: "event", e: { kind: "turn_done", status: "interrupted" } });
  const notices = noticeTexts(state).filter((n) => n.level === "warn" && n.text.includes("idle timeout"));
  ok(notices.length === 0, "stale reason from a previous turn is cleared on turn_started", notices);
}

{
  // The agent emits one safe reason before the controller closes the turn with
  // its raw error. Desktop must keep the safe notice without adding a duplicate.
  let state = reducer(initialState, { type: "user", text: "hello", seq: 0, submissionId: "stream-terminal" });
  state = reducer(state, { type: "event", e: { kind: "turn_started" } });
  state = reducer(state, { type: "event", e: {
    kind: "notice",
    level: "warn",
    code: "stream_interrupted_idle_timeout",
    text: "model stream stalled: no data arrived before the idle timeout; check the provider gateway or network proxy",
  } });
  state = reducer(state, { type: "event", e: { kind: "turn_done", status: "failed", err: "raw upstream transport failure" } });
  const notices = noticeTexts(state);
  ok(notices.filter((n) => n.level === "warn").length === 1, "terminal stream failure renders one warning", notices);
  ok(!notices.some((n) => n.text.includes("raw upstream")), "duplicate raw turn error is suppressed", notices);
}

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
