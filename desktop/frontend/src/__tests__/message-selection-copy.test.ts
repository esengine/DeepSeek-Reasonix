// Run: tsx src/__tests__/message-selection-copy.test.ts

import { messageSelectionCopyText } from "../lib/messageSelectionCopy";

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

console.log("\nmessage selection copy");

eq(
  messageSelectionCopyText({
    text: "selected assistant reply",
    isCollapsed: false,
    targetIsEditable: false,
    intersectsMessage: true,
    canWriteClipboard: true,
  }),
  "selected assistant reply",
  "copies selected message text through the fallback handler",
);

eq(
  messageSelectionCopyText({
    text: "draft text",
    isCollapsed: false,
    targetIsEditable: true,
    intersectsMessage: true,
    canWriteClipboard: true,
  }),
  null,
  "does not override native copy inside editable fields",
);

eq(
  messageSelectionCopyText({
    text: "   \n\t",
    isCollapsed: false,
    targetIsEditable: false,
    intersectsMessage: true,
    canWriteClipboard: true,
  }),
  null,
  "ignores whitespace-only selections",
);

eq(
  messageSelectionCopyText({
    text: "settings panel text",
    isCollapsed: false,
    targetIsEditable: false,
    intersectsMessage: false,
    canWriteClipboard: true,
  }),
  null,
  "leaves non-message selections to the browser",
);

eq(
  messageSelectionCopyText({
    text: "selected assistant reply",
    isCollapsed: true,
    targetIsEditable: false,
    intersectsMessage: true,
    canWriteClipboard: true,
  }),
  null,
  "ignores collapsed selections",
);

eq(
  messageSelectionCopyText({
    text: "selected assistant reply",
    isCollapsed: false,
    targetIsEditable: false,
    intersectsMessage: true,
    canWriteClipboard: false,
  }),
  null,
  "does not claim copy events without writable clipboard data",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
