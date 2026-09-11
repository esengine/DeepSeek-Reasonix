// Run: tsx src/__tests__/hydrate-replace-surface-failure.test.ts

import { initialState, reducer, type Item } from "../lib/useController";

let passed = 0;
let failed = 0;
function ok(value: boolean, label: string) {
  process.stdout.write(`  ${value ? "PASS" : "FAIL"}  ${label}\n`);
  if (value) passed += 1;
  else failed += 1;
}

console.log("\nreplace-surface hydrate failure");
const sourceItems: Item[] = [{ kind: "user", id: "source-only", text: "SOURCE SESSION SENTINEL" }];
const source = { ...initialState, items: sourceItems };
const targetLoading = reducer(
  reducer(reducer(source, { type: "hydrate_start", reason: "open-topic" }), { type: "reset" }),
  { type: "hydrate_start", reason: "open-topic" },
);
ok(targetLoading.items.length === 0, "replace-surface clears source rows before target history settles");
const targetFailed = reducer(targetLoading, { type: "hydrate_error", reason: "open-topic", error: "history failed" });
ok(targetFailed.hydrating === false, "target history failure exits the loading state");
ok(targetFailed.hydrateError === "history failed", "target history failure exposes a retryable error");
ok(!targetFailed.items.some((item) => item.kind === "user" && item.text === "SOURCE SESSION SENTINEL"), "target history failure never restores source rows");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
