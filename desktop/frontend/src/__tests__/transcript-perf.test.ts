// Run: tsx src/__tests__/transcript-perf.test.ts

import { DEFAULT_HOT_TURNS, DEFAULT_WARM_PAGE_SIZE, transcriptLayerBudget } from "../lib/transcriptPerf";
import type { Item } from "../lib/useController";

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

function tool(id: number, output = ""): Item {
  return {
    kind: "tool",
    id: `t${id}`,
    name: "read_file",
    args: "{}",
    readOnly: true,
    status: "done",
    output,
  };
}

console.log("\ntranscript perf");

eq(transcriptLayerBudget([]), { hotTurns: DEFAULT_HOT_TURNS, pageSize: DEFAULT_WARM_PAGE_SIZE }, "small transcript keeps default budget");
eq(transcriptLayerBudget(Array.from({ length: 80 }, (_, i) => tool(i))), { hotTurns: 22, pageSize: 16 }, "many tool cards tighten the hot zone");
eq(transcriptLayerBudget([tool(1, "x".repeat(900_001))]), { hotTurns: 10, pageSize: 10 }, "huge output uses the smallest hot zone");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

