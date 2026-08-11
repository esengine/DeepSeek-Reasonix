// Run: tsx src/__tests__/memory-citation-visibility.test.ts

import { visibleTranscriptMemoryCitations } from "../lib/memoryCitationVisibility";
import type { MemoryCitation } from "../lib/types";

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

console.log("\nmemory citation visibility");

const compilerCitations: MemoryCitation[] = [
  { kind: "compiler_reference", source: "Memory v5", note: "evidence: bash succeeded" },
  { kind: "constraint", source: "Memory v5", note: "must_use: verify source" },
  { kind: "risk_note", source: "Memory v5", note: "risk: avoid failed strategy" },
];
eq(visibleTranscriptMemoryCitations(compilerCitations).length, 0, "compiler-only citations stay out of the transcript");

const regularCitation: MemoryCitation = { id: "mem-1", source: "MEMORY.md", lineStart: 12, note: "workflow" };
eq(visibleTranscriptMemoryCitations([regularCitation])[0]?.source, "MEMORY.md", "regular memory citations remain visible");

const mixed = visibleTranscriptMemoryCitations([...compilerCitations, regularCitation]);
eq(mixed.length, 1, "mixed citations keep only user-facing memory references");
eq(mixed[0]?.id, "mem-1", "mixed citations preserve the regular memory citation");

const compilerWithOtherSource: MemoryCitation = { kind: "compiler_reference", source: "MEMORY.md", note: "explicit memory file reference" };
eq(visibleTranscriptMemoryCitations([compilerWithOtherSource]).length, 1, "non-Memory-v5 compiler kind is not hidden by kind alone");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
