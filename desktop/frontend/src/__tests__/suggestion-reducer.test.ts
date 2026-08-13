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

// A "suggestion" action stores the predicted next prompt on the tab state.
{
  const s = reducer(initialState, { type: "suggestion", text: "继续修复那个 bug" });
  eq(s.suggestion, "继续修复那个 bug", "suggestion action stores text");
}

// An empty suggestion clears the field.
{
  const withText = reducer(initialState, { type: "suggestion", text: "继续修复那个 bug" });
  const s = reducer(withText, { type: "suggestion", text: "" });
  eq(s.suggestion, undefined, "empty suggestion clears field");
}

// Starting a new user turn clears any pending suggestion.
{
  const withText = reducer(initialState, { type: "suggestion", text: "继续修复那个 bug" });
  const s = reducer(withText, {
    type: "user",
    text: "下一步怎么做",
    seq: withText.seq,
    submissionId: "sub-1",
  });
  eq(s.suggestion, undefined, "new user turn clears suggestion");
}

// A completed turn does not itself carry a suggestion (the fetch dispatches a
// separate "suggestion" action); here we just confirm turn_done leaves it alone.
{
  const withText = reducer(initialState, { type: "suggestion", text: "继续修复那个 bug" });
  const done: WireEvent = { kind: "turn_done", seq: 1, outcome: "completed" } as WireEvent;
  const s = reducer(withText, { type: "event", e: done });
  eq(s.suggestion, "继续修复那个 bug", "turn_done keeps existing suggestion until replaced");
}

if (failed > 0) {
  process.exitCode = 1;
}
process.stdout.write(`suggestion-reducer: ${passed} passed, ${failed} failed\n`);
