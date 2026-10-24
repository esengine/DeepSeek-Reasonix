import type { HistoryMessage } from "./session";

/** running is a run this window's kernel is executing now; interrupted is one
 *  whose owner is gone without a result. */
export type SubagentState = "running" | "completed" | "failed" | "cancelled" | "interrupted";

/** One sub-agent run a conversation spawned: GET /subagents. */
export interface SubagentRun {
  ref: string;
  name: string;
  kind: string;
  model: string;
  effort?: string;
  state: SubagentState;
  /** The transcript tool call that spawned the run. */
  parentCall?: string;
  startedAt: number;
  updatedAt: number;
}

/** One page of a run's conversation: GET /subagents/{ref}/transcript. Rows keep
 *  the run's own indices; next is where the following page starts. */
export interface SubagentTranscript {
  run: SubagentRun;
  messages: HistoryMessage[];
  after: number;
  next: number;
  total: number;
  live: boolean;
}
