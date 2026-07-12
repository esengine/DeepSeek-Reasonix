import type { Item } from "./useController";

export const TOOL_SLOW_WARN_MS = 10_000;
export const TOOL_SLOW_STRONG_MS = 30_000;
export const TOOL_SLOW_ACTION_MS = 60_000;

export type ToolDurationSeverity = "normal" | "slow" | "very-slow" | "action";

export interface TurnTimeline {
  turnStartedAt?: number;
  firstModelEventAt?: number;
  firstTokenAt?: number;
  messageDoneAt?: number;
  turnDoneAt?: number;
  contextRefreshStartedAt?: number;
  contextRefreshDoneAt?: number;
}

export function toolDurationSeverity(ms?: number): ToolDurationSeverity {
  if (typeof ms !== "number" || !Number.isFinite(ms)) return "normal";
  if (ms >= TOOL_SLOW_ACTION_MS) return "action";
  if (ms >= TOOL_SLOW_STRONG_MS) return "very-slow";
  if (ms >= TOOL_SLOW_WARN_MS) return "slow";
  return "normal";
}

export function relativeMs(at?: number, origin?: number): number | undefined {
  if (typeof at !== "number" || typeof origin !== "number") return undefined;
  if (!Number.isFinite(at) || !Number.isFinite(origin)) return undefined;
  return Math.max(0, at - origin);
}

export function estimateToolQueueMs(tool: Pick<Extract<Item, { kind: "tool" }>, "dispatchedAt" | "completedAt" | "durationMs">): number | undefined {
  if (typeof tool.dispatchedAt !== "number" || typeof tool.completedAt !== "number" || typeof tool.durationMs !== "number") {
    return undefined;
  }
  const wallMs = tool.completedAt - tool.dispatchedAt;
  if (!Number.isFinite(wallMs) || wallMs < 0) return undefined;
  return Math.max(0, wallMs - Math.max(0, tool.durationMs));
}

export function turnElapsedMs(timeline?: TurnTimeline): number | undefined {
  if (!timeline?.turnStartedAt) return undefined;
  return relativeMs(timeline.turnDoneAt ?? timeline.messageDoneAt, timeline.turnStartedAt);
}

