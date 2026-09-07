import {
  buildTranscriptRowBlocks,
  buildTurnModels,
  EMPTY_FOLDS,
  NO_LIVE,
  type BuildRowsOptions,
} from "../lib/transcriptRows";
import {
  defaultTranscriptRenderMode,
  projectTranscriptTimeline,
  splitWindowedTimeline,
} from "../lib/transcriptTimeline";
import type { Item } from "../lib/useController";

let passed = 0;
let failed = 0;
function ok(condition: unknown, label: string) {
  if (condition) { process.stdout.write(`  PASS  ${label}\n`); passed += 1; }
  else { process.stdout.write(`  FAIL  ${label}\n`); failed += 1; }
}
function items(start: number, count: number): Item[] {
  return Array.from({ length: count }, (_, index) => {
    const turn = start + index;
    return [
      { kind: "user", id: `entry-${turn}`, text: `question ${turn}`, historyTurn: turn } as Item,
      { kind: "assistant", id: `answer-${turn}`, text: `answer ${turn}`, reasoning: "", streaming: false } as Item,
    ];
  }).flat();
}
const options: BuildRowsOptions = {
  folds: EMPTY_FOLDS,
  sessionExperience: "standard",
  hasOlderHistory: false,
  creationMode: false,
  turnForUser: (item) => (item.historyTurn ?? 1) - 1,
};

console.log("\nTimelineProjection block contract");
const newest = buildTranscriptRowBlocks(buildTurnModels(items(4, 4), NO_LIVE, false), options);
const prepended = buildTranscriptRowBlocks(buildTurnModels(items(1, 7), NO_LIVE, false), options);
const oldKeys = new Set(newest.map((block) => block.key));
ok(newest.every((block) => prepended.some((candidate) => candidate.key === block.key)), "prepend preserves every existing block identity");
ok(newest.every((block) => block.rows.every((row) => row.key)), "every block contains stable row identities");
ok(oldKeys.size === newest.length, "backend turn identities produce unique block keys");
const patchedItems = items(4, 4);
const patchedUser = patchedItems[2] as Extract<Item, { kind: "user" }>;
patchedItems[2] = { ...patchedUser, text: "edited question", historyTurn: 400, checkpointTurn: 77 };
const patched = buildTranscriptRowBlocks(buildTurnModels(patchedItems, NO_LIVE, false), options);
ok(patched[1]?.key === newest[1]?.key, "prompt patches and history renumbering do not change block identity");

const streaming = buildTranscriptRowBlocks(buildTurnModels(items(1, 4), { id: "answer-4", hasAnswerText: true, hasReasoning: false }, true), options);
const projection = projectTranscriptTimeline(streaming, true);
ok(projection.completedBlocks.length === 3, "active turn is excluded from cold completed history");
ok(projection.activeBlock?.key === streaming[streaming.length - 1]?.key, "active turn remains one ordinary-DOM block");
ok(projection.hasOlderHistory, "projection preserves paging state without adding a fake row");

ok(defaultTranscriptRenderMode(100) === "full", "100 completed turns use full DOM");
ok(defaultTranscriptRenderMode(101) === "windowed", "101 completed turns enter the window adapter");
const longProjection = projectTranscriptTimeline(buildTranscriptRowBlocks(buildTurnModels(items(1, 101), NO_LIVE, false), options), false);
const split = splitWindowedTimeline(longProjection);
ok(split.cold.length === 99 && split.resident.length === 2, "windowed history keeps the two most recent completed turns resident");

const started = performance.now();
const tenThousand = buildTranscriptRowBlocks(buildTurnModels(items(1, 10_000), NO_LIVE, false), options);
const elapsed = performance.now() - started;
ok(tenThousand.length === 10_000, "10,000 turns project to 10,000 blocks");
ok(elapsed < 1_000, `10,000-turn projection stays below 1s (${elapsed.toFixed(1)}ms)`);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed) process.exit(1);
