/**
 * Orchestrate — split a complex task into independent subtasks, fan out
 * parallel subagents for each, and merge results.
 *
 * Engineered around Reasonix's cache-first architecture: all subagents
 * share one ImmutablePrefix so the second and later subagents hit
 * DeepSeek's prefix-cache (~90% cost reduction on subsequent spawns).
 *
 * No "Lead Agent" LLM call — the decomposition is data parallel (one
 * subagent per task[] entry), and the merge is a pure string formatter.
 */

import type { DeepSeekClient } from "../client.js";
import { ImmutablePrefix } from "../memory/runtime.js";
import type { ToolRegistry } from "../tools.js";
import {
  type SubagentResult,
  type SubagentSink,
  formatSubagentResult,
  spawnSubagent,
} from "./subagent.js";

/** Subagent model default — flash first, same as spawn_subagent. */
const DEFAULT_MODEL = "deepseek-v4-flash";
/** Thinking budget — "high" by default (cost-optimized). */
const DEFAULT_EFFORT = "high" as const;
/** Hard cap on fan-out width.  Prevents accidental fork bombs. */
const MAX_TASKS = 16;

// ── Options ────────────────────────────────────────────────────────

export interface OrchestrateOptions {
  client: DeepSeekClient;
  parentRegistry: ToolRegistry;
  defaultSystem?: string;
  projectRoot?: string;
  sink?: SubagentSink;
  /** Fires once per subagent after spawn. */
  onSpawnComplete?: (result: SubagentResult) => void;
}

// ── Orchestrate ────────────────────────────────────────────────────

export interface OrchestrateInput {
  tasks: string[];
  model?: "deepseek-v4-flash" | "deepseek-v4-pro";
  effort?: "high" | "max";
}

export interface OrchestrateTaskResult {
  index: number;
  task: string;
  result: SubagentResult;
}

export async function orchestrate(
  opts: OrchestrateOptions,
  input: OrchestrateInput,
): Promise<OrchestrateTaskResult[]> {
  const tasks = input.tasks.slice(0, MAX_TASKS);
  if (tasks.length === 0) {
    return [];
  }

  const model = input.model ?? DEFAULT_MODEL;
  const effort = input.effort ?? DEFAULT_EFFORT;
  const baseSystem = opts.defaultSystem ?? "";
  const system = opts.projectRoot ? `${baseSystem}\n\nProject: ${opts.projectRoot}` : baseSystem;

  // Shared prefix — all subagents get the same ImmutablePrefix so
  // DeepSeek's prefix-cache activates on the second spawn onward.
  const sharedPrefix = new ImmutablePrefix({
    system,
    toolSpecs: opts.parentRegistry.specs(),
  });

  const promises = tasks.map(async (task, i) => {
    const result = await spawnSubagent({
      client: opts.client,
      parentRegistry: opts.parentRegistry,
      system,
      task,
      model,
      reasoningEffort: effort,
      sink: opts.sink,
      sharedPrefix,
    });
    opts.onSpawnComplete?.(result);
    return { index: i, task, result };
  });

  return Promise.all(promises);
}

// ── Format Results ─────────────────────────────────────────────────

export function formatOrchestrationResult(results: OrchestrateTaskResult[]): string {
  if (results.length === 0) return "No tasks provided.";

  const succeeded = results.filter((r) => r.result.success).length;
  const totalTurns = results.reduce((s, r) => s + r.result.turns, 0);
  const totalCost = results.reduce((s, r) => s + r.result.costUsd, 0);

  let out = `## Orchestration Results (${succeeded}/${results.length} tasks completed)\n\n`;
  out += "| # | Task | Status | Turns | Cost |\n";
  out += "|---|------|--------|-------|------|\n";

  for (const r of results) {
    const status = r.result.success ? "✅" : "❌";
    const taskPreview = r.task.length > 40 ? `${r.task.slice(0, 40)}…` : r.task;
    out += `| ${r.index + 1} | ${taskPreview} | ${status} | ${r.result.turns} | ¥${r.result.costUsd.toFixed(4)} |\n`;
  }

  out += `| **Total** | | | **${totalTurns}** | **¥${totalCost.toFixed(4)}** |\n`;
  out += "\n---\n";

  for (const r of results) {
    const body = formatSubagentResult(r.result);
    out += `\n### ${r.task}\n\n`;
    if (r.result.success) {
      out += r.result.output;
    } else {
      out += `**Error:** ${r.result.error ?? "unknown error"}`;
    }
    out += "\n";
  }

  return out;
}

// ── Tool Registration ──────────────────────────────────────────────

export function registerOrchestrateTool(
  registry: ToolRegistry,
  opts: OrchestrateOptions,
): ToolRegistry {
  registry.register({
    name: "orchestrate",
    parallelSafe: true,
    description:
      "Split a complex task into independent subtasks and run them in parallel using isolated subagents. Each subtask gets its own tool loop and runs to completion. Results are merged into a single summary. Use this when the task naturally decomposes (e.g. 'fix X, test Y, document Z') and subtasks don't depend on each other. Each spawn pays a prefix-cache miss on the first subagent; subsequent subagents benefit from DeepSeek's prefix-cache (~90% cost reduction).",
    parameters: {
      type: "object",
      properties: {
        tasks: {
          type: "array",
          items: { type: "string" },
          description: `Independent subtask descriptions. Each gets its own subagent. Keep each task focused and self-contained — subagents have none of your conversation context, only what you write here. Max ${MAX_TASKS} tasks.`,
        },
        model: {
          type: "string",
          enum: ["deepseek-v4-flash", "deepseek-v4-pro"],
          description:
            "Model for all subagents. Defaults to deepseek-v4-flash (cost-optimized). Use pro only when subtasks genuinely need deeper reasoning.",
        },
        effort: {
          type: "string",
          enum: ["high", "max"],
          description:
            "Thinking budget per subagent. Defaults to high (cost-optimized — saves output tokens vs max).",
        },
      },
      required: ["tasks"],
    },
    fn: async (args) => {
      const input = args as OrchestrateInput;
      const results = await orchestrate(opts, input);
      return formatOrchestrationResult(results);
    },
  });

  return registry;
}
