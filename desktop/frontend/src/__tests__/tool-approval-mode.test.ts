// Run: tsx src/__tests__/tool-approval-mode.test.ts

import { restorableToolApprovalMode, toggleYoloToolApprovalMode } from "../lib/toolApprovalMode";

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

console.log("\ntool approval mode");

eq(restorableToolApprovalMode("ask"), "ask", "ask restores to ask");
eq(restorableToolApprovalMode("auto"), "auto", "auto restores to auto");
eq(restorableToolApprovalMode("yolo"), "ask", "yolo cannot be a restore base");

let next = toggleYoloToolApprovalMode("ask");
eq(next.mode, "yolo", "Ctrl+Y turns ask into yolo");
eq(next.restore, "ask", "Ctrl+Y remembers ask");

next = toggleYoloToolApprovalMode("auto");
eq(next.mode, "yolo", "Ctrl+Y turns auto into yolo");
eq(next.restore, "auto", "Ctrl+Y remembers auto");

next = toggleYoloToolApprovalMode("yolo", "auto");
eq(next.mode, "auto", "Ctrl+Y restores auto from yolo");

next = toggleYoloToolApprovalMode("yolo");
eq(next.mode, "ask", "Ctrl+Y falls back to ask when no restore base exists");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
