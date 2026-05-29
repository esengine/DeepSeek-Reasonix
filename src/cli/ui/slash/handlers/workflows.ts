import { loadSavedWorkflows } from "../../../../workflow/saved.js";
import type { WorkflowRunSnapshot } from "../../../../workflow/types.js";
import type { SlashHandler } from "../dispatch.js";
import type { SlashContext, SlashResult } from "../types.js";

export function handleWorkflowsSlash(
  args: readonly string[],
  ctx: Pick<
    SlashContext,
    "workflowManager" | "workflowRunner" | "workflowModelPolicy" | "codeRoot" | "homeDir"
  >,
): SlashResult {
  const manager = ctx.workflowManager;
  if (!manager) return { info: "workflows are not available in this session" };

  const [cmd, runId, target, name] = args;
  if (!cmd) return { info: formatRunList(manager.listRuns()) };

  if (cmd === "show" && runId) {
    const run = manager.getRun(runId);
    return { info: run ? formatRunDetail(run) : `workflow run not found: ${runId}` };
  }

  if (cmd === "stop" && runId) {
    const run = manager.stopRun(runId);
    return { info: `workflow ${run.id} ${run.status}` };
  }

  if (cmd === "retry" && runId) {
    const started = manager.retryRun(runId, {
      runner: ctx.workflowRunner,
      modelPolicy: ctx.workflowModelPolicy?.(),
    });
    return { info: `started retry ${started.id} from ${runId}` };
  }

  if (cmd === "delete" && runId) {
    manager.deleteRun(runId);
    return { info: `deleted workflow ${runId}` };
  }

  if (cmd === "save" && runId && (target === "project" || target === "user")) {
    const saved = manager.saveRun(runId, target, name);
    return { info: `saved workflow ${saved.name} to ${saved.path}` };
  }

  if (cmd === "run" && runId) {
    const rootDir = ctx.codeRoot ?? process.cwd();
    const homeDir = ctx.homeDir ?? process.env.HOME ?? rootDir;
    const workflow = loadSavedWorkflows({ rootDir, homeDir }).find((item) => item.name === runId);
    if (!workflow) return { info: `saved workflow not found: ${runId}` };
    const started = manager.startRun({
      script: workflow.script,
      mode: "run",
      args: { input: args.slice(2).join(" ") },
      runner: ctx.workflowRunner,
      modelPolicy: ctx.workflowModelPolicy?.(),
    });
    return { info: `workflow ${started.id} running ${workflow.name}` };
  }

  return {
    info: "usage: /workflows | /workflows show <runId> | /workflows stop <runId> | /workflows retry <runId> | /workflows delete <runId> | /workflows save <runId> project|user [name] | /workflows run <name> [input]",
  };
}

export const handlers: Record<string, SlashHandler> = {
  workflows: (args, _loop, ctx) => handleWorkflowsSlash(args, ctx),
};

function formatRunList(runs: WorkflowRunSnapshot[]): string {
  if (runs.length === 0) return "no workflow runs";
  return runs
    .map((run) => `${run.id}  ${run.status}  ${run.name}  agents=${run.agentCount}`)
    .join("\n");
}

function formatRunDetail(run: WorkflowRunSnapshot): string {
  const agents = run.agents
    .map(
      (agent) =>
        `- ${agent.status} ${agent.label}${agent.outputPreview ? `: ${agent.outputPreview}` : ""}${agent.error ? ` error=${agent.error}` : ""}`,
    )
    .join("\n");
  return [
    `${run.id}  ${run.status}  ${run.name}`,
    run.description,
    `agents=${run.agentCount} duration=${run.durationMs ?? 0}ms`,
    agents,
    run.error ? `error: ${run.error}` : "",
  ]
    .filter(Boolean)
    .join("\n");
}
