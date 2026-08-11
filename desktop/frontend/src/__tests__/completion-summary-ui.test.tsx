// Run: tsx src/__tests__/completion-summary-ui.test.tsx

import { createTranscriptHarness } from "./transcript-dom-harness";
import type { Item } from "../lib/useController";

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
const items: Item[] = [
  { kind: "user", id: "u1", text: "update it" },
  { kind: "assistant", id: "a1", text: "Updated.", reasoning: "", streaming: false },
  {
    kind: "notice",
    id: "q1",
    level: "warn",
    variant: "completion",
    title: "This turn still needs attention",
    text: "One or more checks did not pass. Review the changes before continuing.",
    action: "open_changes",
  },
];

try {
  await harness.render(items, { running: false, onOpenChanges: () => { opens += 1; } });
  ok(harness.container.textContent?.includes("This turn still needs attention"), "actionable summary stays visible outside the process fold");
  ok(!harness.container.textContent?.includes("balanced"), "compact notice exposes no internal enum values");
  const button = Array.from(harness.container.querySelectorAll("button")).find((node) => node.textContent?.includes("View changes"));
  ok(button, "completion notice offers a View changes action");
  button?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  await harness.flush();
  ok(opens === 1, "View changes delegates to the workspace panel action");
} finally {
  await harness.unmount();
  await harness.close();
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
