import type { WireEvent } from "./types";
import type { LiveStream } from "./useController";

export interface StreamDeltaEntry {
  tabId: string;
  e: WireEvent;
}

// StreamSegment is one run of consecutive same-kind deltas within a frame.
// Segment order is authoritative: a reasoning→text boundary completes
// reasoning exactly as per-delta delivery would, so kinds are never bucketed.
export interface StreamSegment {
  kind: "text" | "reasoning";
  delta: string;
}

export interface TabStreamBatch {
  tabId: string;
  segments: StreamSegment[];
}

// coalesceStreamDeltas groups one rAF batch by tab and merges consecutive
// same-kind deltas into ordered segments, so a frame dispatches one
// stream_batch action (one reducer pass, one live-store notification) per tab
// no matter how many token deltas the bridge delivered. Tabs are independent
// state machines, so per-tab grouping cannot reorder anything observable.
// Empty deltas are kept: an empty text delta still completes live reasoning.
export function coalesceStreamDeltas(batch: StreamDeltaEntry[]): TabStreamBatch[] {
  const out: TabStreamBatch[] = [];
  const byTab = new Map<string, StreamSegment[]>();
  for (const { tabId, e } of batch) {
    const kind = e.kind === "reasoning" ? "reasoning" : "text";
    const delta = e.text ?? e.reasoning ?? "";
    let segments = byTab.get(tabId);
    if (!segments) {
      segments = [];
      byTab.set(tabId, segments);
      out.push({ tabId, segments });
    }
    const last = segments[segments.length - 1];
    if (last && last.kind === kind) last.delta += delta;
    else segments.push({ kind, delta });
  }
  return out;
}

export function completeLiveReasoning(live: LiveStream, now = Date.now()): LiveStream {
  if (!live.reasoning || live.reasoningCompletedAt) {
    return { ...live, reasoningComplete: live.reasoning !== "" || live.reasoningComplete };
  }
  return {
    ...live,
    reasoningComplete: true,
    reasoningCompletedAt: now,
  };
}

// applyLiveSegments folds one frame's ordered segments into the live stream,
// replicating per-delta semantics: text completes reasoning first; reasoning
// reopens it and stamps its start on the first non-empty delta.
export function applyLiveSegments(base: LiveStream, segments: StreamSegment[], now: number): LiveStream {
  let live = base;
  for (const seg of segments) {
    live =
      seg.kind === "text"
        ? { ...completeLiveReasoning(live, now), text: live.text + seg.delta }
        : {
            ...live,
            reasoning: live.reasoning + seg.delta,
            reasoningComplete: false,
            reasoningStartedAt: live.reasoningStartedAt ?? (seg.delta ? now : undefined),
            reasoningCompletedAt: undefined,
          };
  }
  return live;
}
