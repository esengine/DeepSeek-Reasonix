// Run: tsx src/__tests__/edit-replay.test.ts

import { replaySubmitText } from "../lib/editReplay";

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

console.log("\nedit replay");

eq(
  replaySubmitText("hidden session context\nvisible prompt", "visible prompt", "visible prompt", "visible prompt"),
  "hidden session context\nvisible prompt",
  "unchanged edits preserve the original submitted text",
);

eq(
  replaySubmitText("hidden session context\nvisible prompt @.reasonix/attachments/a.png", "visible prompt @[a.png](.reasonix/attachments/a.png)", "updated prompt @[a.png](.reasonix/attachments/a.png)", "updated prompt @.reasonix/attachments/a.png"),
  "hidden session context\nupdated prompt @.reasonix/attachments/a.png",
  "edited visible text preserves submit-only prefix and raw attachment refs",
);

eq(
  replaySubmitText(undefined, "visible prompt", "updated prompt", "updated prompt"),
  "updated prompt",
  "messages without hidden submit context use the rebuilt submit text",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
