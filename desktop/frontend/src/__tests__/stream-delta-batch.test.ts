// Run: tsx src/__tests__/stream-delta-batch.test.ts
//
// A frame's worth of token deltas must become ONE stream_batch action per tab
// (one reducer pass, one liveStore notification), with segment order kept so
// the reasoning→text boundary completes reasoning exactly as per-delta
// delivery would.

import { initialState, reducer } from "../lib/useController";
import { coalesceStreamDeltas } from "../lib/streamDeltaBatch";
import type { StreamDeltaEntry } from "../lib/streamDeltaBatch";
import type { WireEvent } from "../lib/types";

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

const text = (tabId: string, t: string): StreamDeltaEntry => ({ tabId, e: { kind: "text", text: t } as WireEvent });
const reasoning = (tabId: string, t: string): StreamDeltaEntry => ({ tabId, e: { kind: "reasoning", text: t } as WireEvent });

// --- consecutive same-kind deltas merge into ordered segments, one batch per tab ---
{
  const batches = coalesceStreamDeltas([
    reasoning("a", "我"), reasoning("a", "需要"), reasoning("a", "检查"), reasoning("a", "一下"),
    text("a", "发现"), text("a", "问题"),
    text("b", "other"),
    text("a", "如下"),
    reasoning("a", "再想"),
  ]);
  eq(batches.length, 2, "one stream_batch per tab");
  eq(batches[0].tabId, "a", "first-seen tab comes first");
  eq(batches[0].segments.length, 3, "tab a: reasoning, text, reasoning segments");
  eq(batches[0].segments[0].kind, "reasoning", "segment order preserved");
  eq(batches[0].segments[0].delta, "我需要检查一下", "reasoning run concatenates in order");
  eq(batches[0].segments[1].delta, "发现问题如下", "text run spans the other tab's interleave");
  eq(batches[0].segments[2].delta, "再想", "reasoning after text stays a separate segment");
  eq(batches[1].segments[0].delta, "other", "tab b keeps its own batch");
}

// --- legacy deltas carried in the reasoning field still coalesce ---
{
  const batches = coalesceStreamDeltas([
    { tabId: "a", e: { kind: "reasoning", reasoning: "le" } as WireEvent },
    { tabId: "a", e: { kind: "reasoning", reasoning: "gacy" } as WireEvent },
  ]);
  eq(batches[0].segments.length, 1, "legacy-field deltas merge");
  eq(batches[0].segments[0].delta, "legacy", "legacy payload concatenates");
}

// --- reducer equivalence: one stream_batch equals per-delta dispatch ---
{
  const deltas = [reasoning("a", "th"), reasoning("a", "inking"), text("a", "an"), text("a", "swer"), reasoning("a", "more")];
  let perDelta = { ...initialState, running: true, turnActive: true };
  for (const { e } of deltas) perDelta = reducer(perDelta, { type: "event", e });

  let batched = { ...initialState, running: true, turnActive: true };
  const batches = coalesceStreamDeltas(deltas);
  eq(batches.length, 1, "single tab folds to a single action");
  for (const b of batches) batched = reducer(batched, { type: "stream_batch", segments: b.segments } as never);

  eq(batched.live?.reasoning, perDelta.live?.reasoning, "reasoning matches per-delta result");
  eq(batched.live?.text, perDelta.live?.text, "text matches per-delta result");
  eq(batched.live?.reasoningComplete, perDelta.live?.reasoningComplete, "reasoning reopened by trailing segment");
  eq(batched.live?.reasoningComplete, false, "trailing reasoning segment leaves reasoning open");
  eq(batched.currentAssistant, perDelta.currentAssistant, "assistant identity matches");
}

// --- the reasoning→text boundary completes reasoning inside one batch ---
{
  let s = { ...initialState, running: true, turnActive: true };
  s = reducer(s, { type: "stream_batch", segments: [{ kind: "reasoning", delta: "想" }, { kind: "text", delta: "答" }] } as never);
  eq(s.live?.reasoningComplete, true, "text segment after reasoning completes it");
  eq(s.live?.reasoningCompletedAt !== undefined, true, "completion is timestamped");
}

// --- preamble parity: discarded turns drop deltas; retry clears ---
{
  const discarding = { ...initialState, discardTurn: true };
  const after = reducer(discarding, { type: "stream_batch", segments: [{ kind: "text", delta: "x" }] } as never);
  eq(after, discarding, "discardTurn swallows a stream_batch like per-delta events");

  let retrying: typeof initialState = { ...initialState, running: true, turnActive: true, retry: { attempt: 1, max: 3, observedAt: 1 } };
  retrying = reducer(retrying, { type: "stream_batch", segments: [{ kind: "text", delta: "x" }] } as never);
  eq(retrying.retry, undefined, "stream_batch clears the retry indicator like per-delta events");
}

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
