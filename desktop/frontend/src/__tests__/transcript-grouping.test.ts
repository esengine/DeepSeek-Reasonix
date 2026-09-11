// Run: tsx src/__tests__/transcript-grouping.test.ts

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";
import { buildStepGroups, buildTurnGroups, lastQuestionTurn, questionTurnsById } from "../lib/transcriptGrouping";
import type { Item } from "../lib/useController";

let passed = 0;
let failed = 0;

function ok(cond: boolean, label: string) {
  if (cond) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq<T>(actual: T, expected: T, label: string) {
  if (actual === expected) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

function syntheticTranscriptItems(turns: number, toolsPerTurn: number): Item[] {
  const items: Item[] = [];
  let seq = 0;
  for (let turn = 0; turn < turns; turn += 1) {
    items.push({ kind: "user", id: `u${seq++}`, text: `prompt ${turn}` });
    items.push({ kind: "assistant", id: `a${seq++}`, text: `answer ${turn}`, reasoning: "", streaming: false });
    for (let tool = 0; tool < toolsPerTurn; tool += 1) {
      items.push({
        kind: "tool",
        id: `t${seq++}`,
        name: "bash",
        args: "",
        readOnly: false,
        status: "done",
        dataArchived: true,
      });
    }
  }
  return items;
}

console.log("\ntranscript grouping contract");

{
  const here = dirname(fileURLToPath(import.meta.url));
  const groupingPath = resolve(here, "../lib/transcriptGrouping.ts");
  const source = readFileSync(groupingPath, "utf8");
  ok(!source.includes(".findIndex("), "turn grouping does not scan a second collection for each item");
}

{
  const groups = buildTurnGroups(syntheticTranscriptItems(3, 2));
  eq(groups.length, 3, "creates one group per user turn");
  eq(groups[0].startIdx, 0, "first group start index");
  eq(groups[0].endIdx, 4, "first group end index");
  eq(groups[0].toolCount, 2, "counts top-level tools in a turn");
  eq(groups[2].assistantPreview, "answer 2", "keeps latest assistant preview for each turn");
}

{
  const groups = buildStepGroups([
    { kind: "user", id: "u0", text: "fix this" },
    { kind: "assistant", id: "a1", text: "visible answer before retry", reasoning: "", streaming: false },
    { kind: "notice", id: "s1", level: "info", text: "↪ steer" },
    { kind: "assistant", id: "a2", text: "", reasoning: "", streaming: true },
  ] as Item[]);
  eq(groups[1]?.isFinal, true, "visible assistant text before a later assistant stays outside processed folds");
}

{
  const groups = buildStepGroups([
    { kind: "user", id: "u0", text: "use tools" },
    { kind: "assistant", id: "a1", text: "", reasoning: "", streaming: false },
    { kind: "tool", id: "t1", name: "read_file", args: "{}", readOnly: true, status: "done" },
    { kind: "assistant", id: "a2", text: "final answer", reasoning: "", streaming: false },
  ] as Item[]);
  eq(groups[1]?.isFinal, false, "tool-only completed steps still fold in compact mode");
  eq(groups[2]?.isFinal, true, "later visible final answer renders directly");
}

{
  const visibleTurns = questionTurnsById([
    { id: "u0", text: "first", turn: 0 },
    { id: "u1", text: "second", turn: 1 },
  ]);
  eq(visibleTurns.get("u0"), 0, "falls back to visible ordinal when no checkpoint turns exist");
  eq(visibleTurns.get("u1"), 1, "visible ordinal fallback increments by question");

  const backendTurns = questionTurnsById([
    { id: "u0", text: "first", turn: 0, checkpointTurn: 0 },
    { id: "u1", text: "live without server stamp yet", turn: 1 },
    { id: "u2", text: "after hidden synthetic", turn: 2, checkpointTurn: 3 },
  ]);
  eq(backendTurns.get("u0"), 0, "uses backend checkpoint turn zero when present");
  eq(backendTurns.get("u2"), 3, "uses non-contiguous backend checkpoint turn");
  ok(!backendTurns.has("u1"), "does not mix visible ordinal fallback into authoritative checkpoint sessions");
  eq(lastQuestionTurn([
    { id: "u0", text: "first", turn: 0 },
    { id: "u1", text: "second", turn: 1 },
  ], visibleTurns), 1, "last question turn follows visible ordinal fallback");
  eq(lastQuestionTurn([
    { id: "u0", text: "first", turn: 0, checkpointTurn: 0 },
    { id: "u1", text: "live without server stamp yet", turn: 1 },
    { id: "u2", text: "after hidden synthetic", turn: 2, checkpointTurn: 3 },
  ], backendTurns), 3, "last question turn follows non-contiguous backend turn");

  const pagedTurns = questionTurnsById([
    { id: "u-recent", text: "recent prompt", turn: 0, checkpointTurn: 1060 },
  ]);
  eq(lastQuestionTurn([
    { id: "u-recent", text: "recent prompt", turn: 0, checkpointTurn: 1060 },
  ], pagedTurns), 1060, "last question turn supports paged history windows");
}

{
  const items = syntheticTranscriptItems(10_000, 1);
  const start = performance.now();
  const groups = buildTurnGroups(items);
  const elapsed = performance.now() - start;
  eq(groups.length, 10_000, "large transcript grouping keeps every turn");
  ok(elapsed < 50, `groups 10k turns in ${elapsed.toFixed(2)}ms`);
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
