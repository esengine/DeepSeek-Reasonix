// Run: tsx src/__tests__/reasoning-summary.test.ts

import { reasoningSummaryText } from "../lib/reasoningSummary";

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

console.log("\nreasoning summary");

eq(
  reasoningSummaryText("first thought\n\nsecond thought", { streaming: false }),
  "first thought",
  "completed reasoning summarizes to the first non-blank line",
);

eq(
  reasoningSummaryText("first thought\n\nlatest thought", { streaming: true }),
  "latest thought",
  "streaming reasoning summarizes to the last non-blank line",
);

eq(
  reasoningSummaryText("\n\n  \nfirst thought\nrest", { streaming: false }),
  "first thought",
  "leading blank lines are skipped when completed",
);

eq(
  reasoningSummaryText("first thought\n\nlatest thought\n\n\n", { streaming: true }),
  "latest thought",
  "trailing blank lines are skipped while streaming",
);

eq(
  reasoningSummaryText("first thought\n\nlatest thought\n  \t  ", { streaming: true }),
  "latest thought",
  "trailing whitespace-only lines are skipped while streaming",
);

eq(
  reasoningSummaryText("first thought\r\n\r\nsecond thought\r\n", { streaming: false }),
  "first thought",
  "handles CRLF line endings when completed",
);

eq(
  reasoningSummaryText("first thought\r\n\r\nlatest thought\r\n", { streaming: true }),
  "latest thought",
  "handles CRLF line endings while streaming",
);

eq(
  reasoningSummaryText("  spaced   out \t line  \nnext", { streaming: false }),
  "spaced out line",
  "collapses intra-line whitespace",
);

eq(reasoningSummaryText("", { streaming: false }), "", "empty reasoning yields an empty summary");
eq(reasoningSummaryText(" \n \r\n \n", { streaming: true }), "", "blank-only reasoning yields an empty summary");

const longLine = "word ".repeat(100).trim();
eq(
  reasoningSummaryText(longLine, { streaming: false }).length <= 180,
  true,
  "summaries are bounded to the default character budget",
);
eq(
  reasoningSummaryText(longLine, { streaming: false, maxChars: 10 }),
  "word word…",
  "honors a custom character budget",
);

const streamingTail = `${"a".repeat(220)}LATEST_TOKEN`;
eq(
  reasoningSummaryText(streamingTail, { streaming: true }),
  `…${"a".repeat(167)}LATEST_TOKEN`,
  "long streaming lines retain the newest text within the character budget",
);
eq(
  reasoningSummaryText("abcdef", { streaming: false, maxChars: 0 }),
  "",
  "a zero character budget yields an empty summary",
);
eq(
  reasoningSummaryText("abcdef", { streaming: false, maxChars: -1 }),
  "",
  "a negative character budget clamps to zero",
);
eq(
  reasoningSummaryText("abcdef", { streaming: false, maxChars: 1 }),
  "…",
  "a one character budget contains only the truncation marker",
);

const emojiLine = "🙂".repeat(200);
const emojiSummary = reasoningSummaryText(emojiLine, { streaming: false });
eq(
  emojiSummary,
  `${"🙂".repeat(179)}…`,
  "truncation never splits a surrogate pair",
);

// The summary scans from one end of the string instead of splitting the whole
// input into a line array — long streaming reasoning must not allocate one
// per token. Forbid split() outright and run a large input through it.
const huge = `${Array.from({ length: 20_000 }, (_, i) => `line ${i}`).join("\n")}\n\ntail line\n`;
const originalSplit = String.prototype.split;
String.prototype.split = (() => {
  throw new Error("split() must not be used");
}) as typeof String.prototype.split;
try {
  eq(reasoningSummaryText(huge, { streaming: false }), "line 0", "completed summary does not split the whole input");
  eq(reasoningSummaryText(huge, { streaming: true }), "tail line", "streaming summary does not split the whole input");
} finally {
  String.prototype.split = originalSplit;
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
