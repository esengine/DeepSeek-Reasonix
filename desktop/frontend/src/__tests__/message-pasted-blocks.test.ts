// Run: tsx src/__tests__/message-pasted-blocks.test.ts

import { parsePastedBlocks } from "../components/Message";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

function wrapped(label: string, content: string): string {
  return `${label}\n\n--- Begin ${label} ---\n${content}\n--- End ${label} ---`;
}

console.log("\nmessage pasted blocks");

for (const [label, name] of [
  ["[已粘贴文本 #2 · 31 行]", "Simplified Chinese"],
  ["[已貼上文字 #2 · 31 行]", "Traditional Chinese"],
  ["[Pasted text #2 · 31 lines]", "English"],
] as const) {
  eq(
    parsePastedBlocks(`before\n${label}\nafter`, wrapped(label, "line 1\nline 2")),
    [{ label, content: "line 1\nline 2\n" }],
    `${name} pasted text labels are parsed from submit text`,
  );
}

eq(parsePastedBlocks("[unknown paste #1]", "--- Begin [unknown paste #1] ---\nnope\n--- End [unknown paste #1] ---"), [], "unknown labels are ignored");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
