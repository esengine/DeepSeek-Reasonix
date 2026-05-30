import type { DeepSeekClient } from "../client.js";
import type { ToolRegistry } from "../tools.js";
import {
  type SpawnSubagentOptions,
  type SubagentResult,
  type SubagentSink,
  spawnSubagent,
} from "../tools/subagent.js";
import type {
  WorkflowAgentOptions,
  WorkflowAgentResult,
  WorkflowAgentRunner,
  WorkflowToolMode,
} from "./types.js";

export interface ReasonixWorkflowAgentRunnerOptions {
  client: DeepSeekClient;
  parentRegistry: ToolRegistry;
  sink?: SubagentSink;
  defaultModel?: string;
  toolMode?: WorkflowToolMode;
  spawn?: (opts: SpawnSubagentOptions) => Promise<SubagentResult>;
}

const ORCHESTRATION_TOOLS = new Set(["workflow", "spawn_subagent", "submit_plan"]);
const DEFAULT_READ_ONLY_TOOLS = [
  "read_file",
  "list_directory",
  "directory_tree",
  "search_files",
  "search_content",
  "get_file_info",
  "semantic_search",
  "web_search",
  "web_fetch",
  "recall_memory",
  "find_in_code",
  "find_references",
  "find_symbol",
] as const;

export class ReasonixWorkflowAgentRunner implements WorkflowAgentRunner {
  private readonly client: DeepSeekClient;
  private readonly parentRegistry: ToolRegistry;
  private readonly sink?: SubagentSink;
  private readonly defaultModel?: string;
  private readonly toolMode: WorkflowToolMode;
  private readonly spawn: (opts: SpawnSubagentOptions) => Promise<SubagentResult>;

  constructor(opts: ReasonixWorkflowAgentRunnerOptions) {
    this.client = opts.client;
    this.parentRegistry = opts.parentRegistry;
    this.sink = opts.sink;
    this.defaultModel = opts.defaultModel;
    this.toolMode = opts.toolMode ?? "read_only";
    this.spawn = opts.spawn ?? spawnSubagent;
  }

  async run(prompt: string, opts: WorkflowAgentOptions): Promise<WorkflowAgentResult> {
    const label = opts.label ?? "agent";
    const result = await this.spawn({
      client: this.client,
      parentRegistry: this.parentRegistry,
      system: workflowSubagentSystem(opts),
      task: prompt,
      model: opts.model ?? this.defaultModel,
      sink: this.sink,
      parentSignal: opts.signal,
      skillName: `workflow:${opts.workflowName}:${label}`,
      allowedTools: this.allowedTools(opts.allowedTools),
    });

    return {
      ok: result.success,
      output: result.output,
      error: result.error,
      raw: result,
    };
  }

  private allowedTools(requested: readonly string[] | undefined): readonly string[] | undefined {
    if (requested) return this.filterRegisteredTools(requested);
    if (this.toolMode === "full") return undefined;
    return this.filterRegisteredTools(DEFAULT_READ_ONLY_TOOLS);
  }

  private filterRegisteredTools(names: readonly string[]): readonly string[] {
    return names.filter((name) => this.parentRegistry.has(name) && !ORCHESTRATION_TOOLS.has(name));
  }
}

function workflowSubagentSystem(opts: WorkflowAgentOptions): string {
  const lines = [
    "You are a Reasonix workflow subagent. The workflow runtime spawned you for one focused task.",
    "Return one complete, self-contained answer. The parent workflow sees only your final answer.",
  ];
  if (opts.phase) lines.push(`Workflow phase: ${opts.phase}`);
  if (opts.label) lines.push(`Task label: ${opts.label}`);
  if (opts.type) lines.push(`Workflow agent type: ${opts.type}`);
  return lines.join("\n");
}
