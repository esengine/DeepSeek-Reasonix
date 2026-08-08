// Run: tsx src/__tests__/reasoning-display.test.ts

import { displayReasoningText } from "../lib/reasoningDisplay";

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

console.log("\nreasoning display");

eq(
  displayReasoningText("a\nb\nc", { streaming: false, maxLines: 2 }),
  "a\nb\nc",
  "keeps completed reasoning intact",
);

eq(
  displayReasoningText("a\nb\nc", { streaming: true, maxLines: 2 }),
  "...\nb\nc",
  "keeps only the tail lines while streaming",
);

eq(
  displayReasoningText("abcdef", { streaming: true, maxChars: 3, maxLines: 10 }),
  "...\ndef",
  "keeps only the tail characters while streaming",
);

eq(
  displayReasoningText("abcdef", { streaming: true, truncateStreaming: false, maxChars: 3 }),
  "abcdef",
  "can opt out of streaming truncation",
);

eq(
  displayReasoningText("abcdefgh", { streaming: true, maxChars: 4, maxLines: 10, stableWindowChars: 3 }),
  "...\ndefgh",
  "keeps a stable append-only character window between coarse rebases",
);

eq(
  displayReasoningText("a\nb\nc\nd\ne", { streaming: true, maxChars: 100, maxLines: 2, stableWindowLines: 2 }),
  "...\nc\nd\ne",
  "keeps a stable append-only line window between coarse rebases",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
