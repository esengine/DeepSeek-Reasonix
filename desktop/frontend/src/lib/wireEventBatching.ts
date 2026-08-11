// Wire-event batching for the controller's per-event render cost. With several
// sessions running in parallel, every tool chunk / progress tick used to force
// a full App re-render, so switching tabs starved on the main thread. Text and
// pure-progress deltas coalesce into the animation-frame budget instead, and a
// background tab's streaming deltas update only its own state.

import { createRafBatch } from "./rafBatch";
import { coalesceStreamDeltas, type StreamDeltaEntry, type StreamSegment } from "./streamDeltaBatch";
import type { WireEvent } from "./types";
import { uiPerfTracker } from "./uiPerf";

// createTextBatch coalesces text/reasoning deltas into one dispatch pass per
// animation frame.
export function createTextBatch(dispatchTo: (tabId: string, action: { type: "stream_batch"; segments: StreamSegment[] }) => void) {
  return createRafBatch<StreamDeltaEntry>((batch) => {
    uiPerfTracker.onStreamDispatch();
    for (const b of coalesceStreamDeltas(batch)) dispatchTo(b.tabId, { type: "stream_batch", segments: b.segments });
  });
}

// createProgressBatch coalesces pure tool/subagent progress into the same
// animation-frame budget as text: a frame's worth of chunks dispatches once
// instead of forcing one full-tree re-render per chunk.
export function createProgressBatch(dispatchTo: (tabId: string, action: { type: "event"; e: WireEvent }) => void) {
  return createRafBatch<{ tabId: string; e: WireEvent }>((batch) => {
    for (const { tabId, e } of batch) dispatchTo(tabId, { type: "event", e });
  });
}

// flushOnTypeSwitch flushes the other coalescer when the wire kind changes so
// batched progress/text keep their wire arrival order; immediate events flush
// both before the caller applies them. Returns the kind for branch routing.
export function flushOnTypeSwitch(kind: WireEventBatchKind, textBatch: { drain(): void }, progressBatch: { drain(): void }): WireEventBatchKind {
  if (kind === "text") progressBatch.drain();
  else if (kind === "progress") textBatch.drain();
  else {
    textBatch.drain();
    progressBatch.drain();
  }
  return kind;
}

// shouldSkipBackgroundBump reports whether a pure streaming action on a
// background tab may skip the full controller re-render: the tab's own state
// updates and renders when it becomes active again.
export function shouldSkipBackgroundBump(tabId: string, activeTabId: string | null | undefined, action: { type: string; e?: { kind: string } }): boolean {
  return tabId !== activeTabId && isBackgroundStreamingAction(action);
}

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
