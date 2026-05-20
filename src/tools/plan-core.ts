import { pauseGate } from "../core/pause-gate.js";
import type { ToolRegistry } from "../tools.js";
import { PlanProposedError, PlanRevisionProposedError } from "./plan-errors.js";
import type { PlanStep, PlanStepRisk, StepCompletion, StepEvidence } from "./plan-types.js";

const SUBMIT_PLAN_DESCRIPTION =
  "Submit ONE concrete plan for review. The user approves / refines / cancels — write a markdown plan body and (strongly preferred) a structured `steps` array. Use for multi-file refactors, architecture changes, anything expensive to undo. Skip for small fixes. Do NOT use for A/B/C menus — the picker has no branch selector, so a menu plan strands the user; call `ask_choice` for branching decisions. See the system prompt for fuller guidance.";

const MARK_STEP_COMPLETE_DESCRIPTION =
  "Mark one approved-plan step as done. Call exactly once after finishing each step, before starting the next. After the FINAL step, write a brief reply summarizing what was done and end the turn. Skip if the plan didn't include structured steps.";

const REVISE_PLAN_DESCRIPTION =
  "Replace the REMAINING steps of an in-flight plan when checkpoint feedback warrants a structural change. Pass `reason`, the new `remainingSteps` tail (done steps are untouched — keep them out), and optional updated `summary`. Don't call submit_plan for revisions — it resets the whole plan.";

// Shared between submit_plan + revise_plan; not `as const` because JSONSchema expects mutable arrays.
const STEP_ITEM_SCHEMA = {
  type: "object",
  properties: {
    id: { type: "string", description: "Stable id, e.g. step-1." },
    title: { type: "string", description: "Short imperative title." },
    action: { type: "string", description: "One-sentence concrete action." },
    risk: {
      type: "string",
      enum: ["low", "med", "high"],
      description:
        "high = hard-to-undo / prod / API break; med = reversible multi-file; low = safe local. Omit if unsure.",
    },
    targets: {
      type: "array",
      description: "Optional. Files/dirs/modules this step touches.",
      items: { type: "string" },
    },
    acceptance: {
      type: "string",
      description: "Optional. One-sentence completion criterion.",
    },
    verification: {
      type: "array",
      description: "Optional. Verification commands/checks for this step.",
      items: { type: "string" },
    },
  },
  required: ["id", "title", "action"],
};

// Registration options

export interface PlanToolOptions {
  onPlanSubmitted?: (plan: string, steps?: PlanStep[]) => void;
  onStepCompleted?: (update: StepCompletion) => void;
  onPlanRevisionProposed?: (reason: string, remainingSteps: PlanStep[], summary?: string) => void;
  requireStepEvidence?: (args: { stepId: string; title?: string }) => string | null | undefined;
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
    const targets = sanitizeStringList(e.targets);
    if (targets) step.targets = targets;
    const acceptance = typeof e.acceptance === "string" ? e.acceptance.trim() : "";
    if (acceptance) step.acceptance = acceptance;
    const verification = sanitizeStringList(e.verification);
    if (verification) step.verification = verification;
    steps.push(step);
  }
  return steps.length > 0 ? steps : undefined;
}

function sanitizeStringList(raw: unknown): string[] | undefined {
  if (!Array.isArray(raw)) return undefined;
  const out = raw
    .map((entry) => (typeof entry === "string" ? entry.trim() : ""))
    .filter((entry) => entry.length > 0);
  return out.length > 0 ? out : undefined;
}

function sanitizeEvidence(raw: unknown): StepEvidence[] | undefined {
  if (!Array.isArray(raw)) return undefined;
  const out: StepEvidence[] = [];
  for (const item of raw) {
    if (!item || typeof item !== "object") continue;
    const e = item as Record<string, unknown>;
    const kind = e.kind;
    if (kind !== "verification" && kind !== "diff" && kind !== "checkpoint" && kind !== "manual") {
      continue;
    }
    const summary = typeof e.summary === "string" ? e.summary.trim() : "";
    if (!summary) continue;
    const evidence: StepEvidence = { kind, summary };
    const command = typeof e.command === "string" ? e.command.trim() : "";
    if (command) evidence.command = command;
    const paths = sanitizeStringList(e.paths);
    if (paths) evidence.paths = paths;
    out.push(evidence);
  }
  return out.length > 0 ? out : undefined;
}

function summarizeEvidence(evidence: StepEvidence[] | undefined): string | undefined {
  if (!evidence || evidence.length === 0) return undefined;
  const parts = evidence.map((item) => `${item.kind}: ${item.summary}`);
  return parts.join("; ");
}

function compactStepCompletion(update: StepCompletion): StepCompletion {
  const compact: StepCompletion = {
    kind: "step_completed",
    stepId: update.stepId,
    result: update.result,
  };
  const evidenceSummary = summarizeEvidence(update.evidence);
  if (evidenceSummary) compact.evidenceSummary = evidenceSummary;
  return compact;
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
            "Markdown plan: one-line summary, file-by-file breakdown, risks/open questions.",
        },
        steps: {
          type: "array",
          description:
            "Structured step list — strongly recommended for >1 step. Stable ids (step-1, step-2, ...).",
          items: STEP_ITEM_SCHEMA,
        },
        summary: {
          type: "string",
          description: "Optional ~80-char plan title for the picker header and /plans listings.",
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
      const fb = verdict.feedback?.trim();
      if (verdict.type === "approve") {
        return fb ? `plan approved. user's additional instructions: ${fb}` : "plan approved";
      }
      if (verdict.type === "refine") {
        throw new Error(fb ? `user requested refinement: ${fb}` : "user requested refinement");
      }
      throw new Error(fb ? `plan cancelled: ${fb}` : "plan cancelled");
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
          description: "Step id from submit_plan's steps array.",
        },
        title: {
          type: "string",
          description: "Optional. Echoed for the UI; falls back to id.",
        },
        result: {
          type: "string",
          description: "One-sentence summary of what was done.",
        },
        notes: {
          type: "string",
          description: "Optional. Surprises — blockers, revised assumptions, follow-ups.",
        },
        evidence: {
          type: "array",
          description: "Optional. Verification summary / diff / checkpoint ref / manual note.",
          items: {
            type: "object",
            properties: {
              kind: { type: "string", enum: ["verification", "diff", "checkpoint", "manual"] },
              summary: { type: "string" },
              command: { type: "string" },
              paths: { type: "array", items: { type: "string" } },
            },
            required: ["kind", "summary"],
          },
        },
      },
      required: ["stepId", "result"],
    },
    fn: async (
      args: {
        stepId: string;
        title?: string;
        result: string;
        notes?: string;
        evidence?: unknown;
      },
      ctx,
    ) => {
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
      const evidence = sanitizeEvidence(args?.evidence);
      const evidenceReason = opts.requireStepEvidence?.({ stepId, title });
      if (evidenceReason && (!evidence || evidence.length === 0)) {
        throw new Error(`mark_step_complete: evidence required — ${evidenceReason}`);
      }
      const update: StepCompletion = { kind: "step_completed", stepId, result };
      if (title) update.title = title;
      if (notes) update.notes = notes;
      if (evidence) update.evidence = evidence;
      opts.onStepCompleted?.(update);
      // Block until the user continues, revises, or stops
      const verdict = await (ctx?.confirmationGate ?? pauseGate).ask({
        kind: "plan_checkpoint",
        payload: { stepId, title, result, notes, completion: update },
      });
      if (verdict.type === "continue") return JSON.stringify(compactStepCompletion(update));
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
          description: "One sentence — why you're revising / what the user asked for.",
        },
        remainingSteps: {
          type: "array",
          description: "New tail of the plan. Reuse old ids when adjusting; new ids for new steps.",
          items: STEP_ITEM_SCHEMA,
        },
        summary: {
          type: "string",
          description: "Optional. Updated one-line summary when framing has shifted.",
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
