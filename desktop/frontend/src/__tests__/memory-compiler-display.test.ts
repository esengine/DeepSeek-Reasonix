// Run: tsx src/__tests__/memory-compiler-display.test.ts
//
// Display-boundary safety net for #5361: a corrupted/accreted Memory v5 contract
// from the pre-fix goal loop (#5342) must never render as raw JSON "乱码".

import { stripMemoryCompilerExecution } from "../lib/memoryCompilerDisplay";

let passed = 0;
let failed = 0;

function ok(cond: boolean, label: string) {
  if (cond) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

const contract = (sourceEvent: string) =>
  "<memory-compiler-execution>\n" +
  JSON.stringify({ type: "memory_v5_execution_contract", planner_ir: { version: 5, source_event: sourceEvent } }) +
  "\n</memory-compiler-execution>";

console.log("\nmemory compiler display strip");

ok(stripMemoryCompilerExecution("fix the login bug") === "fix the login bug", "leaves plain user text untouched");

const complete = stripMemoryCompilerExecution(contract("do the thing"));
ok(!complete.includes("memory-compiler-execution"), "removes a complete contract block");

const withText = stripMemoryCompilerExecution("hello\n" + contract("hello"));
ok(withText.includes("hello") && !withText.includes("<memory-compiler-execution>"), "removes a complete block after user text");

const partial = 'keep this\n<memory-compiler-execution>\n{"planner_ir":{"source_event":"keep this",' + "x".repeat(40);
const cut = stripMemoryCompilerExecution(partial);
ok(
  cut.includes("keep this") && !cut.includes("<memory-compiler-execution>") && !cut.includes("planner_ir"),
  "cuts a dangling/truncated block instead of leaking raw JSON",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
