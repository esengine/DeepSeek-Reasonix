// Run: tsx src/__tests__/composer-insert-reference.test.ts

import {
  composeComposerInsertText,
  shouldParseWorkspaceInsertRequest,
} from "../components/Composer";

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
  if (actual === expected) ok(true, label);
  else ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

eq(
  shouldParseWorkspaceInsertRequest({ id: 1, text: "@a.ts" }),
  true,
  "workspace insert requests parse refs by default",
);

eq(
  shouldParseWorkspaceInsertRequest({ id: 2, text: "@a.ts:1-2", parseWorkspaceRef: false }),
  false,
  "line-range insert requests can force plain text insertion",
);

eq(
  composeComposerInsertText("", "@desktop/frontend/package.json:30-33", 0, 0, "inline").next,
  "@desktop/frontend/package.json:30-33",
  "inline inserts into an empty composer without newlines",
);

eq(
  composeComposerInsertText("look at ", "@desktop/frontend/package.json:30-33", 8, 8, "inline").next,
  "look at @desktop/frontend/package.json:30-33",
  "inline inserts at the caret without adding newlines",
);

eq(
  composeComposerInsertText("before", "@desktop/frontend/package.json:30-33", 6, 6).next,
  "before\n\n@desktop/frontend/package.json:30-33\n",
  "block spacing keeps existing separated insertion behavior",
);

if (failed > 0) {
  process.stderr.write(`composer insert reference tests failed: ${failed}\n`);
  process.exit(1);
}

process.stdout.write(`composer insert reference tests passed: ${passed}\n`);
