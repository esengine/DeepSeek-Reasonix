// Run: tsx src/__tests__/controller-turn-timing.test.ts

import { initialState, reducer, type State } from "../lib/useController";

type ReducerState = ReturnType<typeof reducer>;

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

function ok(value: boolean, label: string) {
  eq(value, true, label);
}

function runningTurn(overrides: Partial<State> = {}): ReducerState {
  return {
    ...initialState,
    running: true,
    turnActive: true,
    turnStartAt: 1_000,
    backendTurnStartedAtMs: 1_000,
    ...overrides,
  };
}

console.log("\nturn timing across tab switches (#7987)");

{
  // Full switch-back sequence: activation clears the local lifecycle flags,
  // then the optimistic runtime status arrives carrying the backend's turn
  // start. The elapsed clock must continue from the original start.
  const before = Date.now();
  let state = runningTurn({ turnStartAt: 1_000, backendTurnStartedAtMs: undefined });
  state = reducer(state, { type: "backend_activation_start", backendTurnStartedAtMs: 1_000 });
  eq(state.running, false, "activation_start clears the local running flag");
  eq(state.backendTurnStartedAtMs, 1_000, "activation_start keeps the backend turn evidence");
  state = reducer(state, { type: "backend_status", running: true, turnStartedAtMs: 1_000 });
  eq(state.running, true, "backend_status restores the running flag");
  eq(state.turnStartAt, 1_000, "switch-back restarts the clock from the original turn start");
  ok(state.turnStartAt < before || state.turnStartAt <= Date.now(), "restart is not a fresh Date.now() clock");
}

{
  // Deferred hydration reset the whole state mid-turn; the next snapshot must
  // rebuild turnStartAt from the backend timestamp instead of "now".
  let state = runningTurn();
  state = reducer(state, { type: "reset" });
  eq(state.turnStartAt, initialState.turnStartAt, "reset wipes the local turn clock");
  state = reducer(state, { type: "backend_status", running: true, turnStartedAtMs: 1_700_000_000_000 - 45_000 });
  eq(state.turnStartAt, 1_700_000_000_000 - 45_000, "rebuilt state ages the turn from backend telemetry");
  eq(state.backendTurnStartedAtMs, 1_700_000_000_000 - 45_000, "backend evidence is stored alongside");
}

{
  // A surviving local clock always wins over the backend timestamp so a
  // slightly stale snapshot cannot rewind the timer.
  const state = reducer(runningTurn({ turnStartAt: 2_000, backendTurnStartedAtMs: 2_000 }),
    { type: "backend_status", running: true, turnStartedAtMs: 1_500 });
  eq(state.turnStartAt, 2_000, "surviving local clock is preferred over backend timestamp");
  eq(state.backendTurnStartedAtMs, 1_500, "backend evidence still tracks the fresher snapshot");
}

{
  // No backend telemetry (older build / background tab): Date.now() fallback
  // must keep working exactly as before.
  const before = Date.now();
  const state = reducer({ ...initialState }, { type: "backend_status", running: true });
  ok(state.turnStartAt >= before, "missing backend timestamp falls back to Date.now()");
}

{
  // Repeated identical snapshots stay no-ops (referential equality).
  const first = reducer({ ...initialState }, { type: "backend_status", running: true, turnStartedAtMs: 5_000 });
  const second = reducer(first, { type: "backend_status", running: true, turnStartedAtMs: 5_000 });
  ok(second === first, "identical backend_status dispatch remains a no-op");
}

{
  // Idle snapshots and turn_done both retire the backend evidence.
  let state = runningTurn();
  state = reducer(state, { type: "backend_status", running: false });
  eq(state.running, false, "idle snapshot stops the turn");
  eq(state.backendTurnStartedAtMs, undefined, "idle snapshot clears the backend turn evidence");

  state = runningTurn();
  state = reducer(state, { type: "event", e: { kind: "turn_done" } });
  eq(state.running, false, "turn_done settles the turn");
  eq(state.backendTurnStartedAtMs, undefined, "turn_done clears the backend turn evidence");
}

{
  // turn_started is a local event, not backend confirmation: it must not
  // install backend evidence. A lost turn_done would otherwise leave the
  // evidence behind forever and the deferred-reset guard would never see idle.
  const state = reducer({ ...initialState, backendTurnStartedAtMs: 123 }, { type: "event", e: { kind: "turn_started" } });
  eq(state.running, true, "turn_started marks the turn running");
  eq(state.backendTurnStartedAtMs, 123, "turn_started never installs backend turn evidence");
}

if (failed > 0) {
  console.error(`\n${failed} check(s) failed`);
  process.exitCode = 1;
} else {
  console.log(`\n${passed} passed, ${failed} failed`);
}
