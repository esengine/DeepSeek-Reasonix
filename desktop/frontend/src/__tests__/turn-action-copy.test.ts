// Run: tsx src/__tests__/turn-action-copy.test.ts

import { appendTurnActionCopyText } from "../lib/turnActionCopy";

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

console.log("\nturn action copy");

eq(
  appendTurnActionCopyText("", "final answer"),
  "final answer",
  "keeps single assistant output unchanged",
);

eq(
  appendTurnActionCopyText("first assistant block", "second assistant block"),
  "first assistant block\n\nsecond assistant block",
  "accumulates multiple assistant outputs in one turn",
);

eq(
  appendTurnActionCopyText("first assistant block\n", "second assistant block"),
  "first assistant block\nsecond assistant block",
  "does not add an extra blank line when the previous block already ends with newline",
);

eq(
  appendTurnActionCopyText("first assistant block", "\nsecond assistant block"),
  "first assistant block\nsecond assistant block",
  "does not add an extra blank line when the next block starts with newline",
);

eq(
  appendTurnActionCopyText("final answer", "   \n\t"),
  "final answer",
  "ignores whitespace-only assistant fragments",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
