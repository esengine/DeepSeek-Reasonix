// Run: tsx src/__tests__/message-pasted-blocks.test.ts

import { parsePastedBlocks, parseSelectedTextBlocks } from "../components/Message";
import { formatSelectedTextContext, formatSelectionLabel, type SelectedTextReference } from "../lib/selectedTextContext";

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

const repeatedPrefix = "same prefix selected text that is deliberately longer than forty characters";
const selections: SelectedTextReference[] = [
  { id: "chat-1", text: `${repeatedPrefix} first` },
  { id: "chat-2", text: `${repeatedPrefix} second` },
  { id: "code-1", path: "src/a]b.ts", text: "if (ready) {\n  run_task();\n}" },
];
const labels = selections.map(formatSelectionLabel);
const authoredLabel = labels[0];
const display = `compare ${authoredLabel}\n${labels.join(" ")}`;
const selectedBlocks = parseSelectedTextBlocks(display, formatSelectedTextContext(selections));

eq(labels[2].includes("a］b.ts"), true, "selection labels sanitize closing brackets in file names");
eq(
  selectedBlocks.map(({ label, content, path, kind }) => ({ label, content, path, kind })),
  [
    { label: labels[0], content: `${repeatedPrefix} first`, kind: "chat" },
    { label: labels[1], content: `${repeatedPrefix} second`, kind: "chat" },
    { label: labels[2], content: "if (ready) {\n  run_task();\n}", path: "src/a]b.ts", kind: "code" },
  ],
  "selection cards recover duplicate snippets and bracketed file names from the existing JSON context",
);
eq(selectedBlocks[0]?.start, display.indexOf(labels[0], display.indexOf("\n") + 1), "label-shaped authored prose is not consumed as a selection card");
const unterminatedDisplay = `explain literal [Chat: ${labels.join(" ")}`;
let expectedLabelStart = unterminatedDisplay.length - labels.join(" ").length;
const expectedTrailingLabels = labels.map((label) => {
  const expected = { label, start: expectedLabelStart };
  expectedLabelStart += label.length + 1;
  return expected;
});
eq(
  parseSelectedTextBlocks(unterminatedDisplay, formatSelectedTextContext(selections)).map(({ label, start }) => ({ label, start })),
  expectedTrailingLabels,
  "unterminated label-shaped prose cannot consume the exact trailing selection labels",
);
eq(parsePastedBlocks(labels.join(" "), formatSelectedTextContext(selections)), [], "selection labels no longer use pasted-text marker parsing");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
