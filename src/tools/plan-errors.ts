/** Plan-mode errors carry `toToolResult` so dispatch serializes structured payloads the TUI parses to mount pickers. */

import type { PlanStep } from "./plan-types.js";

export class PlanProposedError extends Error {
  readonly plan: string;
  readonly steps?: PlanStep[];
  readonly summary?: string;
  constructor(plan: string, steps?: PlanStep[], summary?: string) {
    super(
      "PlanProposedError: plan submitted. STOP calling tools now — the TUI has shown the plan to the user. Wait for their next message; it will either approve (you'll then implement the plan), request a refinement (you should explore more and submit an updated plan), or cancel (drop the plan and ask what they want instead). Don't call any tools in the meantime.",
    );
    this.name = "PlanProposedError";
    this.plan = plan;
    this.steps = steps;
    this.summary = summary;
  }

  toToolResult(): { error: string; plan: string; steps?: PlanStep[]; summary?: string } {
    const payload: { error: string; plan: string; steps?: PlanStep[]; summary?: string } = {
      error: `${this.name}: ${this.message}`,
      plan: this.plan,
    };
    if (this.steps && this.steps.length > 0) payload.steps = this.steps;
    if (this.summary) payload.summary = this.summary;
    return payload;
  }
}

/** Surgical replace of in-flight plan tail; submit_plan would reset done steps. */
export class PlanRevisionProposedError extends Error {
  readonly reason: string;
  readonly remainingSteps: PlanStep[];
  readonly summary?: string;
  constructor(reason: string, remainingSteps: PlanStep[], summary?: string) {
    super(
      "PlanRevisionProposedError: revision submitted. STOP calling tools now — the TUI has paused for the user to review your proposed change. Wait for their next message; it will say 'revision accepted' (proceed with the new step list), 'revision rejected' (keep the original plan and continue), or 'revision cancelled' (drop the proposal entirely). Don't call any tools in the meantime.",
    );
    this.name = "PlanRevisionProposedError";
    this.reason = reason;
    this.remainingSteps = remainingSteps;
    this.summary = summary;
  }

  toToolResult(): {
    error: string;
    reason: string;
    remainingSteps: PlanStep[];
    summary?: string;
  } {
    const payload: {
      error: string;
      reason: string;
      remainingSteps: PlanStep[];
      summary?: string;
    } = {
      error: `${this.name}: ${this.message}`,
      reason: this.reason,
      remainingSteps: this.remainingSteps,
    };
    if (this.summary) payload.summary = this.summary;
    return payload;
  }
}
