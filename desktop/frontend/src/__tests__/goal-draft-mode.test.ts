// Run: tsx src/__tests__/goal-draft-mode.test.ts

import {
  controllerCollaborationMode,
  displayedCollaborationMode,
  keepGoalDraftMode,
  metaSyncedCollaborationMode,
  tabListCollaborationMode,
} from "../lib/goalDraftMode";

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

console.log("\ngoal draft mode");

eq(
  displayedCollaborationMode({ goalDraftMode: true, localMode: "normal", goal: "" }),
  "goal",
  "draft goal mode wins over stale local normal mode",
);

eq(
  tabListCollaborationMode({ goalDraftMode: true, tabMode: "normal", tabGoal: "" }),
  "goal",
  "tab list keeps draft goal mode visible before a goal is started",
);

eq(
  metaSyncedCollaborationMode({ nextGoal: "", goalDraftMode: true, legacyMode: "normal" }),
  "goal",
  "empty controller meta does not collapse a draft goal mode",
);

eq(
  controllerCollaborationMode({ collaborationMode: "goal", goal: "" }),
  "normal",
  "empty draft goal syncs to the controller as normal mode",
);

eq(
  controllerCollaborationMode({ collaborationMode: "goal", goal: "ship the fix" }),
  "goal",
  "started goal syncs to the controller as goal mode",
);

eq(
  keepGoalDraftMode(true, ""),
  true,
  "draft flag is retained while goal text is empty",
);

eq(
  keepGoalDraftMode(true, "ship the fix"),
  false,
  "draft flag clears after a real goal exists",
);

eq(
  metaSyncedCollaborationMode({ nextGoal: "", goalDraftMode: false, legacyMode: "plan" }),
  "plan",
  "non-draft empty goal falls back to legacy plan mode",
);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
