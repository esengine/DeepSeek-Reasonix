// Run: tsx src/__tests__/hydrate-history-apply.test.ts

import {
  duplicateLiveItemIds,
  hasCachedLiveTurn,
  hydratedHistoryApplyMode,
  sameSessionPlaceholderItems,
} from "../lib/hydrateHistoryApply";

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\nhydrate history apply");

const mode = hydratedHistoryApplyMode;

ok(mode(true, true, false, { items: [] }) === "skip", "skipHistory blocks apply");
ok(mode(false, false, false, { items: [] }) === "skip", "missing projection blocks apply");
ok(mode(false, true, false, { items: [] }) === "replace", "idle empty surface applies history");
ok(mode(false, true, true, { running: true, items: [] }) === "replace", "running empty surface applies history");
ok(
  mode(false, true, true, { running: true, live: { text: "partial" }, items: [] }) === "replace",
  "a mid-stream surface with no rows yet applies history",
);
ok(
  mode(false, true, true, { running: true, items: [{ kind: "user" }] }) === "prepend",
  "a running turn with no history page behind it gets one prepended",
);
ok(
  mode(false, true, true, { running: true, historyTotalTurns: 3, items: [{ kind: "user" }] }) === "skip",
  "an already-hydrated running transcript is left alone",
);
ok(
  hasCachedLiveTurn({
    running: true,
    historyTotalTurns: 2,
    items: [{ kind: "assistant", streaming: true }],
  }),
  "streaming assistant counts as a cached live turn",
);
ok(
  !hasCachedLiveTurn({ running: true, items: [{ kind: "assistant", streaming: true }] }),
  "a live turn with no history page behind it is not cached",
);
ok(
  duplicateLiveItemIds(
    [{ kind: "user", id: "h1", text: "ask" }],
    [{ kind: "user", id: "l1", text: "ask" }, { kind: "assistant", id: "l2", text: "" }],
  ).join(",") === "l1",
  "a live row the page already carries is dropped",
);
ok(
  duplicateLiveItemIds(
    [{ kind: "user", id: "h1", text: "ask" }],
    [{ kind: "assistant", id: "l2", text: "" }],
  ).length === 0,
  "a live tail the page does not carry is kept",
);
ok(
  sameSessionPlaceholderItems("a.jsonl", { meta: { sessionPath: "b.jsonl" }, items: [{ kind: "user" }] }) === undefined,
  "foreign session items are not placeholders",
);
ok(
  (sameSessionPlaceholderItems("a.jsonl", { meta: { sessionPath: "a.jsonl" }, items: [{ kind: "user" }] }) ?? []).length === 1,
  "same-session items stay placeholders",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
