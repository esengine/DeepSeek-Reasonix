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
  "workspace-write",
  "legacy auto migrates to workspace-write during a plan switch",
);
eq(
  mockToolApprovalModeAfterModeChange("auto", "normal"),
  "workspace-write",
  "legacy auto migrates to workspace-write during a normal switch",
);
eq(
  mockToolApprovalModeAfterModeChange("ask", "yolo"),
  "workspace-write",
  "legacy yolo mode migrates conservatively to workspace-write",
);
eq(
  mockToolApprovalModeAfterModeChange("yolo", "plan"),
  "workspace-write",
  "legacy yolo state never silently enables full access",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
