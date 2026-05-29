import type { CacheFirstLoop } from "../../../../loop.js";
import type { ChatMessage } from "../../../../types.js";
import { loadSavedWorkflows } from "../../../../workflow/saved.js";
import type { WorkflowRunSnapshot } from "../../../../workflow/types.js";
import type { SlashHandler } from "../dispatch.js";
import type { SlashContext, SlashResult } from "../types.js";

type WorkflowAttachLoop = Pick<CacheFirstLoop, "appendAndPersist">;

export function handleWorkflowsSlash(
  args: readonly string[],
  ctx: Pick<
    SlashContext,
    "workflowManager" | "workflowRunner" | "workflowModelPolicy" | "codeRoot" | "homeDir"
  >,
  loop?: WorkflowAttachLoop,
): SlashResult {
  const manager = ctx.workflowManager;
  if (!manager) return { info: "workflows are not available in this session" };

  const [cmd, runId, target, name] = args;
  if (!cmd || cmd === "list") return { info: formatRunList(manager.listRuns()) };

  if (cmd === "show" && runId) {
    const run = manager.getRun(runId);
    return { info: run ? formatRunDetail(run) : `workflow run not found: ${runId}` };
  }

  if ((cmd === "attach" || cmd === "use") && runId) {
    const run = manager.getRun(runId);
    if (!run) return { info: `workflow run not found: ${runId}` };
    if (run.status === "running") {
      return { info: `workflow ${runId} is still running; use /workflows show ${runId}` };
    }
    if (!loop) return { info: "workflow attach is not available in this session" };
    appendWorkflowContext(loop, run);
    return { info: `attached workflow ${run.id} to the current conversation` };
  }

  if (cmd === "continue" && runId) {
    const run = manager.getRun(runId);
    if (!run) return { info: `workflow run not found: ${runId}` };
    if (run.status === "running") {
      return { info: `workflow ${runId} is still running; use /workflows show ${runId}` };
    }
    if (!loop) return { info: "workflow continue is not available in this session" };
    appendWorkflowContext(loop, run);
    const instruction = args.slice(2).join(" ").trim();
    return {
      info: `attached workflow ${run.id} to the current conversation`,
      resubmit: instruction || `Continue from workflow ${run.id}.`,
    };
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
    info: "usage: /workflows [list] | /workflows show <runId> | /workflows attach <runId> | /workflows continue <runId> [instruction] | /workflows stop <runId> | /workflows retry <runId> | /workflows delete <runId> | /workflows save <runId> project|user [name] | /workflows run <name> [input]",
  };
}

export const handlers: Record<string, SlashHandler> = {
  workflows: (args, loop, ctx) => handleWorkflowsSlash(args, ctx, loop),
};

function appendWorkflowContext(loop: WorkflowAttachLoop, run: WorkflowRunSnapshot): void {
  loop.appendAndPersist({
    role: "system",
    content: formatRunContext(run),
  } satisfies ChatMessage);
}

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
    run.error ? `error${run.errorKind ? ` (${run.errorKind})` : ""}: ${run.error}` : "",
  ]
    .filter(Boolean)
    .join("\n");
}

function formatRunContext(run: WorkflowRunSnapshot): string {
  return [
    `Workflow run attached: ${run.id}`,
    `Name: ${run.name}`,
    `Description: ${run.description}`,
    `Status: ${run.status}`,
    `Mode: ${run.mode}`,
    `Agents: ${run.agentCount}`,
    run.error ? `Error${run.errorKind ? ` (${run.errorKind})` : ""}: ${run.error}` : "",
    run.phases.length > 0
      ? `Phases:\n${run.phases.map((phase) => `- ${phase.title} agents=${phase.agentCount}`).join("\n")}`
      : "",
    run.agents.length > 0
      ? `Agent results:\n${run.agents
          .map(
            (agent) =>
              `- ${agent.status} ${agent.label}${agent.phase ? ` (${agent.phase})` : ""}${agent.outputPreview ? `: ${agent.outputPreview}` : ""}${agent.error ? ` error=${agent.error}` : ""}`,
          )
          .join("\n")}`
      : "",
    run.logs.length > 0
      ? `Logs:\n${run.logs
          .slice(-20)
          .map((line) => `- ${line}`)
          .join("\n")}`
      : "",
    `Result:\n${truncateWorkflowContextValue(run.result)}`,
  ]
    .filter(Boolean)
    .join("\n\n");
}

function truncateWorkflowContextValue(value: unknown): string {
  const text = typeof value === "string" ? value : JSON.stringify(value ?? null, null, 2);
  return text.length > 12_000 ? `${text.slice(0, 12_000)}\n[truncated]` : text;
}
