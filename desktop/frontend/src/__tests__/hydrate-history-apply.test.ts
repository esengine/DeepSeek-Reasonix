// Run: tsx src/__tests__/hydrate-history-apply.test.ts

import {
  hasCachedLiveTurn,
  sameSessionPlaceholderItems,
  shouldApplyHydratedHistory,
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

ok(!shouldApplyHydratedHistory(true, true, false, { items: [] }), "skipHistory blocks apply");
ok(!shouldApplyHydratedHistory(false, false, false, { items: [] }), "missing projection blocks apply");
ok(shouldApplyHydratedHistory(false, true, false, { items: [] }), "idle empty surface applies history");
ok(
  shouldApplyHydratedHistory(false, true, true, { running: true, items: [] }),
  "running empty surface applies history",
);
ok(
  !shouldApplyHydratedHistory(false, true, true, {
    running: true,
    items: [{ kind: "user" }],
  }),
  "running visible transcript is not replaced",
);
ok(
  !shouldApplyHydratedHistory(false, true, true, {
    running: true,
    live: { text: "partial" },
    items: [],
  }),
  "running live stream without items is not replaced",
);
ok(
  hasCachedLiveTurn({
    running: true,
    items: [{ kind: "assistant", streaming: true }],
  }),
  "streaming assistant counts as a cached live turn",
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
