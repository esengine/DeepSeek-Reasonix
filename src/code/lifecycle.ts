import type { ToolInterceptor } from "../tools.js";
import type { PlanStep, StepEvidence } from "../tools/plan.js";

export type EngineeringLifecycleMode = "off" | "strict";
export type EngineeringLifecycleState =
  | "idle"
  | "armed"
  | "planning"
  | "approved"
  | "executing"
  | "checkpoint"
  | "complete"
  | "cancelled";

export interface EngineeringLifecycleSnapshot {
  mode: EngineeringLifecycleMode;
  state: EngineeringLifecycleState;
  planSteps: PlanStep[];
  completedStepIds: string[];
  mutatedSinceLastStep: boolean;
}

export interface EngineeringLifecycleOptions {
  mode?: EngineeringLifecycleMode;
}

const SAFE_TOOL_NAMES = new Set([
  "read_file",
  "list_directory",
  "directory_tree",
  "search_files",
  "search_content",
  "glob",
  "get_file_info",
  "semantic_search",
  "web_search",
  "web_fetch",
  "recall_memory",
  "todo_write",
  "ask_choice",
  "submit_plan",
  "mark_step_complete",
  "revise_plan",
  "job_output",
  "wait_for_job",
  "list_jobs",
]);

const HIGH_RISK_TOOL_NAMES = new Set([
  "multi_edit",
  "move_file",
  "delete_file",
  "delete_directory",
  "copy_file",
  "create_directory",
  "run_background",
  "stop_job",
]);

const MUTATION_TOOL_NAMES = new Set([
  "edit_file",
  "write_file",
  "multi_edit",
  "move_file",
  "delete_file",
  "delete_directory",
  "copy_file",
  "create_directory",
  "run_background",
  "stop_job",
]);

export function isHighRiskLifecycleToolCall(name: string, args: Record<string, unknown>): boolean {
  if (HIGH_RISK_TOOL_NAMES.has(name)) return true;
  if (SAFE_TOOL_NAMES.has(name)) return false;
  if (name === "write_file") {
    const path = typeof args.path === "string" ? args.path : "";
    return isPackageOrConfigPath(path);
  }
  if (name === "edit_file") {
    const path = typeof args.path === "string" ? args.path : "";
    return isPackageOrConfigPath(path);
  }
  if (name === "run_command") {
    const command = typeof args.command === "string" ? args.command : "";
    return isHighRiskCommand(command);
  }
  return false;
}

export function isLifecycleMutationToolCall(name: string, args: Record<string, unknown>): boolean {
  if (MUTATION_TOOL_NAMES.has(name)) return true;
  if (name === "run_command") {
    const command = typeof args.command === "string" ? args.command : "";
    return isHighRiskCommand(command);
  }
  return false;
}

export class EngineeringLifecycleRuntime {
  private _mode: EngineeringLifecycleMode;
  private _state: EngineeringLifecycleState = "idle";
  private _planSteps: PlanStep[] = [];
  private readonly _completedStepIds = new Set<string>();
  private _mutatedSinceLastStep = false;

  constructor(opts: EngineeringLifecycleOptions = {}) {
    this._mode = opts.mode ?? "off";
    if (this._mode === "strict") this._state = "armed";
  }

  get mode(): EngineeringLifecycleMode {
    return this._mode;
  }

  setMode(mode: EngineeringLifecycleMode): void {
    this._mode = mode;
    if (mode === "off") {
      this.reset();
      return;
    }
    if (mode === "strict" && this._state === "idle") this._state = "armed";
  }

  observeUserPrompt(_text: string): void {
    if (this._mode === "off") return;
    if (this._state === "complete" || this._state === "cancelled") {
      this.reset();
    }
    if (this._state === "idle") this._state = "armed";
  }

  recordPlanProposed(steps?: readonly PlanStep[]): void {
    if (this._mode === "off") return;
    this._state = "planning";
    this._planSteps = [...(steps ?? [])];
    this._completedStepIds.clear();
    this._mutatedSinceLastStep = false;
  }

  recordPlanApproved(steps?: readonly PlanStep[]): void {
    if (this._mode === "off") return;
    this._state = "approved";
    this._planSteps = [...(steps ?? this._planSteps)];
    this._completedStepIds.clear();
    this._mutatedSinceLastStep = false;
  }

  recordPlanRevised(remainingSteps: readonly PlanStep[]): void {
    if (this._mode === "off") return;

    const donePrefix = this._planSteps.filter((step) => this._completedStepIds.has(step.id));
    const merged: PlanStep[] = [...donePrefix];
    for (const step of remainingSteps) {
      if (this._completedStepIds.has(step.id)) continue;
      merged.push(step);
    }

    this._planSteps = merged;
    if (this._planSteps.length > 0 && this._completedStepIds.size >= this._planSteps.length) {
      this._state = "complete";
    } else {
      this._state = "executing";
    }
  }

  recordStepCompleted(stepId: string): void {
    if (!stepId) return;
    this._completedStepIds.add(stepId);
    this._mutatedSinceLastStep = false;
    if (this._planSteps.length > 0 && this._completedStepIds.size >= this._planSteps.length) {
      this._state = "complete";
    } else if (this._state !== "idle" && this._state !== "cancelled") {
      this._state = "executing";
    }
  }

  recordToolResult(name: string, args: Record<string, unknown>, result: string): void {
    if (this._mode === "off") return;
    if (!isLifecycleMutationToolCall(name, args)) return;
    if (!toolResultLooksSuccessful(result)) return;
    if (this._state === "approved" || this._state === "executing") {
      this._state = "executing";
      this._mutatedSinceLastStep = true;
    }
  }

  cancel(): void {
    this._state = "cancelled";
    this._planSteps = [];
    this._completedStepIds.clear();
    this._mutatedSinceLastStep = false;
  }

  reset(): void {
    this._state = this._mode === "strict" ? "armed" : "idle";
    this._planSteps = [];
    this._completedStepIds.clear();
    this._mutatedSinceLastStep = false;
  }

  guardToolCall: ToolInterceptor = (name, args) => {
    if (this._mode === "off") return null;
    if (name === "mark_step_complete") return this.guardStepCompletion(args);
    if (!isHighRiskLifecycleToolCall(name, args)) return null;

    if (this._state !== "approved" && this._state !== "executing") {
      return JSON.stringify({
        error: `${name}: blocked by Engineering Lifecycle — high-risk engineering work requires an approved structured plan first. Explore with read-only tools, call ask_choice for real forks, then call submit_plan with concrete steps.`,
        rejectedReason: "engineering-lifecycle",
        state: this._state,
      });
    }

    this._state = "executing";
    return null;
  };

  snapshot(): EngineeringLifecycleSnapshot {
    return {
      mode: this._mode,
      state: this._state,
      planSteps: [...this._planSteps],
      completedStepIds: [...this._completedStepIds],
      mutatedSinceLastStep: this._mutatedSinceLastStep,
    };
  }

  private guardStepCompletion(args: Record<string, unknown>): string | null {
    const stepId = typeof args.stepId === "string" ? args.stepId.trim() : "";
    const step = this._planSteps.find((s) => s.id === stepId);
    const evidence = Array.isArray(args.evidence) ? (args.evidence as StepEvidence[]) : [];
    const evidenceRequired =
      this._mutatedSinceLastStep ||
      step?.risk === "med" ||
      step?.risk === "high" ||
      (step?.verification?.length ?? 0) > 0;
    if (evidenceRequired && evidence.length === 0) {
      return JSON.stringify({
        error:
          "mark_step_complete: evidence required by Engineering Lifecycle — include verification, diff, checkpoint, or manual evidence before completing this step.",
        rejectedReason: "engineering-lifecycle-evidence",
        stepId,
      });
    }
    return null;
  }
}

function toolResultLooksSuccessful(result: string): boolean {
  const text = result.trim();
  if (!text) return false;
  try {
    const parsed = JSON.parse(text) as unknown;
    if (parsed && typeof parsed === "object" && "error" in parsed) return false;
  } catch {
    // Non-JSON tool results are normal.
  }
  if (/\b0\/\d+\s+applied\b/i.test(text)) return false;
  return !/(user rejected|rejected this edit|discarded|unavailable in plan mode|interceptor failed|\berror\b|failed)/i.test(
    text,
  );
}

function isPackageOrConfigPath(path: string): boolean {
  const normalized = path.replaceAll("\\", "/").toLowerCase();
  return (
    /(^|\/)package(-lock)?\.json$/.test(normalized) ||
    /(^|\/)pnpm-lock\.yaml$/.test(normalized) ||
    /(^|\/)yarn\.lock$/.test(normalized) ||
    /(^|\/)tsconfig[^/]*\.json$/.test(normalized) ||
    /(^|\/)vitest\.config\./.test(normalized) ||
    /(^|\/)biome\.json$/.test(normalized) ||
    normalized.startsWith(".github/workflows/")
  );
}

function isHighRiskCommand(command: string): boolean {
  const tokens = shellTokens(command);
  for (let i = 0; i < tokens.length; i++) {
    const token = tokens[i]?.toLowerCase();
    if (!token || !isCommandPosition(tokens, i)) continue;
    if (
      (token === "npm" || token === "pnpm" || token === "yarn") &&
      isPackageMutation(tokens[i + 1])
    ) {
      return true;
    }
    if (token === "git" && isHighRiskGitCommand(tokens.slice(i + 1))) return true;
    if (token === "rm" || token === "mv" || token === "cp") return true;
  }
  return false;
}

function shellTokens(command: string): string[] {
  const out: string[] = [];
  let current = "";
  let quote: "'" | '"' | null = null;
  for (let i = 0; i < command.length; i++) {
    const ch = command[i] ?? "";
    if (quote) {
      if (ch === quote) quote = null;
      else current += ch;
      continue;
    }
    if (ch === "'" || ch === '"') {
      quote = ch;
      continue;
    }
    if (/\s/.test(ch)) {
      if (current) {
        out.push(current);
        current = "";
      }
      continue;
    }
    if (ch === ";" || ch === "|" || ch === "&") {
      if (current) {
        out.push(current);
        current = "";
      }
      const next = command[i + 1];
      if ((ch === "|" || ch === "&") && next === ch) {
        out.push(`${ch}${next}`);
        i++;
      } else {
        out.push(ch);
      }
      continue;
    }
    current += ch;
  }
  if (current) out.push(current);
  return out;
}

function isCommandPosition(tokens: string[], index: number): boolean {
  if (index === 0) return true;
  const previous = tokens[index - 1];
  return previous === ";" || previous === "|" || previous === "&&" || previous === "||";
}

function isPackageMutation(token: string | undefined): boolean {
  const normalized = token?.toLowerCase();
  return (
    normalized === "install" ||
    normalized === "add" ||
    normalized === "remove" ||
    normalized === "update"
  );
}

function isHighRiskGitCommand(args: string[]): boolean {
  const subcommandIndex = args.findIndex((arg) => arg && !arg.startsWith("-"));
  const subcommand = args[subcommandIndex]?.toLowerCase();
  if (!subcommand) return false;
  if (
    subcommand === "push" ||
    subcommand === "reset" ||
    subcommand === "clean" ||
    subcommand === "switch"
  ) {
    return true;
  }
  if (subcommand !== "checkout") return false;
  const checkoutArgs = args.slice(subcommandIndex + 1);
  if (checkoutArgs[0] === "--") return false;
  if (checkoutArgs.some((arg) => arg === "-b" || arg === "-B" || arg === "--orphan")) return true;
  const positional = checkoutArgs.filter((arg) => arg && !arg.startsWith("-"));
  if (positional.length === 0) return false;
  return !positional.every(looksLikePathCheckout);
}

function looksLikePathCheckout(arg: string): boolean {
  return arg.includes("/") || arg.includes("\\") || arg.includes(".");
}
