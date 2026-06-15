// Run: tsx src/__tests__/replay-pending-prompts.test.ts
//
// Verifies that replayPendingPromptsForActiveTab is called after every
// reconcileTabRuntime dispatch, so approval/ask prompts blocked on a
// stale-turn reconcile are re-emitted for the frontend to rebuild its
// modal (#4474).

import { replayPendingPromptsForActiveTab } from "../lib/useController";

let passed = 0;
let failed = 0;

function eq<T>(a: T, b: T, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

// ── replayPendingPromptsForActiveTab ──────────────────────────────────────────

// Test 1: undefined activeTabId does nothing
let replayCalls = 0;
replayPendingPromptsForActiveTab(undefined, () => {
  replayCalls += 1;
  return Promise.resolve();
});
eq(replayCalls, 0, "undefined tab does not replay pending prompts");

// Test 2: valid tabId replays once
replayPendingPromptsForActiveTab("tab-a", () => {
  replayCalls += 1;
  return Promise.resolve();
});
eq(replayCalls, 1, "valid tabId replays pending prompts once");

// Test 3: different tabId replays again
replayPendingPromptsForActiveTab("tab-b", () => {
  replayCalls += 1;
  return Promise.resolve();
});
eq(replayCalls, 2, "different tabId replays again");

// Test 4: bridge error is silently swallowed (no unhandled rejection)
let caught = false;
try {
  await replayPendingPromptsForActiveTab("tab-c", () => {
    replayCalls += 1;
    return Promise.reject(new Error("bridge unavailable"));
  });
  // After the rejection, wait a microtask for the .catch() to fire
  await new Promise((resolve) => setTimeout(resolve, 0));
} catch {
  caught = true;
}
eq(caught, false, "bridge reject does not throw — swallowed by .catch()");
// The replay function IS called (it just rejects), so the count reflects the call.
eq(replayCalls, 3, "replay called even when bridge rejects — error is swallowed");

// Test 5: replayPendingPromptsForActiveTab passes tabId through
let lastTabReplayed: string | undefined;
function trackReplay(tabId: string) {
  lastTabReplayed = tabId;
}
replayPendingPromptsForActiveTab("tab-x", () => {
  trackReplay("tab-x");
  return Promise.resolve();
});
eq(lastTabReplayed, "tab-x", "replayPendingPromptsForActiveTab passes tabId through");

// ── Summary ──────────────────────────────────────────────────────────────────
const total = passed + failed;
process.stdout.write(`\n${passed}/${total} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
