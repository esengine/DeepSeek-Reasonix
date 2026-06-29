// Run: tsx src/__tests__/transcript-grouping.test.ts

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";
import { buildTurnGroups, createWarmLayerState, questionTurnsById, warmColdPageForTurn, warmLayerForSession, warmLayerWithColdPageAtLeast, warmLayerWithExpandedTurn, warmLayerWithNextColdPage, warmPagination } from "../lib/transcriptGrouping";
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
}

{
  const items = syntheticTranscriptItems(10_000, 1);
  const start = performance.now();
  const groups = buildTurnGroups(items);
  const elapsed = performance.now() - start;
  eq(groups.length, 10_000, "large transcript grouping keeps every turn");
  ok(elapsed < 50, `groups 10k turns in ${elapsed.toFixed(2)}ms`);
}

{
  const firstPage = warmPagination({ turnCount: 100, hotTurns: 30, pageSize: 20, coldPage: 0 });
  eq(firstPage.warmStartTurn, 50, "long transcripts initially render only the latest warm page");
  eq(firstPage.warmEndTurn, 70, "warm page stops before the hot zone");
  eq(firstPage.coldTurnCount, 50, "older cold turns stay hidden behind the load-more button");

  const secondPage = warmPagination({ turnCount: 100, hotTurns: 30, pageSize: 20, coldPage: 1 });
  eq(secondPage.warmStartTurn, 30, "loading earlier history adds one more warm page");
  eq(secondPage.warmEndTurn, 70, "loading earlier history keeps the hot-zone boundary stable");
  eq(secondPage.coldTurnCount, 30, "loading earlier history reduces the hidden cold count");

  const shortTranscript = warmPagination({ turnCount: 25, hotTurns: 30, pageSize: 20, coldPage: 0 });
  eq(shortTranscript.warmStartTurn, 0, "short transcripts have no warm zone");
  eq(shortTranscript.warmEndTurn, 0, "short transcripts have no warm boundary");
  eq(shortTranscript.coldTurnCount, 0, "short transcripts have no cold turns");
}

{
  eq(warmColdPageForTurn({ turn: 10, turnCount: 100, hotTurns: 30, pageSize: 20 }), 2, "jumping to cold-zone turn 10 loads enough warm pages");
  eq(warmColdPageForTurn({ turn: 0, turnCount: 100, hotTurns: 30, pageSize: 20 }), 3, "jumping to the first warm turn loads all warm pages");
  eq(warmColdPageForTurn({ turn: 65, turnCount: 100, hotTurns: 30, pageSize: 20 }), 0, "jumping inside the initial warm page needs no extra cold page");
  eq(warmColdPageForTurn({ turn: 80, turnCount: 100, hotTurns: 30, pageSize: 20 }), 0, "jumping inside the hot zone needs no warm pagination");
}

{
  let state = createWarmLayerState("tab-a|0|a-u0");
  state = warmLayerWithNextColdPage(state, "tab-a|0|a-u0");
  state = warmLayerWithNextColdPage(state, "tab-a|0|a-u0");
  state = warmLayerWithNextColdPage(state, "tab-a|0|a-u0");
  state = warmLayerWithExpandedTurn(state, "tab-a|0|a-u0", 10, true);
  eq(state.coldPage, 3, "loading earlier history advances the current session page");
  ok(state.expandedWarmTurns.has(10), "expanded warm turns stay scoped to the current session");

  const switched = warmLayerForSession(state, "tab-b|1|b-u0");
  eq(switched.coldPage, 0, "switching sessions resets warm pagination before rendering");
  eq(switched.expandedWarmTurns.size, 0, "switching sessions clears expanded warm turns");

  const switchedPage = warmPagination({ turnCount: 100, hotTurns: 30, pageSize: 20, coldPage: switched.coldPage });
  eq(switchedPage.warmStartTurn, 50, "switched long transcripts render only the latest warm page first");

  const paged = warmLayerWithColdPageAtLeast(switched, "tab-b|1|b-u0", 2);
  eq(paged.coldPage, 2, "jumping to a cold-zone question raises the session warm page");
  eq(warmLayerWithColdPageAtLeast(paged, "tab-b|1|b-u0", 1).coldPage, 2, "jumping never lowers the loaded warm page");
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
