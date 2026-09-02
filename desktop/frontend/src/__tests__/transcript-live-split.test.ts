// Run: tsx src/__tests__/transcript-live-split.test.ts

import type { Item } from "../lib/useController";
import {
  buildTranscriptRows,
  buildTurnModels,
  EMPTY_FOLDS,
  userRowKey,
  type BuildRowsOptions,
  type TranscriptLiveFlags,
} from "../lib/transcriptRows";
import { resolveLiveTurnGrowthFloor, splitTranscriptLiveRows } from "../lib/transcriptLiveTurn";

let passed = 0;
let failed = 0;

function check(condition: unknown, label: string) {
  if (condition) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\ntranscript live-turn split");

const user = (id: string): Item => ({ kind: "user", id, text: `q-${id}` });
const assistant = (id: string, over: Partial<Extract<Item, { kind: "assistant" }>> = {}): Item => ({
  kind: "assistant",
  id,
  text: "",
  reasoning: "",
  streaming: false,
  ...over,
});

const options: BuildRowsOptions = {
  folds: EMPTY_FOLDS,
  foldPreference: "auto",
  hasOlderHistory: false,
  creationMode: false,
  turnForUser: (item) => (item.id === "u1" ? 0 : item.id === "u2" ? 1 : undefined),
};

function rowsFor(items: Item[], live?: TranscriptLiveFlags, running = false) {
  const models = buildTurnModels(items, live, running, false);
  const rows = buildTranscriptRows(models, options);
  return { models, rows };
}
const keys = (rows: readonly { key: string }[]) => rows.map((row) => row.key).join(",");

check(resolveLiveTurnGrowthFloor(3, 5, 420, null) === 420, "adding live rows carries the previous painted height into the next commit");
check(resolveLiveTurnGrowthFloor(5, 5, 420, null) === null, "steady live rows do not invent a geometry floor");
check(resolveLiveTurnGrowthFloor(5, 4, 420, 420) === 420, "an active floor survives an unrelated intermediate commit");

// ── Settled transcript: everything is history ────────────────────────────────
{
  const { models, rows } = rowsFor([
    user("u1"),
    assistant("a1", { text: "hello", reasoning: "thought", reasoningComplete: true }),
  ]);
  const split = splitTranscriptLiveRows(models, rows, undefined, false);
  check(!split.liveActive, "settled transcript has no live turn");
  check(split.liveRows.length === 0, "settled transcript contributes no live rows");
  check(split.historyRows.length === rows.length, "settled transcript keeps every row in history");
}

// ── Running turn: rows after the user message move to the live region ────────
{
  const live: TranscriptLiveFlags = { id: "a2", hasAnswerText: false, hasReasoning: true };
  const { models, rows } = rowsFor([user("u1"), assistant("a2", { streaming: true })], live, true);
  const processKey = models[0].segments[0].key;
  check(keys(rows) === `${userRowKey("u1")},ph:${processKey},r:a2`, `running turn rows look as expected (got ${keys(rows)})`);
  const split = splitTranscriptLiveRows(models, rows, "a2", true);
  check(split.liveActive, "running turn marks the live region active");
  check(keys(split.historyRows) === userRowKey("u1"), "the active turn's user message stays in history");
  check(keys(split.liveRows) === `ph:${processKey},r:a2`, "the active turn's process rows render in the live region");
}

// ── A later user message stays in visual order behind the streaming turn ─────
{
  const live: TranscriptLiveFlags = { id: "a1", hasAnswerText: true, hasReasoning: true };
  const { models, rows } = rowsFor(
    [user("u1"), assistant("a1", { streaming: true }), user("u2")],
    live,
    true,
  );
  const split = splitTranscriptLiveRows(models, rows, "a1", true);
  const processKey = models[0].segments[0].key;
  check(keys(split.historyRows) === userRowKey("u1"), "only the streaming turn's user row stays in history");
  check(
    keys(split.liveRows) === `ph:${processKey},r:a1,a:a1,${userRowKey("u2")}`,
    `rows after the live turn keep their order in the live region (got ${keys(split.liveRows)})`,
  );
}

// ── Running with no assistant item yet: status-only live region ──────────────
{
  const { models, rows } = rowsFor([user("u1")], undefined, true);
  const split = splitTranscriptLiveRows(models, rows, undefined, true);
  check(split.liveActive, "a fresh turn activates the live region before the first item");
  check(split.liveRows.length === 0, "a fresh turn has no live rows yet");
  check(keys(split.historyRows) === userRowKey("u1"), "a fresh turn keeps its user message in history");
}

// ── Lingering live stream without running still splits ───────────────────────
{
  const live: TranscriptLiveFlags = { id: "a1", hasAnswerText: true, hasReasoning: false };
  const { models, rows } = rowsFor([user("u1"), assistant("a1", { streaming: true })], live, false);
  const split = splitTranscriptLiveRows(models, rows, "a1", false);
  check(split.liveActive, "lingering live stream keeps the live region active");
  check(keys(split.historyRows) === userRowKey("u1"), "lingering live stream keeps the user row in history");
  check(keys(split.liveRows) === "a:a1", "lingering live answer renders in the live region");
}

// ── Unknown live id without running: no split ────────────────────────────────
{
  const { models, rows } = rowsFor([user("u1"), assistant("a1", { text: "done", reasoningComplete: true })]);
  const split = splitTranscriptLiveRows(models, rows, "missing", false);
  check(!split.liveActive && split.liveRows.length === 0, "an unknown live id splits nothing");
  check(split.historyRows.length === rows.length, "an unknown live id keeps all rows in history");
}

// ── Active prelude turn (no user message): split at the turn's first row ─────
{
  const tool: Item = { kind: "tool", id: "t1", name: "read_file", args: "{}", readOnly: true, status: "running" };
  const { models, rows } = rowsFor([tool], undefined, true);
  const split = splitTranscriptLiveRows(models, rows, undefined, true);
  check(split.liveActive, "a running prelude turn activates the live region");
  check(split.historyRows.length === 0, "a prelude turn leaves no history rows");
  check(keys(split.liveRows) === keys(rows), "a prelude turn's rows all render in the live region");
}

if (failed > 0) {
  console.error(`\n${failed} transcript live-split test(s) failed; ${passed} passed.`);
  process.exit(1);
}
console.log(`\n${passed} transcript live-split tests passed.`);
