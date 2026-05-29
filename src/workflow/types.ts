import type { Usage } from "../client.js";
import type { SubagentResult, SubagentSink } from "../tools/subagent.js";

export interface WorkflowMetaPhase {
  title: string;
  detail?: string;
  model?: string;
}

export interface WorkflowMeta {
  name: string;
  description: string;
  whenToUse?: string;
  phases?: WorkflowMetaPhase[];
}

export type WorkflowMode = "run" | "dry_run" | "validate_only";
export type WorkflowToolMode = "read_only" | "full";
export type WorkflowAgentType = "explore" | "verify" | "synthesis";

export interface WorkflowAgentOptions {
  label?: string;
  phase?: string;
  type?: WorkflowAgentType;
  model?: string;
  allowedTools?: readonly string[];
  signal?: AbortSignal;
  workflowName: string;
}

export interface WorkflowAgentResult {
  ok: boolean;
  output: string;
  error?: string;
  raw?: SubagentResult;
}

export interface WorkflowAgentRunner {
  run(prompt: string, opts: WorkflowAgentOptions): Promise<WorkflowAgentResult>;
}

export interface WorkflowRunOptions {
  mode?: WorkflowMode;
  args?: unknown;
  cwd?: string;
  concurrency?: number;
  maxAgents?: number;
  tokenBudget?: number | null;
  signal?: AbortSignal;
  runner?: WorkflowAgentRunner;
  sink?: SubagentSink;
  onLog?: (message: string) => void;
  onPhase?: (title: string) => void;
  onAgentStart?: (event: { label: string; phase?: string; prompt: string }) => void;
  onAgentEnd?: (event: {
    label: string;
    phase?: string;
    result: WorkflowAgentResult | null;
  }) => void;
}

export interface WorkflowRunResult<T = unknown> {
  success: boolean;
  meta: WorkflowMeta;
  result?: T;
  error?: string;
  logs: string[];
  phases: string[];
  agentCount: number;
  plannedAgents?: Array<{ label: string; phase?: string; promptPreview: string }>;
  durationMs: number;
  usage?: Usage;
  costUsd?: number;
}
