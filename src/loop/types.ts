import type { RepairReport } from "../repair/index.js";
import type { TurnStats } from "../telemetry/stats.js";

export type EventRole =
  | "assistant_delta"
  | "assistant_final"
  /** Only liveness signal during a large-args tool call (no content/reasoning bytes). */
  | "tool_call_delta"
  /** Pre-dispatch ping so the TUI can show a spinner during long tool awaits. */
  | "tool_start"
  | "tool"
  | "done"
  | "error"
  | "warning"
  /** Transient indicator for silent phases; UI clears on next primary event. */
  | "status";

export interface LoopEvent {
  turn: number;
  role: EventRole;
  content: string;
  reasoningDelta?: string;
  toolName?: string;
  /** Raw args JSON — needed by `reasonix diff` to explain why a tool was called. */
  toolArgs?: string;
  /** Cumulative arguments-string length for `role === "tool_call_delta"`. */
  toolCallArgsChars?: number;
  /** Zero-based index of the tool call this delta belongs to (multi-tool progress). */
  toolCallIndex?: number;
  /** Count of tool calls whose args have parsed as valid JSON (UI progress, not dispatch gate). */
  toolCallReadyCount?: number;
  stats?: TurnStats;
  repair?: RepairReport;
  error?: string;
  /** Display-only — code-mode applier MUST skip SEARCH/REPLACE in forced-summary text. */
  forcedSummary?: boolean;
}
