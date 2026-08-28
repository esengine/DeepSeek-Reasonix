// Run: tsx src/__tests__/transcript-rows.test.ts
//
// Virtual row model: block-level rows replace the hot/warm/cold turn layers.
// Covers row types/counts for a fixture transcript, fold expand/collapse
// inserting and removing rows, fold-state reconciliation (auto-open while
// running, auto-close on completion, user override, preference switches), and
// the lazy-content entry id derivation.

import {
  buildTranscriptRows,
  buildTurnModels,
  defaultFoldOpen,
  estimateTranscriptRowSize,
  foldMapWithToggle,
  foldSegmentStates,
  historyEntryIdForItemId,
  historyEntryIdForRow,
  reconcileFoldEntries,
  EMPTY_FOLDS,
  type FoldMap,
  type TranscriptRow,
} from "../lib/transcriptRows";
import type { Item } from "../lib/useController";

let passed = 0;
let failed = 0;

function ok(cond: unknown, label: string) {
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

console.log("\ntranscript virtual row model");

const fixture: Item[] = [
  { kind: "user", id: "u1", text: "first" },
  { kind: "assistant", id: "a1", text: "", reasoning: "thinking", streaming: false },
  { kind: "tool", id: "t1", name: "read_file", args: "{}", readOnly: true, status: "done" },
  { kind: "tool", id: "t2", name: "read_file", args: "{}", readOnly: true, status: "done" },
  { kind: "tool", id: "t3", name: "bash", args: "{}", readOnly: false, status: "done" },
  { kind: "notice", id: "n1", level: "warn", text: "careful" },
  { kind: "assistant", id: "a2", text: "answer one", reasoning: "", streaming: false },
  { kind: "user", id: "u2", text: "second" },
  { kind: "assistant", id: "a3", text: "answer two", reasoning: "more", streaming: false },
];

{
  const current: Item[] = [
    { kind: "user", id: "u-current", text: "current" },
    { kind: "phase", id: "duplicate-phase", text: "current work" },
  ];
  const before = buildTurnModels(current)[0]?.segments[0]?.key;
  const after = buildTurnModels([
    { kind: "user", id: "u-older", text: "older" },
    { kind: "phase", id: "duplicate-phase", text: "older work" },
    ...current,
  ])[1]?.segments[0]?.key;
  eq(after, before, "prepending history with a duplicate raw id preserves the mounted segment key");
}

{
  const reasoningOnly: Item[] = [
    { kind: "user", id: "u-late-answer", text: "question" },
    { kind: "assistant", id: "a-late-answer", text: "", reasoning: "thinking", streaming: false },
  ];
  const before = buildTurnModels(reasoningOnly)[0]?.segments[0]?.key;
  const after = buildTurnModels([
    reasoningOnly[0],
    reasoningOnly[1],
    { kind: "assistant", id: "a-late-answer-visible", text: "resolved answer", reasoning: "", streaming: false },
  ])[0]?.segments[0]?.key;
  eq(after, before, "outside answer content does not remount the existing process segment");
}

{
  const currentUser: Item = { kind: "user", id: "duplicate-user", text: "current", createdAt: 200 };
  const currentAnswer: Item = { kind: "assistant", id: "duplicate-answer", text: "current answer", reasoning: "", streaming: false };
  const options = {
    folds: EMPTY_FOLDS,
    foldPreference: "auto" as const,
    hasOlderHistory: false,
    creationMode: false,
    turnForUser: () => undefined,
  };
  const before = buildTranscriptRows(buildTurnModels([currentUser, currentAnswer]), options);
  const after = buildTranscriptRows(buildTurnModels([
    { kind: "user", id: "duplicate-user", text: "older", createdAt: 100, historyTurn: 1 },
    { kind: "assistant", id: "duplicate-answer", text: "older answer", reasoning: "", streaming: false },
    currentUser,
    currentAnswer,
  ]), options);
  const beforeCurrent = before.filter((row) => row.kind === "user" || row.kind === "answer").map((row) => row.key).join(",");
  const afterCurrent = after.filter((row) =>
    (row.kind === "user" && row.item.text === "current")
    || (row.kind === "answer" && row.item.text === "current answer")
  ).map((row) => row.key).join(",");
  eq(afterCurrent, beforeCurrent, "prepending duplicate user/answer ids preserves the mounted current row keys");
  const olderKeys = after.filter((row) =>
    (row.kind === "user" && row.item.text === "older")
    || (row.kind === "answer" && row.item.text === "older answer")
  ).map((row) => row.key);
  ok(olderKeys.every((key) => key.includes("@") && !key.includes("#")), "duplicate history rows use immutable identity hashes instead of occurrence suffixes");
}

const turnOf = new Map([
  ["u1", 0],
  ["u2", 1],
  ["u9", 2],
]);
const rowOptions = (folds: FoldMap, pref: "auto" | "expanded" = "auto", hasOlderHistory = false) => ({
  folds,
  foldPreference: pref,
  hasOlderHistory,
  creationMode: false,
  turnForUser: (item: Extract<Item, { kind: "user" }>) => turnOf.get(item.id),
});
const kinds = (rows: TranscriptRow[]) => rows.map((row) => row.kind).join(",");
const keys = (rows: TranscriptRow[]) => rows.map((row) => row.key).join(",");

{
  const models = buildTurnModels(fixture);
  const rows = buildTranscriptRows(models, rowOptions(EMPTY_FOLDS));
  eq(
    kinds(rows),
    "user,process-header,notice,answer,turn-actions,user,process-header,answer,turn-actions",
    "fixture transcript flattens to block rows with collapsed folds mounting nothing",
  );
  eq(
    keys(rows),
    `u:u1,ph:${models[0].segments[0].key},n:n1,a:a2,ta:u1,u:u2,ph:${models[1].segments[0].key},a:a3,ta:u2`,
    "row keys derive from stable item ids",
  );
}

{
  const models = buildTurnModels(fixture);
  const rows = buildTranscriptRows(models, rowOptions(EMPTY_FOLDS, "auto", true));
  eq(rows[0]?.kind, "older-history", "older history paging is the first virtual row");
  eq(rows.length, 10, "older history row adds exactly one row");
}

{
  // Expanding a fold inserts its process rows into the model; collapsing
  // removes them again. Read-only tools batch; the write tool stays a card.
  const models = buildTurnModels(fixture);
  const foldKey = models[0].segments[0].key;
  let folds = EMPTY_FOLDS;
  folds = foldMapWithToggle(folds, foldKey, false);
  const openRows = buildTranscriptRows(models, rowOptions(folds));
  eq(
    kinds(openRows),
    "user,process-header,reasoning,tool-batch,tool,notice,answer,turn-actions,user,process-header,answer,turn-actions",
    "expanding the fold inserts reasoning, read-only batch, and tool rows",
  );
  const batch = openRows.find((row) => row.kind === "tool-batch");
  eq(batch && "items" in batch ? batch.items.length : 0, 2, "consecutive completed read-only tools batch into one row");
  const collapsed = buildTranscriptRows(models, rowOptions(foldMapWithToggle(folds, foldKey, true)));
  eq(kinds(collapsed), "user,process-header,notice,answer,turn-actions,user,process-header,answer,turn-actions", "collapsing removes the process rows again");
}

{
  // processFoldPreference "expanded": every fold's rows are in the model.
  const models = buildTurnModels(fixture);
  const rows = buildTranscriptRows(models, rowOptions(EMPTY_FOLDS, "expanded"));
  eq(
    kinds(rows),
    "user,process-header,reasoning,tool-batch,tool,notice,answer,turn-actions,user,process-header,reasoning,answer,turn-actions",
    "expanded preference keeps all process rows in the virtual model",
  );
}

{
  // Creation mode groups groupable tools into ToolGroup rows instead of
  // read-only batches.
  const models = buildTurnModels(fixture);
  const rows = buildTranscriptRows(models, { ...rowOptions(EMPTY_FOLDS, "expanded"), creationMode: true });
  const group = rows.find((row) => row.kind === "tool-group");
  ok(group && "items" in group && group.items.length === 2 && group.groupKind === "explore", "creation mode batches groupable read tools into a ToolGroup row");
  ok(!rows.some((row) => row.kind === "tool-batch"), "creation mode never emits read-only batches");
}

{
  const models = buildTurnModels([
    { kind: "user", id: "u-search", text: "search" },
    { kind: "tool", id: "s1", name: "web_search", args: `{"query":"bitcoin"}`, readOnly: true, status: "done" },
    { kind: "tool", id: "r1", name: "read_file", args: "{}", readOnly: true, status: "done" },
    { kind: "assistant", id: "a-search", text: "answer only", reasoning: "", streaming: false },
  ]);
  const rows = buildTranscriptRows(models, rowOptions(EMPTY_FOLDS, "expanded"));
  const searchRow = rows.find((row) => row.kind === "tool" && "item" in row && row.item.id === "s1");
  const batch = rows.find((row) => row.kind === "tool-batch");
  ok(Boolean(searchRow), "provider web search stays a standalone tool card");
  eq(batch && "items" in batch ? batch.items.map((item) => item.name).join(",") : "", "read_file", "ordinary readers still batch beside the search card");
}

{
  // A fold whose process items are all filtered out (sub-agent subcalls,
  // todo_write) renders no header row at all.
  const models = buildTurnModels([
    { kind: "user", id: "u9", text: "delegate" },
    { kind: "tool", id: "task-1", name: "task", args: "{}", readOnly: false, status: "done" },
    { kind: "tool", id: "sub-1", name: "read_file", args: "{}", readOnly: true, status: "done", parentId: "task-1" },
    { kind: "assistant", id: "a9", text: "done", reasoning: "", streaming: false },
  ]);
  const rows = buildTranscriptRows(models, rowOptions(EMPTY_FOLDS, "expanded"));
  eq(kinds(rows), "user,process-header,tool,answer,turn-actions", "sub-agent subcalls nest under their parent card, not their own rows");
}

{
  // Prelude items (before the first user message) emit rows without a user
  // row or turn actions.
  const models = buildTurnModels([
    { kind: "notice", id: "np", level: "warn", text: "early warning" },
    { kind: "user", id: "u1", text: "first" },
    { kind: "assistant", id: "a1", text: "answer", reasoning: "", streaming: false },
  ]);
  const rows = buildTranscriptRows(models, rowOptions(EMPTY_FOLDS));
  eq(kinds(rows), "notice,user,answer,turn-actions", "prelude notices render without a synthetic user row");
}

{
  const models = buildTurnModels([
    { kind: "user", id: "u1", text: "请继续完成 PPT 任务" },
    { kind: "assistant", id: "a-step", text: "下一步：安装 pptxgenjs 并确认 Pillow。", reasoning: "", streaming: false },
    { kind: "notice", id: "auto-guard", level: "info", text: "↪ A tool failed. Use read-only diagnosis as needed, continue unrelated work automatically." },
    { kind: "notice", id: "user-steer", level: "info", text: "↪ 改用 Pillow 10 验证" },
    { kind: "assistant", id: "a-done", text: "pptxgenjs 已安装成功", reasoning: "", streaming: false },
  ]);
  const rows = buildTranscriptRows(models, rowOptions(EMPTY_FOLDS));
  eq(kinds(rows), "user,answer,notice,answer,turn-actions", "host Auto Guard guidance is not a user-side steer row");
  const notice = rows.find((row) => row.kind === "notice");
  eq(notice && "item" in notice ? notice.item.text : "", "↪ 改用 Pillow 10 验证", "real user steers stay visible");
}

{
  const models = buildTurnModels([
    { kind: "user", id: "u1", text: "cancelled before output" },
  ]);
  const withoutCheckpoint = buildTranscriptRows(models, rowOptions(EMPTY_FOLDS));
  eq(kinds(withoutCheckpoint), "user", "textless turns do not expose actions without a checkpoint");

  const withCheckpoint = buildTranscriptRows(models, {
    ...rowOptions(EMPTY_FOLDS),
    hasCheckpointForTurn: (turn) => turn === 0,
  });
  eq(kinds(withCheckpoint), "user,turn-actions", "checkpoint-only cancelled turns keep rewind actions visible");
  const action = withCheckpoint.find((row) => row.kind === "turn-actions");
  eq(action?.kind === "turn-actions" ? action.text : "missing", "", "checkpoint-only actions carry no empty copy payload");
}

// ── Fold reconciliation ───────────────────────────────────────────────────────

{
  const reasoningOnly = buildTurnModels([
    { kind: "user", id: "u-hidden", text: "inspect" },
    { kind: "assistant", id: "a-hidden", text: "", reasoning: "private thought", streaming: false },
  ], undefined, false, true);
  eq(foldSegmentStates(reasoningOnly).length, 0, "hidden reasoning does not create an empty process fold");

  const mixed = buildTurnModels([
    { kind: "user", id: "u-mixed", text: "inspect" },
    { kind: "assistant", id: "a-mixed", text: "", reasoning: "private thought", streaming: false },
    { kind: "tool", id: "tool-mixed", name: "bash", args: "{}", output: "ok", status: "done", readOnly: false },
  ], undefined, false, true);
  const mixedStates = foldSegmentStates(mixed);
  eq(mixedStates.length, 1, "hidden reasoning keeps a mixed tool process fold");
  eq(foldSegmentStates(mixed, true)[0]?.keepReasoningExpanded, false, "expanded reasoning mode does not pin tool-only folds");
  eq(mixed[0]?.segments[0]?.displayItems.filter((item) => item.kind === "assistant").length ?? -1, 0, "hidden reasoning is excluded from fold body items");
  eq(mixed[0]?.segments[0]?.displayItems.filter((item) => item.kind === "tool").length ?? -1, 1, "hidden reasoning does not hide tools");
}

{
  // Auto-open while running, auto-close on completion.
  const running = buildTurnModels(fixture.slice(0, 7), { id: "a2", hasAnswerText: true, hasReasoning: false, reasoningComplete: true }, true);
  const states = foldSegmentStates(running);
  eq(states.length, 1, "one fold segment for the fixture turn");
  eq(states[0].hasRunningWork, true, "active turn marks its fold as running");
  eq(defaultFoldOpen(states[0], "auto"), true, "running folds default open");

  const seeded = reconcileFoldEntries(EMPTY_FOLDS, states, "auto", false);
  const foldKey = states[0].key;
  ok(seeded?.get(foldKey)?.open === true, "reconcile seeds running folds open");

  const settledModels = buildTurnModels(fixture.slice(0, 7), undefined, false);
  const settledStates = foldSegmentStates(settledModels);
  eq(settledStates[0].hasRunningWork, false, "settled turn clears the running flag");
  const closed = reconcileFoldEntries(seeded ?? EMPTY_FOLDS, settledStates, "auto", false);
  ok(closed?.get(foldKey)?.open === false, "completion auto-closes an untouched fold");
  eq(reconcileFoldEntries(closed ?? EMPTY_FOLDS, settledStates, "auto", false), null, "steady state reconciles to no change");
}

{
  // The first answer token completes the reasoning phase but not the active
  // turn. Its row must keep the expanded geometry until turn_done, otherwise
  // the live footer loses the full reasoning height in one commit.
  const activeModels = buildTurnModels([
    { kind: "user", id: "u-auto-geometry", text: "inspect" },
    {
      kind: "assistant",
      id: "a-auto-geometry",
      text: "first answer token",
      reasoning: "long reasoning",
      streaming: false,
      reasoningComplete: true,
    },
    { kind: "tool", id: "t-auto-geometry", name: "read_file", args: "{}", status: "running", readOnly: true },
  ], undefined, true);
  const activeRows = buildTranscriptRows(activeModels, rowOptions(EMPTY_FOLDS, "expanded"));
  const activeReasoning = activeRows.find((row) => row.kind === "reasoning");
  eq(activeReasoning?.layoutVariant, "reasoning-expanded", "active turn keeps completed reasoning geometry expanded");

  const settledModels = buildTurnModels([
    { kind: "user", id: "u-auto-geometry", text: "inspect" },
    {
      kind: "assistant",
      id: "a-auto-geometry",
      text: "first answer token",
      reasoning: "long reasoning",
      streaming: false,
      reasoningComplete: true,
    },
  ], undefined, false);
  const settledRows = buildTranscriptRows(settledModels, rowOptions(EMPTY_FOLDS, "expanded"));
  const settledReasoning = settledRows.find((row) => row.kind === "reasoning");
  eq(settledReasoning?.layoutVariant, "reasoning-summary", "settled turn returns auto reasoning geometry to its summary");
}

{
  // Expanded reasoning pins only reasoning-bearing folds across completion.
  const running = buildTurnModels(fixture.slice(0, 7), { id: "a2", hasAnswerText: true, hasReasoning: false, reasoningComplete: true }, true);
  const runningStates = foldSegmentStates(running, true);
  const foldKey = runningStates[0].key;
  eq(runningStates[0]?.keepReasoningExpanded, true, "expanded mode marks a reasoning-bearing fold as pinned");
  const seeded = reconcileFoldEntries(EMPTY_FOLDS, runningStates, "auto", false) ?? EMPTY_FOLDS;

  const settledModels = buildTurnModels(fixture.slice(0, 7), undefined, false);
  const settledPinnedStates = foldSegmentStates(settledModels, true);
  const completed = reconcileFoldEntries(seeded, settledPinnedStates, "auto", false);
  ok(completed?.get(foldKey)?.open === true, "expanded reasoning keeps its parent fold open after completion");

  const manuallyCollapsed = foldMapWithToggle(seeded, foldKey, true);
  const completedCollapsed = reconcileFoldEntries(manuallyCollapsed, settledPinnedStates, "auto", false);
  ok(completedCollapsed?.get(foldKey)?.open === false, "manual parent collapse still wins when expanded reasoning completes");

  const settledAutoStates = foldSegmentStates(settledModels, false);
  const backToAuto = reconcileFoldEntries(completed ?? EMPTY_FOLDS, settledAutoStates, "auto", false);
  ok(backToAuto?.get(foldKey)?.open === false, "leaving expanded reasoning re-applies the automatic parent fold policy");
}

{
  // User override survives completion; a fold with nothing outside never
  // auto-closes.
  const settledModels = buildTurnModels(fixture.slice(0, 7), undefined, false);
  const states = foldSegmentStates(settledModels);
  const foldKey = states[0].key;
  const overridden: FoldMap = new Map([[foldKey, { open: true, userOverridden: true, running: true }]]);
  const next = reconcileFoldEntries(overridden, states, "auto", false);
  ok(next?.get(foldKey)?.open === true, "user-opened fold survives completion");

  const soloModels = buildTurnModels([
    { kind: "user", id: "u5", text: "cancelled" },
    { kind: "assistant", id: "a8", text: "", reasoning: "cut off", streaming: false },
  ]);
  const soloStates = foldSegmentStates(soloModels);
  eq(soloStates[0].hasOutsideContent, false, "reasoning-only turn has nothing outside its fold");
  eq(defaultFoldOpen(soloStates[0], "auto"), true, "a fold with nothing outside stays expanded");
}

{
  // Preference switches apply to existing folds and clear manual overrides.
  const models = buildTurnModels(fixture.slice(0, 7), undefined, false);
  const states = foldSegmentStates(models);
  const foldKey = states[0].key;
  const seeded: FoldMap = new Map([[foldKey, { open: true, userOverridden: true, running: false }]]);
  const expanded = reconcileFoldEntries(seeded, states, "expanded", true);
  ok(expanded?.get(foldKey)?.open === true && expanded.get(foldKey)?.userOverridden === false, "switching to expanded opens folds and clears overrides");
  const backToAuto = reconcileFoldEntries(expanded ?? EMPTY_FOLDS, states, "auto", true);
  ok(backToAuto?.get(foldKey)?.open === false, "switching back to auto re-closes completed folds");

  const pruned = reconcileFoldEntries(backToAuto ?? EMPTY_FOLDS, [], "auto", false);
  ok(pruned?.size === 0, "vanished segments are pruned from the fold map");
}

// ── Lazy content entry derivation ─────────────────────────────────────────────

{
  eq(historyEntryIdForItemId("he:entry-1"), "entry-1", "history item id maps to its entry id");
  eq(historyEntryIdForItemId("he:entry-1:tc2"), "entry-1", "tool call fallback id maps to the owning entry");
  eq(historyEntryIdForItemId("call_abc"), undefined, "bare tool call ids carry no entry");
  eq(historyEntryIdForItemId("h3-2"), undefined, "legacy item ids carry no entry");
  const answerRow: TranscriptRow = { kind: "answer", key: "a:he:entry-9", item: { kind: "assistant", id: "he:entry-9", text: "x", reasoning: "", streaming: false } };
  eq(historyEntryIdForRow(answerRow), "entry-9", "answer rows expose their entry for lazy ref resolution");
  eq(historyEntryIdForRow({ kind: "older-history", key: "older-history" }), undefined, "the paging row has no entry");
}

{
  const localOnly: Item[] = [
    { kind: "user", id: "u1", text: "first" },
    { kind: "tool", id: "__reasonix_local_only__", name: "__reasonix_local_only__", args: "", readOnly: false, status: "done", output: "partial one" },
    { kind: "user", id: "u2", text: "second" },
    { kind: "tool", id: "__reasonix_local_only__", name: "__reasonix_local_only__", args: "", readOnly: false, status: "done", output: "partial two" },
  ];
  const rows = buildTranscriptRows(buildTurnModels(localOnly), rowOptions(EMPTY_FOLDS, "expanded"));
  const rowKeys = rows.map((row) => row.key);
  eq(new Set(rowKeys).size, rowKeys.length, "two local-only recoveries cannot share ph:/t: row keys");
}

// ── Size estimates ────────────────────────────────────────────────────────────

{
  const models = buildTurnModels(fixture);
  const rows = buildTranscriptRows(models, rowOptions(EMPTY_FOLDS));
  ok(rows.every((row) => estimateTranscriptRowSize(row) > 0), "every row kind has a positive size estimate");
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
