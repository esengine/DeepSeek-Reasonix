import { DeepSeekClient } from "../client.js";
import { loadEndpoint } from "../config.js";
import type { ToolRegistry } from "../tools.js";
import { ReasonixWorkflowAgentRunner } from "../workflow/agent-runner.js";
import type { WorkflowRunManager } from "../workflow/manager.js";
import { runWorkflow } from "../workflow/runtime.js";
import type {
  WorkflowAgentRunner,
  WorkflowMode,
  WorkflowRunResult,
  WorkflowToolMode,
} from "../workflow/types.js";
import type { SubagentSink } from "./subagent.js";

export interface WorkflowToolOptions {
  rootDir?: string;
  runner?: WorkflowAgentRunner;
  manager?: WorkflowRunManager;
  clientFactory?: () => DeepSeekClient;
  subagentSink?: SubagentSink;
  defaultModel?: string;
}

const WORKFLOW_DESCRIPTION =
  'Execute a deterministic JavaScript workflow that orchestrates multiple internal Reasonix subagents with agent(), parallel(), pipeline(), phase(), and log(). Use for repository-wide audits, large refactors, migrations, multi-module bug hunts, architecture/security review, test coverage sweeps, multi-perspective verification, or when the user explicitly asks for workflow/dynamic workflow/parallel agents/subagents/fan-out analysis. For independent subtasks, use parallel([() => agent("task", { label: "..." }), ...]); consecutive `await agent(...)` calls are serial and should only be used when each task depends on the previous result. agent() accepts agent(promptString, opts) or agent({ instruction, label/name, type, phase, model, allowedTools }). Do not use for simple single-file edits, small bug fixes, direct Q&A, formatting, tasks that do not need parallel analysis, or when the user asks not to spawn subagents. Script must be raw JavaScript, start with export const meta = { name, description, phases? }, and call agent() at least once.';

export function registerWorkflowTool(
  registry: ToolRegistry,
  opts: WorkflowToolOptions = {},
): ToolRegistry {
  let client: DeepSeekClient | null = null;
  const getClient = (): DeepSeekClient => {
    if (opts.clientFactory) return opts.clientFactory();
    if (!client) {
      const ep = loadEndpoint();
      client = new DeepSeekClient({ apiKey: ep.apiKey, baseUrl: ep.baseUrl });
    }
    return client;
  };

  registry.register<Record<string, unknown>, string>({
    name: "workflow",
    description: WORKFLOW_DESCRIPTION,
    readOnlyCheck: (args) => args.tool_mode !== "full",
    parameters: {
      type: "object",
      properties: {
        script: {
          type: "string",
          description:
            'Required raw JavaScript workflow script, with no Markdown fences. First statement must be `export const meta = { name, description, phases? }`. Available globals: agent(), parallel(), pipeline(), phase(), log(), args, cwd, budget. Use `agent("prompt", { label, type })` or `agent({ instruction, label/name, type })`. Use `parallel([() => agent(...), () => agent(...)])` for independent fan-out; writing `await agent(...); await agent(...)` is serial. The workflow must call agent() at least once.',
        },
        args: {
          description: "Optional JSON value exposed to the workflow script as global `args`.",
        },
        mode: {
          type: "string",
          enum: ["run", "dry_run", "validate_only"],
          description:
            "run executes internal subagents. dry_run executes the script with stub agents and returns planned agents. validate_only only parses metadata and safety checks.",
        },
        concurrency: {
          type: "number",
          description:
            "Maximum concurrent workflow agent tasks. Default 3, hard-capped by runtime.",
        },
        max_agents: {
          type: "number",
          description: "Maximum number of agent() calls the workflow may start. Default 8.",
        },
        token_budget: {
          type: "number",
          description: "Optional workflow token budget exposed through budget.remaining().",
        },
        background: {
          type: "boolean",
          description:
            "When true, start the workflow in the session WorkflowRunManager and return a run_id immediately. Default false keeps the current synchronous behavior.",
        },
        tool_mode: {
          type: "string",
          enum: ["read_only", "full"],
          description:
            "read_only is the default and limits workflow subagents to safe read/search tools. full allows inherited tools but still uses Reasonix gates and is unavailable in plan mode.",
        },
      },
      required: ["script"],
    },
    fn: async (args, ctx) => {
      const script = normalizeWorkflowScript(args.script);
      if (!script) {
        return JSON.stringify({ success: false, error: "workflow requires a non-empty script" });
      }

      const mode = workflowMode(args.mode);
      const toolMode = workflowToolMode(args.tool_mode);
      const runner =
        mode === "run"
          ? (opts.runner ??
            new ReasonixWorkflowAgentRunner({
              client: getClient(),
              parentRegistry: registry,
              sink: opts.subagentSink,
              defaultModel: opts.defaultModel,
              toolMode,
            }))
          : opts.runner;

      try {
        if (args.background === true) {
          if (!opts.manager) {
            return JSON.stringify({
              success: false,
              error: "background workflow requires a WorkflowRunManager",
            });
          }
          const started = opts.manager.startRun({
            script,
            mode,
            args: args.args,
            concurrency: numberOption(args.concurrency),
            maxAgents: numberOption(args.max_agents),
            tokenBudget: numberOption(args.token_budget) ?? null,
            runner,
          });
          return JSON.stringify({
            success: true,
            run_id: started.id,
            status: started.status,
            meta: { name: started.name, description: started.description },
          });
        }

        const result = await runWorkflow(script, {
          mode,
          args: args.args,
          cwd: opts.rootDir,
          concurrency: numberOption(args.concurrency),
          maxAgents: numberOption(args.max_agents),
          tokenBudget: numberOption(args.token_budget) ?? null,
          signal: ctx?.signal,
          runner,
        });
        if (mode !== "validate_only" && result.success && result.agentCount === 0) {
          return JSON.stringify({
            ...formatWorkflowResult(result),
            success: false,
            error: "workflow scripts must call agent() at least once",
          });
        }
        return JSON.stringify(formatWorkflowResult(result));
      } catch (error) {
        return JSON.stringify({
          success: false,
          error: error instanceof Error ? error.message : String(error),
        });
      }
    },
  });

  return registry;
}

function normalizeWorkflowScript(value: unknown): string {
  if (typeof value !== "string") return "";
  let text = value.trim();
  const fence = text.match(/^```(?:js|javascript)?\s*\n([\s\S]*?)\n```$/i);
  if (fence?.[1]) text = fence[1].trim();
  return text;
}

function workflowMode(value: unknown): WorkflowMode {
  return value === "dry_run" || value === "validate_only" ? value : "run";
}

function workflowToolMode(value: unknown): WorkflowToolMode {
  return value === "full" ? "full" : "read_only";
}

function numberOption(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function formatWorkflowResult(result: WorkflowRunResult): Record<string, unknown> {
  const out: Record<string, unknown> = {
    success: result.success,
    meta: result.meta,
    logs: result.logs,
    phases: result.phases,
    agent_count: result.agentCount,
    duration_ms: result.durationMs,
  };
  if (result.result !== undefined) out.result = result.result;
  if (result.error) out.error = result.error;
  if (result.plannedAgents) out.planned_agents = result.plannedAgents;
  if (result.usage) out.usage = result.usage;
  if (result.costUsd !== undefined) out.cost_usd = result.costUsd;
  return out;
}
