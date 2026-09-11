import assert from "node:assert/strict";
import { initialState, reducer, type State } from "../lib/useController";

const originalNow = Date.now;
let now = 1_000;
Date.now = () => now;
const done = (s: State) => reducer(s, { type: "event", e: { kind: "turn_done" } });
const idle = (s: State, backgroundJobs = 0) => reducer(s, {
  type: "backend_status", running: false, pendingPrompt: false,
  cancellable: false, cancelRequested: false, backgroundJobs,
});
const metrics = (s: State) => [s.turnDoneAt, s.lastTurnDoneAt, s.lastTurnOutputTokens,
  s.lastTurnModelMs, s.lastTurnWaitAccumMs, s.lastTurnOutputEstimated];
try {
  const active: State = { ...initialState, running: true, turnActive: true,
    turnStartAt: 1_000, backgroundJobs: 1, turnOutputTokens: 20, turnTokens: 100,
    turnModelActiveMs: 2_000, live: { id: "a", text: "x".repeat(16), reasoning: "", reasoningComplete: true } };
  now = 21_000;
  const completed = done(active);
  assert.equal(completed.turnDoneAt - completed.turnStartAt, 20_000);
  assert.equal(completed.lastTurnOutputTokens, 24);
  now = 90_000;
  const jobsEnded = idle(completed);
  assert.equal(jobsEnded.backgroundJobs, 0);
  assert.deepEqual(metrics(jobsEnded), metrics(completed), "background completion preserves the final snapshot");
  assert.deepEqual(metrics(done(jobsEnded)), metrics(completed), "duplicate turn_done is idempotent");
  now = 21_000;
  const fallback = idle(active, 1);
  now = 90_000;
  assert.deepEqual(metrics(done(fallback)), metrics(fallback), "late turn_done preserves a backend-settled snapshot");

  now = 21_000;
  const waiting = done({ ...active, promptWaitStartedAt: 11_000, turnModelActiveAt: 10_000 });
  assert.equal(waiting.lastTurnWaitAccumMs, 10_000, "open wait closes at completion");
  assert.equal(waiting.promptWaitStartedAt, undefined);
  assert.equal(waiting.lastTurnModelMs, 13_000, "model activity closes at the same timestamp");
  assert.deepEqual(metrics(idle(waiting)), metrics(waiting));

  const planGate = done({ ...active, promptWaitStartedAt: 11_000,
    approval: { id: "plan", tool: "exit_plan_mode", subject: "Plan ready" } });
  assert.equal(planGate.running, true, "plan approval remains actionable after turn_done");
  assert.equal(planGate.promptWaitStartedAt, 21_000);
  now = 90_000;
  const gateClosed = idle(planGate);
  assert.deepEqual(metrics(gateClosed), metrics(planGate), "later plan approval wait cannot rewrite the settled snapshot");
  const lateUsage = reducer(completed, { type: "event", e: { kind: "usage", usage: {
    promptTokens: 100, completionTokens: 30, totalTokens: 130, cacheHitTokens: 0,
    cacheMissTokens: 100, sessionCacheHitTokens: 0, sessionCacheMissTokens: 100,
  } } });
  assert.equal(lateUsage.turnDoneAt, completed.turnDoneAt, "late usage cannot change the completion timestamp");

  now = 100_000;
  for (const next of [
    reducer(completed, { type: "user", text: "next", seq: 1, submissionId: "next-turn" }),
    reducer(completed, { type: "event", e: { kind: "turn_started", turnStartedAt: now } }),
    reducer(completed, { type: "backend_status", running: true, cancellable: true, turnStartedAt: now }),
  ]) {
    assert.equal(next.turnDoneAt, 0, "new turn clears completion through every entrypoint");
    assert.equal(next.turnOutputTokens, 0);
    now += 1_000;
    assert.notEqual(done(next).turnDoneAt, completed.turnDoneAt);
  }
  const retry = reducer(active, { type: "event", e: { kind: "retrying", retryAttempt: 1, retryMax: 3 } });
  assert.equal(retry.turnStartAt, active.turnStartAt);
  assert.equal(retry.turnOutputTokens, 20, "same-turn retry retains counters");
  assert.deepEqual(metrics(completed), [21_000, 21_000, 24, 2_000, 0, true], "other tab transitions cannot mutate a completed state");

  // An MCP interaction is a user wait like an approval or an ask. Clearing the
  // approval while one is outstanding must not close the shared interval, or the
  // MCP wait never reaches turnWaitAccumMs and the turn clock overcounts it.
  now = 5_000;
  const bothPrompts: State = { ...initialState, running: true, turnActive: true,
    turnStartAt: 1_000, promptWaitStartedAt: 1_000,
    approval: { id: "a1", tool: "write_file", subject: "Run command" },
    mcpInteraction: { id: "m1", server: "srv", mode: "form", message: "Fill the form" } };
  const approvalCleared = reducer(bothPrompts, { type: "clearApproval" });
  assert.equal(approvalCleared.promptWaitStartedAt, 1_000,
    "clearing an approval leaves a concurrent MCP wait open");
  assert.equal(approvalCleared.pendingPrompt, true,
    "an outstanding MCP interaction still counts as a pending prompt");
  now = 9_000;
  const mcpAnswered = reducer(approvalCleared, {
    type: "expire_prompt", id: "m1", epoch: approvalCleared.promptEpoch, kind: "mcp" });
  assert.equal(mcpAnswered.promptWaitStartedAt, undefined, "the last prompt closes the interval");
  assert.equal(mcpAnswered.turnWaitAccumMs, 8_000, "the whole MCP wait is charged exactly once");

  now = 5_000;
  const drain: State = { ...bothPrompts };
  const drained = reducer(drain, { type: "approval_drained", ids: ["a1"], epoch: drain.promptEpoch });
  assert.equal(drained.promptWaitStartedAt, 1_000,
    "an auto-drained approval also leaves a concurrent MCP wait open");
  console.log("completed-turn telemetry: all assertions passed");
} finally {
  Date.now = originalNow;
}
