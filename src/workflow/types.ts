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
export type WorkflowRunStatus = "running" | "completed" | "failed" | "aborted";
export type WorkflowSaveTarget = "project" | "user";

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

export interface WorkflowPhaseSnapshot {
  title: string;
  startedAt: number;
  completedAt?: number;
  agentCount: number;
}

export interface WorkflowAgentSnapshot {
  id: string;
  label: string;
  phase?: string;
  status: "running" | "completed" | "failed" | "aborted";
  promptPreview: string;
  outputPreview?: string;
  error?: string;
  startedAt: number;
  completedAt?: number;
  durationMs?: number;
}

export type WorkflowRunEvent =
  | {
      type: "workflow.started";
      runId: string;
      name: string;
      description: string;
      startedAt: number;
    }
  | { type: "workflow.phase.started"; runId: string; phase: string; ts: number }
  | {
      type: "workflow.agent.started";
      runId: string;
      agentId: string;
      label: string;
      phase?: string;
      promptPreview: string;
      ts: number;
    }
  | {
      type: "workflow.agent.completed";
      runId: string;
      agentId: string;
      label: string;
      ok: boolean;
      outputPreview?: string;
      error?: string;
      durationMs: number;
      ts: number;
    }
  | { type: "workflow.log"; runId: string; message: string; ts: number }
  | {
      type: "workflow.completed";
      runId: string;
      success: boolean;
      durationMs: number;
      agentCount: number;
      ts: number;
    }
  | { type: "workflow.aborted"; runId: string; reason: string; durationMs: number; ts: number };

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

export interface WorkflowRunSnapshot<T = unknown> {
  id: string;
  name: string;
  description: string;
  status: WorkflowRunStatus;
  mode: WorkflowMode;
  startedAt: number;
  updatedAt: number;
  durationMs?: number;
  phases: WorkflowPhaseSnapshot[];
  agents: WorkflowAgentSnapshot[];
  logs: string[];
  result?: T;
  error?: string;
  agentCount: number;
  usage?: Usage;
  costUsd?: number;
  script: string;
}
