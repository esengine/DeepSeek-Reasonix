// Run: tsx src/__tests__/workspace-line-reference.test.ts

import {
  formatWorkspaceLineReference,
  normalizeMonacoSelectionRange,
} from "../lib/workspaceLineReference";

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq(actual: unknown, expected: unknown, label: string) {
  if (JSON.stringify(actual) === JSON.stringify(expected)) ok(true, label);
  else ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

eq(formatWorkspaceLineReference("a.ts", 3, 3), "@a.ts:3", "single-line references use one line number");
eq(formatWorkspaceLineReference("a.ts", 3, 5), "@a.ts:3-5", "multi-line references use an inclusive range");

eq(
  normalizeMonacoSelectionRange({ startLineNumber: 3, startColumn: 1, endLineNumber: 5, endColumn: 9 }),
  { startLine: 3, endLine: 5 },
  "normalizes ordinary multi-line selections",
);

eq(
  normalizeMonacoSelectionRange({ startLineNumber: 3, startColumn: 1, endLineNumber: 5, endColumn: 1 }),
  { startLine: 3, endLine: 4 },
  "does not include the next line when selection ends at column 1",
);

eq(
  normalizeMonacoSelectionRange({ startLineNumber: 5, startColumn: 1, endLineNumber: 3, endColumn: 1 }),
  { startLine: 3, endLine: 4 },
  "does not include the next line when reverse selection ends at column 1",
);

eq(
  normalizeMonacoSelectionRange({ startLineNumber: 8, startColumn: 2, endLineNumber: 8, endColumn: 2 }),
  null,
  "empty selections do not create line references",
);

if (failed > 0) {
  process.stderr.write(`workspace line reference tests failed: ${failed}\n`);
  process.exit(1);
}

process.stdout.write(`workspace line reference tests passed: ${passed}\n`);
