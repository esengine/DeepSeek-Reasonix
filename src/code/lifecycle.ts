import type { ToolInterceptor } from "../tools.js";
import type { PlanStep, StepEvidence } from "../tools/plan.js";
import { classifyLifecycleToolCall } from "./lifecycle-policy.js";

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

export function isHighRiskLifecycleToolCall(name: string, args: Record<string, unknown>): boolean {
  return classifyLifecycleToolCall(name, args).risk === "high-risk";
}

export function isLifecycleMutationToolCall(name: string, args: Record<string, unknown>): boolean {
  return classifyLifecycleToolCall(name, args).risk !== "safe";
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

  recordCheckpointReached(): void {
    if (this._mode === "off") return;
    if (this._state === "approved" || this._state === "executing") {
      this._state = "checkpoint";
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
        error: `${name}: blocked by Engineering Lifecycle — submit an approved plan before high-risk mutation.`,
        rejectedReason: "engineering-lifecycle",
        state: this._state,
        nextAction: "submit_plan",
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
          "mark_step_complete: evidence required — add verification, diff, checkpoint, or manual evidence.",
        rejectedReason: "engineering-lifecycle-evidence",
        stepId,
        nextAction: "add_evidence",
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
