import type { HistoryMessage } from "./session";

/** running is a run this window's kernel is executing now; interrupted is one
 *  whose owner is gone without a result. */
export type SubagentState = "running" | "completed" | "failed" | "cancelled" | "interrupted";

export interface SubagentHandoff {
  summary: string;
  resultRefs: string[];
}

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
	 snapshot?: string;
  run: SubagentRun;
  messages: HistoryMessage[];
  after: number;
  next: number;
  total: number;
  live: boolean;
}
export interface FleetRecoveryPlan {
	legacy?: boolean;
  id: string;
  snapshot: string;
  ready: boolean;
  reason?: string;
  tasks: { id: string; description: string; prompt: string; dependsOn: string[]; state: string; ref?: string }[];
}
export interface SubagentPort {
  subagents(): Promise<SubagentRun[]>;
  fleetRecoveryPlans(ref: string): Promise<FleetRecoveryPlan[]>;
  requestFleetRecovery(ref: string, id: string, snapshot: string): Promise<void>;
  subagentTranscript(ref: string, after?: number, limit?: number): Promise<SubagentTranscript>;
  steerSubagent(ref: string, text: string, context?: SubagentHandoff): Promise<void>;
}
