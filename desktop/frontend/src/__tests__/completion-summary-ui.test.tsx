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
let opens = 0;
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
  const verificationOpens: WireCompletionSummary[] = [];
  await harness.render(items, {
    running: false,
    onOpenChanges: () => { opens += 1; },
    onOpenVerification: (summary: WireCompletionSummary) => { verificationOpens.push(summary); },
  });
  ok(harness.container.textContent?.includes("This turn still needs attention"), "actionable summary stays visible outside the process fold");
  ok(!harness.container.textContent?.includes("balanced"), "compact notice exposes no internal enum values");
  const button = Array.from(harness.container.querySelectorAll("button")).find((node) => node.textContent?.includes("View changes"));
  ok(button, "completion notice offers a View changes action");
  button?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  await harness.flush();
  ok(opens === 1, "View changes delegates to the workspace panel action");
  const verifyButtons = Array.from(harness.container.querySelectorAll("button")).filter((node) => node.textContent?.includes("Turn verification"));
  ok(verifyButtons.length === 2, "each completion notice offers a Turn verification action");
  verifyButtons[0]?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  verifyButtons[1]?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  await harness.flush();
  ok(verificationOpens[0] === earlierSummary, "an older notice opens its own turn summary");
  ok(verificationOpens[1] === laterSummary, "the latest notice opens its own turn summary");
} finally {
  await harness.unmount();
  await harness.close();
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
