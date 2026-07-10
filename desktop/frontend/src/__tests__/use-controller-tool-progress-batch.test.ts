// Run: tsx src/__tests__/use-controller-tool-progress-batch.test.ts

import { mergeToolProgressEvents } from "../lib/useController";
import type { WireEvent } from "../lib/types";

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

function progress(id: string, output: string): WireEvent {
  return { kind: "tool_progress", tool: { id, name: "shell", output, readOnly: true } };
}

console.log("\nuseController tool progress batching");

const sameTool = mergeToolProgressEvents([
  { tabId: "tab-a", e: progress("tool-1", "a") },
  { tabId: "tab-a", e: progress("tool-1", "b") },
  { tabId: "tab-a", e: progress("tool-1", "c") },
]);
eq(sameTool.map((item) => item.e.tool?.output), ["abc"], "same-frame progress chunks merge per tab and tool");

const separateKeys = mergeToolProgressEvents([
  { tabId: "tab-a", e: progress("tool-1", "a") },
  { tabId: "tab-b", e: progress("tool-1", "b") },
  { tabId: "tab-a", e: progress("tool-2", "c") },
]);
eq(
  separateKeys.map((item) => `${item.tabId}:${item.e.tool?.id}:${item.e.tool?.output}`),
  ["tab-a:tool-1:a", "tab-b:tool-1:b", "tab-a:tool-2:c"],
  "different tabs and tools stay separate",
);

const resultBeforeFlush = mergeToolProgressEvents([
  { tabId: "tab-a", e: progress("tool-1", "a") },
  { tabId: "tab-a", e: progress("tool-1", "b") },
]);
eq(resultBeforeFlush[0]?.e.tool?.output, "ab", "tool_result callers can flush a merged progress batch before dispatch");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
