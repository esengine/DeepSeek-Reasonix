import { pauseGate } from "../core/pause-gate.js";
import type { ToolRegistry } from "../tools.js";
import { PlanProposedError, PlanRevisionProposedError } from "./plan-errors.js";
import type { PlanStep, PlanStepRisk, StepCompletion } from "./plan-types.js";

// Tool descriptions (teaching prompts for the model). Edit here, not inline.

const SUBMIT_PLAN_DESCRIPTION =
  "Submit ONE concrete plan you've already decided on. Use this for tasks that warrant a review gate — multi-file refactors, architecture changes, anything that would be expensive or confusing to undo. Skip it for small fixes (one-line typo, obvious bug with a clear fix) — just make the change. The user will either approve (you then implement it), ask for refinement, or cancel. If the user has already enabled /plan mode, writes are blocked at dispatch and you MUST use this. CRITICAL: do NOT use submit_plan to present alternative routes (A/B/C, option 1/2/3) for the user to pick from — the picker only exposes approve/refine/cancel, so a menu plan strands the user with no way to choose. For branching decisions, call `ask_choice` instead; only call submit_plan once the user has picked a direction and you have a single actionable plan. Write the plan as markdown with a one-line summary, a bulleted list of files to touch and what will change, and any risks or open questions. STRONGLY PREFERRED: pass `steps` — an array of {id, title, action, risk?} — so the UI renders a structured step list above the approval picker and tracks per-step progress. Use risk='high' for steps that touch prod data / break public APIs / are hard to undo; 'med' for non-trivial but reversible (multi-file edits, schema tweaks); 'low' for safe local work. After each step, call `mark_step_complete` so the user sees progress ticks.";

const MARK_STEP_COMPLETE_DESCRIPTION =
  "Mark one step of the approved plan as done. Call this after finishing each step, then immediately continue with the NEXT step — do not stop or wait for the user. The TUI updates the plan card's progress in place. After the FINAL step, write a brief reply summarizing what was done and end the turn. Pass the `stepId` from the plan's steps array, a short `result` (what you did), and optional `notes` for anything surprising (errors, scope changes, follow-ups). This tool doesn't change any files. Don't call it if the plan didn't include structured steps, and don't invent ids that weren't in the original plan.";

const REVISE_PLAN_DESCRIPTION =
  "Surgically replace the REMAINING steps of an in-flight plan. Call this when the user has given feedback at a checkpoint that warrants a structured plan change — skip a step, swap two steps, add a new step, change risk, etc. Pass: `reason` (one sentence why), `remainingSteps` (the new tail of the plan, replacing whatever steps haven't been done yet), and optional `summary` (updated one-line plan summary). Done steps are NEVER touched — keep them out of `remainingSteps`. The TUI shows a diff (removed in red, kept in gray, added in green) and the user accepts or rejects. Don't call this for trivial mid-step adjustments — just keep executing. Don't call submit_plan for revisions either — that resets the whole plan including completed steps. Use submit_plan only when the entire approach has changed; use revise_plan when the tail needs editing.";

// Reused by both submit_plan and revise_plan — the step list shape is
// identical, only the outer wrapper differs. Deliberately NOT `as const`:
// ToolRegistry's JSONSchema type expects mutable arrays.
const STEP_ITEM_SCHEMA = {
  type: "object",
  properties: {
    id: { type: "string", description: "Stable id, e.g. step-1." },
    title: { type: "string", description: "Short imperative title." },
    action: { type: "string", description: "One-sentence description of the concrete action." },
    risk: {
      type: "string",
      enum: ["low", "med", "high"],
      description:
        "Self-assessed risk. 'high' = hard-to-undo / touches prod / breaks API; 'med' = non-trivial but reversible; 'low' = safe local work. The UI shows a colored dot per step so the user knows where to focus review. Omit if you're unsure.",
    },
  },
  required: ["id", "title", "action"],
};

// Registration options

export interface PlanToolOptions {
  onPlanSubmitted?: (plan: string, steps?: PlanStep[]) => void;
  onStepCompleted?: (update: StepCompletion) => void;
  onPlanRevisionProposed?: (reason: string, remainingSteps: PlanStep[], summary?: string) => void;
}

// Arg sanitizers — defensive cleanup shared between submit_plan and revise_plan

function sanitizeRisk(raw: unknown): PlanStepRisk | undefined {
  if (raw === "low" || raw === "med" || raw === "high") return raw;
  return undefined;
}

function sanitizeSteps(raw: unknown): PlanStep[] | undefined {
  if (!Array.isArray(raw)) return undefined;
  const steps: PlanStep[] = [];
  for (const entry of raw) {
    if (!entry || typeof entry !== "object") continue;
    const e = entry as Record<string, unknown>;
    const id = typeof e.id === "string" ? e.id.trim() : "";
    const title = typeof e.title === "string" ? e.title.trim() : "";
    const action = typeof e.action === "string" ? e.action.trim() : "";
    if (!id || !title || !action) continue;
    const step: PlanStep = { id, title, action };
    const risk = sanitizeRisk(e.risk);
    if (risk) step.risk = risk;
    steps.push(step);
  }
  return steps.length > 0 ? steps : undefined;
}

// Individual tool registrations — one per screen

function registerSubmitPlan(registry: ToolRegistry, opts: PlanToolOptions): void {
  registry.register({
    name: "submit_plan",
    description: SUBMIT_PLAN_DESCRIPTION,
    readOnly: true,
    parameters: {
      type: "object",
      properties: {
        plan: {
          type: "string",
          description:
            "Markdown-formatted plan. Lead with a one-sentence summary. Then a file-by-file breakdown of what you'll change and why. Flag any risks or open questions at the end so the user can weigh in before you start.",
        },
        steps: {
          type: "array",
          description:
            "Structured step list (strongly recommended). When provided, the UI renders a compact step list above the approval picker AND tracks per-step progress via `mark_step_complete`. Use stable ids (step-1, step-2, ...). Skip only for tiny one-step plans where the markdown body is enough.",
          items: STEP_ITEM_SCHEMA,
        },
        summary: {
          type: "string",
          description:
            "Optional. One-sentence human-friendly title for the plan, ~80 chars max. Surfaces in the PlanConfirm picker header and in /plans listings ('▸ refactor auth into signed tokens · 2/5 done'). Skip for trivial plans where the first line of the markdown body is already short and clear.",
        },
      },
      required: ["plan"],
    },
    fn: async (args: { plan: string; steps?: unknown; summary?: string }, ctx) => {
      const plan = (args?.plan ?? "").trim();
      if (!plan) {
        throw new Error("submit_plan: empty plan — write a markdown plan and try again.");
      }
      const steps = sanitizeSteps(args?.steps);
      const summary =
        typeof args?.summary === "string" ? args.summary.trim() || undefined : undefined;
      opts.onPlanSubmitted?.(plan, steps);
      // Block until the user approves, refines, or cancels
      const verdict = await (ctx?.confirmationGate ?? pauseGate).ask({
        kind: "plan_proposed",
        payload: { plan, steps, summary },
      });
      if (verdict.type === "approve") return "plan approved";
      if (verdict.type === "refine") throw new Error("user requested refinement");
      throw new Error("plan cancelled");
    },
  });
}

function registerMarkStepComplete(registry: ToolRegistry, opts: PlanToolOptions): void {
  registry.register({
    name: "mark_step_complete",
    description: MARK_STEP_COMPLETE_DESCRIPTION,
    readOnly: true,
    parameters: {
      type: "object",
      properties: {
        stepId: {
          type: "string",
          description:
            "The id of the step being marked complete. Must match one from submit_plan's steps array.",
        },
        title: {
          type: "string",
          description:
            "Optional. The step's title, echoed back for the UI. If omitted, the UI falls back to the id.",
        },
        result: {
          type: "string",
          description: "One-sentence summary of what was done for this step.",
        },
        notes: {
          type: "string",
          description:
            "Optional. Anything surprising — blockers hit, assumptions revised, follow-ups for later steps.",
        },
      },
      required: ["stepId", "result"],
    },
    fn: async (args: { stepId: string; title?: string; result: string; notes?: string }, ctx) => {
      const stepId = (args?.stepId ?? "").trim();
      const result = (args?.result ?? "").trim();
      if (!stepId) {
        throw new Error("mark_step_complete: stepId is required.");
      }
      if (!result) {
        throw new Error(
          "mark_step_complete: result is required — say in one sentence what you did.",
        );
      }
      const title = typeof args?.title === "string" ? args.title.trim() || undefined : undefined;
      const notes = typeof args?.notes === "string" ? args.notes.trim() || undefined : undefined;
      const update: StepCompletion = { kind: "step_completed", stepId, result };
      if (title) update.title = title;
      if (notes) update.notes = notes;
      opts.onStepCompleted?.(update);
      // Block until the user continues, revises, or stops
      const verdict = await (ctx?.confirmationGate ?? pauseGate).ask({
        kind: "plan_checkpoint",
        payload: { stepId, title, result, notes },
      });
      if (verdict.type === "continue") return JSON.stringify(update);
      if (verdict.type === "revise") {
        if (verdict.feedback) return `revision requested: ${verdict.feedback}`;
        throw new Error("user requested revision at checkpoint");
      }
      throw new Error("user stopped at checkpoint");
    },
  });
}

function registerRevisePlan(registry: ToolRegistry, opts: PlanToolOptions): void {
  registry.register({
    name: "revise_plan",
    description: REVISE_PLAN_DESCRIPTION,
    readOnly: true,
    parameters: {
      type: "object",
      properties: {
        reason: {
          type: "string",
          description:
            "One sentence explaining why you're revising — what the user asked for, what changed your assessment.",
        },
        remainingSteps: {
          type: "array",
          description:
            "The new tail of the plan — what should run from here on. Each entry: {id, title, action, risk?}. Use stable ids; reuse old ids when a step is just being adjusted, generate new ones for genuinely new steps.",
          items: STEP_ITEM_SCHEMA,
        },
        summary: {
          type: "string",
          description:
            "Optional. Updated one-line plan summary if the overall framing has shifted.",
        },
      },
      required: ["reason", "remainingSteps"],
    },
    fn: async (args: { reason: string; remainingSteps: unknown; summary?: string }, ctx) => {
      const reason = (args?.reason ?? "").trim();
      if (!reason) {
        throw new Error(
          "revise_plan: reason is required — write one sentence explaining the change.",
        );
      }
      const remainingSteps = sanitizeSteps(args?.remainingSteps);
      if (!remainingSteps || remainingSteps.length === 0) {
        throw new Error(
          "revise_plan: remainingSteps must be a non-empty array of well-formed steps. If the user wants to STOP rather than continue, don't revise — the picker has its own Stop option.",
        );
      }
      const summary =
        typeof args?.summary === "string" ? args.summary.trim() || undefined : undefined;
      opts.onPlanRevisionProposed?.(reason, remainingSteps, summary);
      // Block until the user accepts, rejects, or cancels the revision
      const verdict = await (ctx?.confirmationGate ?? pauseGate).ask({
        kind: "plan_revision",
        payload: { reason, remainingSteps, summary },
      });
      if (verdict.type === "accepted") return "revision accepted";
      if (verdict.type === "rejected") throw new Error("revision rejected");
      throw new Error("revision cancelled");
    },
  });
}

// Public entry point

export function registerPlanTool(registry: ToolRegistry, opts: PlanToolOptions = {}): ToolRegistry {
  registerSubmitPlan(registry, opts);
  registerMarkStepComplete(registry, opts);
  registerRevisePlan(registry, opts);
  return registry;
}
