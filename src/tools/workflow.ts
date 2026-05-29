import { DeepSeekClient } from "../client.js";
import { loadEndpoint } from "../config.js";
import type { ToolCallContext, ToolRegistry } from "../tools.js";
import { ReasonixWorkflowAgentRunner } from "../workflow/agent-runner.js";
import type { WorkflowRunManager } from "../workflow/manager.js";
import { type ParsedWorkflowScript, parseWorkflowScript } from "../workflow/parser.js";
import { runWorkflow } from "../workflow/runtime.js";
import type {
  WorkflowAgentRunner,
  WorkflowMode,
  WorkflowModelPolicy,
  WorkflowRunResult,
  WorkflowRunSnapshot,
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
  defaultModelPolicy?: WorkflowModelPolicy | (() => WorkflowModelPolicy);
}

const WORKFLOW_DESCRIPTION =
  'Execute a deterministic JavaScript workflow that orchestrates multiple internal Reasonix subagents with agent(), parallel(), pipeline(), phase(), log(), verifyFindings(), adversarialReview(), and synthesize(). Use for repository-wide audits, large refactors, migrations, multi-module bug hunts, architecture/security review, test coverage sweeps, multi-perspective verification, or when the user explicitly asks for workflow/dynamic workflow/parallel agents/subagents/fan-out analysis. For non-trivial audit/review/research workflows, prefer this orchestration shape: scope/inventory phase -> parallel agents for independent workstreams -> verifier or adversarial review agents for important findings -> synthesis agent for the final answer. For independent subtasks, use parallel([() => agent("task", { label: "..." }), ...]); consecutive `await agent(...)` calls are serial and should only be used when each task depends on the previous result. Use pipeline() when stages have real dependencies instead of forcing fan-out. In cost-sensitive workflows, limit verifier agents to high-severity, high-uncertainty, or sampled findings. agent() accepts agent(promptString, opts) or agent({ instruction, label/name, type, phase, model, allowedTools }). Do not use for simple single-file edits, small bug fixes, direct Q&A, formatting, tasks that do not need parallel analysis, or when the user asks not to spawn subagents. Script must be raw JavaScript, start with export const meta = { name, description, phases? }, and call agent() at least once.';

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
            'Required raw JavaScript workflow script, with no Markdown fences. First statement must be `export const meta = { name, description, phases? }`. Available globals: agent(), parallel(), pipeline(), verifyFindings(), adversarialReview(), synthesize(), phase(), log(), args, cwd, budget. For large audits/reviews, write scripts as scope/inventory phase -> parallel agents -> verifier/adversarial review -> synthesis. Use `agent("prompt", { label, type })` or `agent({ instruction, label/name, type })`. Use `parallel([() => agent(...), () => agent(...)])` for independent fan-out; writing `await agent(...); await agent(...)` is serial. Use pipeline() for dependency-heavy stages. Use verifyFindings([...]) for independent verifier subagents, adversarialReview(result) to challenge a draft, and synthesize(inputs) for final synthesis. For cost-sensitive runs, verify only high-risk or sampled findings. The workflow must call agent() at least once.',
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
        model_policy: {
          type: "string",
          enum: ["inherit", "flash", "pro", "mixed", "auto"],
          description:
            "Workflow subagent model routing policy. Precedence: agent({ model }) > phase model > model_policy > session default. inherit leaves subagents on the runner default, flash forces deepseek-v4-flash, pro forces deepseek-v4-pro, mixed/auto route explore/inventory scans to flash and verify/adversarial/synthesis work to pro.",
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
      const modelPolicy = workflowModelPolicy(args.model_policy, opts.defaultModelPolicy);
      const parsed = parseWorkflowScript(script);
      const confirmation = await confirmWorkflowRun(args, ctx, parsed, mode, toolMode);
      if (confirmation) return confirmation;
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
            modelPolicy,
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

        if (opts.manager && mode === "run") {
          const started = opts.manager.startRun({
            script,
            mode,
            args: args.args,
            concurrency: numberOption(args.concurrency),
            maxAgents: numberOption(args.max_agents),
            modelPolicy,
            tokenBudget: numberOption(args.token_budget) ?? null,
            runner,
          });
          const abort = (): void => {
            if (opts.manager?.getRun(started.id)?.status === "running")
              opts.manager.stopRun(started.id);
          };
          ctx?.signal?.addEventListener("abort", abort, { once: true });
          const done = await opts.manager.waitForRun(started.id);
          ctx?.signal?.removeEventListener("abort", abort);
          return JSON.stringify(formatWorkflowSnapshot(done));
        }

        const result = await runWorkflow(script, {
          mode,
          args: args.args,
          cwd: opts.rootDir,
          concurrency: numberOption(args.concurrency),
          maxAgents: numberOption(args.max_agents),
          modelPolicy,
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

async function confirmWorkflowRun(
  args: Record<string, unknown>,
  ctx: ToolCallContext | undefined,
  parsed: ParsedWorkflowScript,
  mode: WorkflowMode,
  toolMode: WorkflowToolMode,
): Promise<string | null> {
  const background = args.background === true;
  const maxAgents = numberOption(args.max_agents) ?? 8;
  const concurrency = numberOption(args.concurrency) ?? 3;
  const needsGate =
    mode === "run" && (toolMode === "full" || background || maxAgents > 8 || concurrency > 4);
  if (!needsGate) return null;
  if (!ctx?.confirmationGate) {
    return JSON.stringify({
      success: false,
      error: "workflow requires interactive confirmation for this run",
      rejectedReason: "workflow-confirmation",
    });
  }
  const verdict = await ctx.confirmationGate.ask({
    kind: "workflow_confirm",
    payload: {
      name: parsed.meta.name,
      description: parsed.meta.description,
      mode,
      toolMode,
      background,
      concurrency,
      maxAgents,
      phases: parsed.meta.phases?.map((phase) => phase.title) ?? [],
    },
  });
  if (verdict.type === "deny") {
    return JSON.stringify({
      success: false,
      error: "workflow denied by user",
      rejectedReason: "workflow-confirmation",
    });
  }
  return null;
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

function workflowModelPolicy(
  value: unknown,
  fallback: WorkflowModelPolicy | (() => WorkflowModelPolicy) | undefined,
): WorkflowModelPolicy {
  if (
    value === "inherit" ||
    value === "flash" ||
    value === "pro" ||
    value === "mixed" ||
    value === "auto"
  ) {
    return value;
  }
  return typeof fallback === "function" ? fallback() : (fallback ?? "mixed");
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

function formatWorkflowSnapshot(snapshot: WorkflowRunSnapshot): Record<string, unknown> {
  const out: Record<string, unknown> = {
    success: snapshot.status === "completed",
    run_id: snapshot.id,
    status: snapshot.status,
    meta: { name: snapshot.name, description: snapshot.description },
    logs: snapshot.logs,
    phases: snapshot.phases.map((phase) => phase.title),
    agent_count: snapshot.agentCount,
    duration_ms: snapshot.durationMs ?? 0,
  };
  if (snapshot.result !== undefined) out.result = snapshot.result;
  if (snapshot.error) out.error = snapshot.error;
  if (snapshot.agents.length > 0) {
    out.agents = snapshot.agents.map((agent) => ({
      id: agent.id,
      label: agent.label,
      phase: agent.phase,
      status: agent.status,
      prompt_preview: agent.promptPreview,
      output_preview: agent.outputPreview,
      error: agent.error,
      duration_ms: agent.durationMs,
    }));
  }
  if (snapshot.usage) out.usage = snapshot.usage;
  if (snapshot.costUsd !== undefined) out.cost_usd = snapshot.costUsd;
  return out;
}
