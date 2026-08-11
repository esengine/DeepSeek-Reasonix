// Run: tsx src/__tests__/wire-event-batching.test.ts
//
// Multi-session rendering cost: pure progress deltas (tool output, subagent
// preview) must classify as frame-coalescable, state/result events must stay
// immediate, and a background tab's streaming delta must not bump the full
// controller tree — switching tabs while other sessions run must not starve on
// re-renders driven by the other sessions' progress.

import { classifyWireEvent, flushOnTypeSwitch, isBackgroundStreamingAction } from "../lib/wireEventBatching";

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

// --- classifyWireEvent routes the three dispatch budgets ---
{
  eq(classifyWireEvent({ kind: "text" }), "text", "text deltas keep their existing frame batch");
  eq(classifyWireEvent({ kind: "reasoning" }), "text", "reasoning deltas share the text batch");
  eq(classifyWireEvent({ kind: "tool_progress" }), "progress", "tool progress coalesces per frame");
  eq(classifyWireEvent({ kind: "subagent_progress" }), "progress", "subagent progress coalesces per frame");
  eq(classifyWireEvent({ kind: "tool_result" }), "immediate", "tool result stays immediate");
  eq(classifyWireEvent({ kind: "usage" }), "immediate", "usage stays immediate");
  eq(classifyWireEvent({ kind: "notice" }), "immediate", "notice stays immediate");
  eq(classifyWireEvent({ kind: "turn_done" }), "immediate", "turn_done stays immediate");
}

// --- isBackgroundStreamingAction: only pure deltas may skip the re-render ---
{
  eq(isBackgroundStreamingAction({ type: "event", e: { kind: "tool_progress" } }), true, "background tool progress skips the bump");
  eq(isBackgroundStreamingAction({ type: "event", e: { kind: "subagent_progress" } }), true, "background subagent progress skips the bump");
  eq(isBackgroundStreamingAction({ type: "event", e: { kind: "text" } }), true, "background text delta skips the bump");
  eq(isBackgroundStreamingAction({ type: "stream_batch", segments: [] } as never), true, "stream_batch action skips the bump");
  eq(isBackgroundStreamingAction({ type: "event", e: { kind: "tool_result" } }), false, "tool result still re-renders");
  eq(isBackgroundStreamingAction({ type: "event", e: { kind: "turn_done" } }), false, "turn_done still re-renders");
  eq(isBackgroundStreamingAction({ type: "event", e: { kind: "notice" } }), false, "notice still re-renders");
  eq(isBackgroundStreamingAction({ type: "context", context: {} } as never), false, "non-event actions still re-render");
}

// --- flushOnTypeSwitch: batching must not invert wire arrival order ---
// tool_progress → text → tool_result must apply progress before text; the
// kind switch drains the other queue so causal order survives coalescing.
{
  const seq: string[] = [];
  const text = { drain: () => seq.push("text") };
  const progress = { drain: () => seq.push("progress") };
  flushOnTypeSwitch("text", text, progress);
  eq(seq.join(","), "progress", "text event flushes queued progress first");
  seq.length = 0;
  flushOnTypeSwitch("progress", text, progress);
  eq(seq.join(","), "text", "progress event flushes queued text first");
  seq.length = 0;
  flushOnTypeSwitch("immediate", text, progress);
  eq(seq.join(","), "text,progress", "immediate event flushes both queues");
}

process.stdout.write(`\n${failed === 0 ? "ALL PASS" : `${failed} FAILED`} (${passed} passed)\n`);
process.exit(failed === 0 ? 0 : 1);
