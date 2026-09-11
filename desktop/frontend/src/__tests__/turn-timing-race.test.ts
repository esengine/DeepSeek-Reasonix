// Run: tsx src/__tests__/turn-timing-race.test.ts

import { initialState, promptEventClock, reducer } from "../lib/useController";

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

process.stdout.write("turn timing snapshot ordering\n");

const originalNow = Date.now;
let now = 2_000;
Date.now = () => now;
try {
  let state = reducer(initialState, {
    type: "event",
    e: { kind: "turn_started", submissionId: "turn-a", turnStartedAt: 2_000 },
  });
  const staleSnapshotAt = promptEventClock();
  state = reducer(state, { type: "event", e: { kind: "turn_done", submissionId: "turn-a" } });

  const staleAfterDone = reducer(state, {
    type: "backend_status",
    running: true,
    cancellable: true,
    turnStartedAt: 2_000,
    snapshotAt: staleSnapshotAt,
  });
  eq(staleAfterDone.running, false, "a stale running snapshot cannot resurrect a completed turn");

  now = 4_000;
  state = reducer(state, {
    type: "event",
    e: { kind: "turn_started", submissionId: "turn-b", turnStartedAt: 4_000 },
  });

  const staleRunning = reducer(state, {
    type: "backend_status",
    running: true,
    cancellable: true,
    turnStartedAt: 2_000,
    snapshotAt: staleSnapshotAt,
  });
  eq(staleRunning.turnStartAt, 4_000, "a stale running snapshot cannot rewind a newer turn clock");

  const unorderedRunning = reducer(state, {
    type: "backend_status",
    running: true,
    cancellable: true,
    turnStartedAt: 2_000,
  });
  eq(unorderedRunning.turnStartAt, 4_000, "an unordered status keeps a surviving newer local clock");

  const staleIdle = reducer(state, {
    type: "backend_status",
    running: false,
    cancellable: false,
    snapshotAt: staleSnapshotAt,
  });
  eq(staleIdle.running, true, "a stale idle snapshot cannot stop a newer live turn");

  now = 5_000;
  const correctedFallback = reducer({ ...state, turnStartAt: 3_900 }, {
    type: "backend_status",
    running: true,
    cancellable: true,
    turnStartedAt: 4_000,
  });
  eq(correctedFallback.turnStartAt, 4_000, "a newer backend start still corrects an earlier local fallback");
} finally {
  Date.now = originalNow;
}

if (failed > 0) {
  process.stderr.write(`\n${failed} check(s) failed\n`);
  process.exitCode = 1;
} else {
  process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
}
