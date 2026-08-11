// Wire-event batching for the controller's per-event render cost. With several
// sessions running in parallel, every tool chunk / progress tick used to force
// a full App re-render, so switching tabs starved on the main thread. Text and
// pure-progress deltas coalesce into the animation-frame budget instead, and a
// background tab's streaming deltas update only its own state.

// WireEventBatchKind classifies a wire event for dispatch routing:
// - "text"      — token deltas (already coalesced by textBatch)
// - "progress"  — pure tool/subagent progress, safe to coalesce into the frame
// - "immediate" — state/notice/result events, must apply right away to keep
//                 causal ordering (e.g. tool_result after tool_progress)
export type WireEventBatchKind = "text" | "progress" | "immediate";

export function classifyWireEvent(e: { kind: string }): WireEventBatchKind {
  if (e.kind === "text" || e.kind === "reasoning") return "text";
  if (e.kind === "tool_progress" || e.kind === "subagent_progress") return "progress";
  return "immediate";
}

// isBackgroundStreamingAction reports whether an action is a pure streaming
// delta (text/reasoning/progress), which a background tab may apply to its own
// state without forcing the full controller re-render — the tab re-renders
// when it becomes active again.
export function isBackgroundStreamingAction(action: {
  type: string;
  e?: { kind: string };
}): boolean {
  if (action.type === "stream_batch") return true;
  return action.type === "event" && classifyWireEvent(action.e ?? { kind: "" }) !== "immediate";
}
