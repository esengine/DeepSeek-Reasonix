// Run: tsx src/__tests__/btw-command.test.ts

import { isBtwCommand, parseBtwCommandInput } from "../lib/btwCommand";

let passed = 0;
let failed = 0;

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

console.log("\nBTW command parsing");

eq(parseBtwCommandInput("/btw"), "", "bare /btw opens the desktop surface");
eq(parseBtwCommandInput("  /btw explain this  "), "explain this", "/btw preserves its question");
eq(parseBtwCommandInput("/btw\ncompare both options"), "compare both options", "/btw accepts a question after whitespace");
eq(parseBtwCommandInput("/btwx"), null, "similar command names do not trigger BTW");
eq(isBtwCommand("/btw while the main turn runs"), true, "/btw is recognized for immediate running-turn dispatch");
eq(isBtwCommand("keep working"), false, "ordinary guidance stays in the main-turn queue");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
