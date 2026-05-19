import type { ToolInterceptor } from "../tools.js";
import type { PlanStep, StepEvidence } from "../tools/plan.js";

export type EngineeringLifecycleMode = "off" | "auto" | "strict";
export type EngineeringLifecyclePromptClass = "small" | "large";
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

const LARGE_PROMPT_PATTERNS = [
  /\brefactor\b/i,
  /\bmigration\b/i,
  /\bmigrate\b/i,
  /\barchitecture\b/i,
  /\bpublic API\b/i,
  /\bbreaking\b/i,
  /\bacross\b/i,
  /\bmulti[- ]?file\b/i,
  /重构/,
  /迁移/,
  /架构/,
  /多文件/,
  /大型/,
  /强约束/,
];

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

export function classifyEngineeringPrompt(text: string): EngineeringLifecyclePromptClass {
  const trimmed = text.trim();
  if (!trimmed) return "small";
  return LARGE_PROMPT_PATTERNS.some((rx) => rx.test(trimmed)) ? "large" : "small";
}

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
    this._mode = opts.mode ?? "auto";
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

  observeUserPrompt(text: string): void {
    if (this._mode === "off") return;
    if (this._state === "complete" || this._state === "cancelled") {
      this.reset();
    }
    if (this._mode === "strict") {
      if (this._state === "idle") this._state = "armed";
      return;
    }
    if (this._state === "idle" && classifyEngineeringPrompt(text) === "large") {
      this._state = "armed";
    }
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

    if (this._mode === "auto" && this._state === "idle") {
      this._state = "armed";
    }

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
  return (
    /\b(npm|pnpm|yarn)\s+(install|add|remove|update)\b/i.test(command) ||
    /\b(git)\s+(push|reset|clean|checkout|switch)\b/i.test(command) ||
    /\b(rm|mv|cp)\b/.test(command)
  );
}
