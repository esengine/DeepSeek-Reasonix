// Run: tsx src/__tests__/turn-timeline.test.ts

import { turnTimelineRows } from "../components/ContextPanel";
import { estimateToolQueueMs, toolDurationSeverity, turnElapsedMs } from "../lib/turnTimeline";
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

function ok(condition: boolean, label: string) {
  if (condition) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

const t = (key: string, vars?: Record<string, unknown>) => {
  if (key === "context.timelineTool") return `Tool ${vars?.name}`;
  if (key === "context.timelineToolValue") return `queue ${vars?.queue} run ${vars?.exec} total ${vars?.total}`;
  return key;
};

console.log("\nturn timeline");

eq(toolDurationSeverity(9999), "normal", "under 10s is normal");
eq(toolDurationSeverity(10_000), "slow", "10s enters slow tier");
eq(toolDurationSeverity(30_000), "very-slow", "30s enters strong tier");
eq(toolDurationSeverity(60_000), "action", "60s enters action tier");

eq(estimateToolQueueMs({ dispatchedAt: 1000, completedAt: 6200, durationMs: 5000 }), 200, "queue time subtracts backend execution duration from wall time");
eq(turnElapsedMs({ turnStartedAt: 1000, turnDoneAt: 3500 }), 2500, "turn elapsed uses turn_done");

const tools: Item[] = [
  {
    kind: "tool",
    id: "slow",
    name: "bash",
    args: "",
    readOnly: true,
    status: "done",
    durationMs: 31_000,
    dispatchedAt: 2_000,
    completedAt: 34_500,
  },
];
const rows = turnTimelineRows(
  { turnStartedAt: 1_000, firstTokenAt: 1_900, messageDoneAt: 40_000, contextRefreshStartedAt: 41_000, contextRefreshDoneAt: 41_250 },
  tools,
  t as never,
);
ok(rows.some((row) => row.key === "first-token" && row.value === "900 ms"), "timeline includes first token latency");
ok(rows.some((row) => row.key === "tool-slow" && row.tone === "warn"), "very slow tools get warning tone");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);

