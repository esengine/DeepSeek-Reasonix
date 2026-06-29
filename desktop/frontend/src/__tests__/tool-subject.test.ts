// Run: tsx src/__tests__/tool-subject.test.ts

import { subjectOf } from "../lib/tools";

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

console.log("\ntool subject contract");

eq(subjectOf("task", JSON.stringify({ description: "audit docs" })), "audit docs", "task subject uses description");
eq(subjectOf("run_skill", JSON.stringify({ name: "code-reviewer", arguments: "review this branch" })), "code-reviewer", "run_skill subject uses skill name");

if (failed) {
  process.stdout.write(`\n${failed} failed, ${passed} passed\n`);
  process.exit(1);
}
process.stdout.write(`\n${passed} passed\n`);
