// Run: tsx src/__tests__/code-block-toolbar.test.ts

import { buildMarkdownCodeBlock } from "../components/CodeBlockToolbar";

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

console.log("\ncode block toolbar");

eq(
  buildMarkdownCodeBlock("console.log(1);\n", "ts"),
  "```ts\nconsole.log(1);\n```\n",
  "uses a standard fence for ordinary snippets",
);

eq(
  buildMarkdownCodeBlock("before\n```md\ninside\n```\nafter", "markdown"),
  "````markdown\nbefore\n```md\ninside\n```\nafter\n````\n",
  "uses a longer fence when the snippet contains triple backticks",
);

eq(
  buildMarkdownCodeBlock("````\ncontent", ""),
  "`````\n````\ncontent\n`````\n",
  "uses a fence longer than the longest backtick run",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
