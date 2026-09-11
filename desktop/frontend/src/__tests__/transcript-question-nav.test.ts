// Run: node --import tsx src/__tests__/transcript-question-nav.test.ts

import { activeQuestionTurn, type QuestionAnchorPosition } from "../lib/transcriptGrouping";

let passed = 0;
let failed = 0;

function equal(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

console.log("\ntranscript question navigation");

equal(
  activeQuestionTurn([
    { turn: 0, top: -520 },
    { turn: 1, top: -36 },
    { turn: 2, top: 240 },
  ] satisfies QuestionAnchorPosition[], 0),
  1,
  "active question follows the last anchor above the viewport",
);

equal(
  activeQuestionTurn([
    { turn: 1, top: 28 },
    { turn: 2, top: 300 },
  ] satisfies QuestionAnchorPosition[], 0),
  1,
  "active question falls back to the first mounted anchor at the top boundary",
);

equal(
  activeQuestionTurn([{ turn: 2, top: -12 }] satisfies QuestionAnchorPosition[], 0),
  2,
  "virtualized scrolling can update from the mounted question window",
);

equal(activeQuestionTurn([], 0), undefined, "missing anchors leave the active question unchanged");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
