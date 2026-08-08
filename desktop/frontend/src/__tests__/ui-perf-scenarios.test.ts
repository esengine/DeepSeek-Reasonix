// Run: tsx src/__tests__/ui-perf-scenarios.test.ts
//
// Simulated-driver UI performance scenarios: the streaming pipeline's count
// budgets under realistic workloads, not just a normal chat answer. Wall-clock
// and input-latency budgets (UI-PERF-04) belong to the real-browser driver;
// here every assertion is deterministic — reducer passes per frame, markdown
// parses per stream, and the background-tab bump-skip invariant.

import { streamingCommitTarget, streamingMarkdownCommitInterval } from "../components/Markdown";
import { initialState, reducer } from "../lib/useController";
import { coalesceStreamDeltas } from "../lib/streamDeltaBatch";
import type { StreamDeltaEntry } from "../lib/streamDeltaBatch";
import { generateScenarioChunks, UI_PERF_SCENARIOS } from "../lib/uiPerfScenarios";
import type { UIPerfScenario } from "../lib/uiPerfScenarios";
import type { WireEvent } from "../lib/types";

let passed = 0;
let failed = 0;

function ok(cond: boolean, label: string) {
  process.stdout.write(`  ${cond ? "PASS" : "FAIL"}  ${label}\n`);
  if (cond) passed += 1;
  else failed += 1;
}

function eq(a: unknown, b: unknown, label: string) {
  ok(a === b, a === b ? label : `${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}`);
}

const FRAME_MS = 1000 / 60;

interface SimResult {
  frames: number;
  commits: number;
  answerMarkdownParses: number;
  reasoningMarkdownParses: number;
  itemsIdentityBreaks: number;
  bumpSkipViolations: number;
  state: typeof initialState;
}

// simulate streams a scenario at its chunk rate through the real pipeline:
// per 16.7ms frame the accumulated deltas coalesce into one stream_batch and
// fold through the reducer, while the markdown commit model advances at the
// tiered interval over the growing text.
function simulate(spec: UIPerfScenario, base?: typeof initialState): SimResult {
  const chunks = generateScenarioChunks(spec);
  let state = base ?? reducer({ ...initialState }, { type: "event", e: { kind: "turn_started" } as WireEvent });
  const chunksPerFrame = spec.chunksPerSec / 60;

  let frames = 0;
  let commits = 0;
  let answerMarkdownParses = 0;
  let reasoningMarkdownParses = 0;
  let itemsIdentityBreaks = 0;
  let bumpSkipViolations = 0;
  let text = "";
  let reasoning = "";
  let answerRenderedLen = 0;
  let reasoningRenderedLen = 0;
  let lastAnswerParseAt = -Infinity;
  let lastReasoningParseAt = -Infinity;
  let pendingChunks = 0;
  let index = 0;
  let firstBatch = true;
  let reasoningFinalized = false;

  while (index < chunks.length) {
    frames += 1;
    pendingChunks += chunksPerFrame;
    const batch: StreamDeltaEntry[] = [];
    while (pendingChunks >= 1 && index < chunks.length) {
      const chunk = chunks[index];
      index += 1;
      pendingChunks -= 1;
      if (chunk.kind === "text") text += chunk.delta;
      else reasoning += chunk.delta;
      batch.push({ tabId: "a", e: { kind: chunk.kind, text: chunk.delta } as WireEvent });
    }
    if (batch.length === 0) continue;
    for (const b of coalesceStreamDeltas(batch)) {
      const prev = state;
      state = reducer(prev, { type: "stream_batch", segments: b.segments } as never);
      if (state !== prev) commits += 1;
      if (!firstBatch && prev.items !== state.items) itemsIdentityBreaks += 1;
      const bumpSkipped =
        prev.items === state.items &&
        prev.currentAssistant === state.currentAssistant &&
        prev.pendingUser === state.pendingUser &&
        prev.retry === state.retry;
      if (!firstBatch && !bumpSkipped) bumpSkipViolations += 1;
      firstBatch = false;
    }
    const nowMs = frames * FRAME_MS;
    if (spec.reasoningVisible && reasoning.length > 0 && text.length > 0 && !reasoningFinalized) {
      reasoningMarkdownParses += 1;
      reasoningRenderedLen = reasoning.length;
      reasoningFinalized = true;
    }
    if (spec.reasoningVisible && !reasoningFinalized && nowMs - lastReasoningParseAt >= streamingMarkdownCommitInterval(reasoning.length)) {
      const target = streamingCommitTarget(reasoning);
      if (target.length > reasoningRenderedLen) {
        reasoningMarkdownParses += 1;
        reasoningRenderedLen = target.length;
        lastReasoningParseAt = nowMs;
      }
    }
    if (nowMs - lastAnswerParseAt >= streamingMarkdownCommitInterval(text.length)) {
      const target = streamingCommitTarget(text);
      if (target.length > answerRenderedLen) {
        answerMarkdownParses += 1;
        answerRenderedLen = target.length;
        lastAnswerParseAt = nowMs;
      }
    }
  }
  if (text.length > answerRenderedLen) answerMarkdownParses += 1;
  if (spec.reasoningVisible && reasoning.length > reasoningRenderedLen) reasoningMarkdownParses += 1;
  return { frames, commits, answerMarkdownParses, reasoningMarkdownParses, itemsIdentityBreaks, bumpSkipViolations, state };
}

const byId = new Map(UI_PERF_SCENARIOS.map((s) => [s.id, s]));
const scenario = (id: string): UIPerfScenario => {
  const s = byId.get(id);
  if (!s) throw new Error(`missing scenario ${id}`);
  return s;
};

// --- UI-PERF-01: normal 2KB markdown answer ---
{
  const spec = scenario("UI-PERF-01");
  const r = simulate(spec);
  ok(r.commits <= r.frames + 1, `01: one reducer pass per frame at most (${r.commits} commits / ${r.frames} frames)`);
  ok(
    r.answerMarkdownParses <= spec.paragraphs + 3,
    `01: markdown parses bounded by blocks, not ticks (${r.answerMarkdownParses} for ${spec.paragraphs} paragraphs)`,
  );
  ok(r.state.live !== undefined && r.state.live.text.length >= spec.textChars, "01: full answer reached the live stream");
}

// --- UI-PERF-02: 16KB reasoning + 8KB answer at 150 chunks/sec ---
{
  const spec = scenario("UI-PERF-02");
  const r = simulate(spec);
  const seconds = r.frames / 60;
  ok(r.commits / seconds <= 61, `02: state commits stay at or under display FPS (${(r.commits / seconds).toFixed(1)}/s)`);
  ok(r.reasoningMarkdownParses <= 2, `02: visible reasoning Markdown stays within its parse budget (${r.reasoningMarkdownParses})`);
  ok(r.answerMarkdownParses <= spec.paragraphs + 3, `02: answer Markdown keeps its existing parse budget (${r.answerMarkdownParses})`);
  ok(r.state.live !== undefined && r.state.live.reasoning.length >= spec.reasoningChars, "02: full reasoning reached the live stream");
  eq(r.state.live?.reasoningComplete, true, "02: answer text after reasoning completed it");
}

// --- UI-PERF-03: 10 code fences in 20KB ---
{
  const spec = scenario("UI-PERF-03");
  const r = simulate(spec);
  ok(r.commits <= r.frames + 1, `03: commits bounded by frames (${r.commits}/${r.frames})`);
  ok(
    r.answerMarkdownParses >= spec.codeFences,
    `03: open fences keep committing so streamed code stays highlighted (${r.answerMarkdownParses} parses)`,
  );
  ok(
    r.answerMarkdownParses <= Math.ceil(r.frames / 3) + spec.paragraphs + spec.codeFences,
    `03: parse cadence capped by the 50ms tier even inside fences (${r.answerMarkdownParses} parses / ${r.frames} frames)`,
  );
}

// --- UI-PERF-05: streaming on top of a 100-turn, 30-tool-call transcript ---
{
  let long = reducer({ ...initialState }, { type: "event", e: { kind: "turn_started" } as WireEvent });
  for (let turn = 0; turn < 100; turn += 1) {
    long = reducer(long, { type: "user", text: `question ${turn}`, seq: long.seq });
    if (turn < 30) {
      const id = `tool-${turn}`;
      long = reducer(long, { type: "event", e: { kind: "tool_dispatch", tool: { id, name: "edit_file", readOnly: false, args: "{}" } } as WireEvent });
      long = reducer(long, { type: "event", e: { kind: "tool_result", tool: { id, name: "edit_file", readOnly: false, output: "ok", diff: "@@ -1 +1 @@\n-a\n+b\n", added: 1, removed: 1 } } as WireEvent });
    }
    long = reducer(long, { type: "event", e: { kind: "message", text: `answer ${turn} with \`code\`` } as WireEvent });
  }
  long = reducer(long, { type: "event", e: { kind: "turn_started" } as WireEvent });
  const itemsBefore = long.items.length;
  ok(itemsBefore >= 130, `05: transcript carries the long session (${itemsBefore} items)`);

  const r = simulate(scenario("UI-PERF-05"), long);
  eq(r.itemsIdentityBreaks, 0, "05: per-delta cost is O(1) — no transcript cloning while streaming over a long session");
  ok(r.commits <= r.frames + 1, `05: commits bounded by frames on a long transcript (${r.commits}/${r.frames})`);
}

// --- UI-PERF-06: tab A streams while tab B is active ---
{
  const r = simulate(scenario("UI-PERF-06"));
  eq(r.bumpSkipViolations, 0, "06: background streaming never trips the whole-App bump — liveStore only");
  eq(r.reasoningMarkdownParses, 0, "06: background reasoning performs no Markdown parsing");
}

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
