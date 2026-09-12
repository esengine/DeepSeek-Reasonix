// Run: tsx src/__tests__/completion-summary-ui.test.tsx

import { createTranscriptHarness } from "./transcript-dom-harness";
import type { Item } from "../lib/useController";
import type { WireCompletionSummary } from "../lib/types";

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\ncompletion summary UI");

const harness = await createTranscriptHarness();
const changesOpens: (WireCompletionSummary | undefined)[] = [];
const earlierSummary = {
  preset: "balanced",
  verdict: "partial",
  mutations: 1,
  checks_passed: 2,
  checks_failed: 1,
  checks_suppressed: 0,
  review: "passed",
} satisfies WireCompletionSummary;
const laterSummary = {
  preset: "balanced",
  verdict: "partial",
  mutations: 3,
  checks_passed: 4,
  checks_failed: 0,
  checks_suppressed: 1,
  review: "passed",
} satisfies WireCompletionSummary;
type CompletionNotice = Extract<Item, { kind: "notice" }> & { completionSummary: WireCompletionSummary };
const completionNotice = (id: string, summary: WireCompletionSummary): CompletionNotice => ({
  kind: "notice",
  id,
  level: "warn",
  variant: "completion",
  title: "This turn still needs attention",
  text: "One or more checks did not pass. Review the changes before continuing.",
  action: "open_changes",
  completionSummary: summary,
});
const items: Item[] = [
  { kind: "user", id: "u1", text: "update it" },
  { kind: "assistant", id: "a1", text: "Updated.", reasoning: "", streaming: false },
  completionNotice("q1", earlierSummary),
  { kind: "user", id: "u2", text: "update it again" },
  { kind: "assistant", id: "a2", text: "Updated again.", reasoning: "", streaming: false },
  completionNotice("q2", laterSummary),
];

try {
  await harness.render(items, { running: false }); await harness.settle();
  const records = harness.container.querySelectorAll(".chat-notice");
  ok(records.length === 2, "completion results remain ordinary records outside folds");
  ok(records[0].textContent?.includes(items[2].text), "record preserves its explanation");
  ok(records[0].querySelector("details pre")?.textContent?.includes('"checks_failed": 1'), "older result preserves its own details");
  ok(records[1].querySelector("details pre")?.textContent?.includes('"checks_passed": 4'), "newer result preserves its own details");
  ok(!records[0].querySelector("button"), "retired delivery and verification actions are absent");
} finally {
  await harness.unmount();
  await harness.close();
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
