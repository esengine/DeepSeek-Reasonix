// Run: tsx src/__tests__/bridge-mock-approval-mode.test.ts

import { mockToolApprovalModeAfterModeChange } from "../lib/bridge";

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

console.log("\nbridge mock approval mode");

eq(
  mockToolApprovalModeAfterModeChange("auto", "plan"),
  "auto",
  "legacy plan switch keeps explicit auto approval mode",
);
eq(
  mockToolApprovalModeAfterModeChange("auto", "normal"),
  "auto",
  "legacy normal switch keeps explicit auto approval mode",
);
eq(
  mockToolApprovalModeAfterModeChange("ask", "yolo"),
  "yolo",
  "legacy yolo switch still enables yolo approval mode",
);
eq(
  mockToolApprovalModeAfterModeChange("yolo", "plan"),
  "ask",
  "legacy non-yolo switch clears yolo back to ask",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
