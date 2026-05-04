import type { Dispatch, MutableRefObject, SetStateAction } from "react";
import { archivePlanState } from "../../../code/plan-store.js";
import type { LoopEvent } from "../../../loop.js";
import type { ChoiceOption } from "../../../tools/choice.js";
import type { PlanStep, StepCompletion } from "../../../tools/plan.js";
import type { TurnTranslator } from "../state/TurnTranslator.js";
import type { Scrollback } from "./useScrollback.js";

export interface ToolEventContext {
  flush: () => void;
  translator: TurnTranslator;
  setOngoingTool: Dispatch<SetStateAction<{ name: string; args?: string } | null>>;
  setToolProgress: Dispatch<
    SetStateAction<{ progress: number; total?: number; message?: string } | null>
  >;
  toolStartedAtRef: MutableRefObject<number | null>;
  toolHistoryRef: MutableRefObject<Array<{ toolName: string; text: string }>>;
  setPendingShell: Dispatch<
    SetStateAction<{ command: string; kind: "run_command" | "run_background" } | null>
  >;
  setPendingPlan: Dispatch<SetStateAction<string | null>>;
  setPendingRevision: Dispatch<
    SetStateAction<{ reason: string; remainingSteps: PlanStep[]; summary?: string } | null>
  >;
  setPendingChoice: Dispatch<
    SetStateAction<{ question: string; options: ChoiceOption[]; allowCustom: boolean } | null>
  >;
  planStepsRef: MutableRefObject<PlanStep[] | null>;
  completedStepIdsRef: MutableRefObject<Set<string>>;
  planBodyRef: MutableRefObject<string | null>;
  planSummaryRef: MutableRefObject<string | null>;
  persistPlanState: () => void;
  log: Scrollback;
  session: string | null;
  codeModeOn: boolean;
}

export function handleToolEvent(ev: LoopEvent, ctx: ToolEventContext): void {
  ctx.flush();
  ctx.setOngoingTool(null);
  ctx.setToolProgress(null);
  ctx.translator.toolEnd(ev.content);

  // mark_step_complete gets its own pretty scrollback row below — suppress
  // the raw tool row here so we don't show the same JSON blob twice.
  const isStepProgressTool = ev.toolName === "mark_step_complete";
  ctx.toolStartedAtRef.current = null;
  if (!isStepProgressTool) {
    ctx.toolHistoryRef.current.push({
      toolName: ev.toolName ?? "?",
      text: ev.content,
    });
  }

  if (
    ctx.codeModeOn &&
    (ev.toolName === "run_command" || ev.toolName === "run_background") &&
    ev.content.includes('"NeedsConfirmationError:') &&
    ev.toolArgs
  ) {
    try {
      const parsed = JSON.parse(ev.toolArgs) as { command?: unknown };
      if (typeof parsed.command === "string" && parsed.command.trim()) {
        ctx.setPendingShell({
          command: parsed.command.trim(),
          kind: ev.toolName as "run_command" | "run_background",
        });
      }
    } catch {
      /* malformed args — skip the prompt */
    }
  }

  if (
    ctx.codeModeOn &&
    ev.toolName === "submit_plan" &&
    ev.content.includes('"PlanProposedError:')
  ) {
    try {
      const parsed = JSON.parse(ev.content) as {
        plan?: unknown;
        steps?: unknown;
        summary?: unknown;
      };
      if (typeof parsed.plan === "string" && parsed.plan.trim()) {
        const planText = parsed.plan.trim();
        ctx.setPendingPlan(planText);
        const steps = Array.isArray(parsed.steps) ? (parsed.steps as PlanStep[]) : null;
        ctx.planStepsRef.current = steps;
        ctx.completedStepIdsRef.current = new Set();
        ctx.planBodyRef.current = planText;
        ctx.planSummaryRef.current =
          typeof parsed.summary === "string" && parsed.summary.trim()
            ? parsed.summary.trim()
            : null;
        ctx.persistPlanState();
        const summary =
          typeof parsed.summary === "string" && parsed.summary.trim() ? parsed.summary.trim() : "";
        const title = summary ? `Plan · ${summary}` : "Plan submitted";
        ctx.log.showPlan({
          title,
          steps: (steps ?? []).map((s) => ({ id: s.id, title: s.title, status: "queued" })),
          variant: "active",
        });
      }
    } catch {
      /* malformed payload — skip the picker */
    }
  }

  if (ev.toolName === "revise_plan" && ev.content.includes('"PlanRevisionProposedError:')) {
    try {
      const parsed = JSON.parse(ev.content) as {
        reason?: unknown;
        remainingSteps?: unknown;
        summary?: unknown;
      };
      const reason = typeof parsed.reason === "string" ? parsed.reason.trim() : "";
      const remainingSteps = Array.isArray(parsed.remainingSteps)
        ? (parsed.remainingSteps as PlanStep[]).filter(
            (s) =>
              s &&
              typeof s.id === "string" &&
              s.id.trim() &&
              typeof s.title === "string" &&
              s.title.trim() &&
              typeof s.action === "string" &&
              s.action.trim(),
          )
        : [];
      if (reason && remainingSteps.length > 0) {
        const summary =
          typeof parsed.summary === "string" ? parsed.summary.trim() || undefined : undefined;
        ctx.setPendingRevision({ reason, remainingSteps, summary });
      }
    } catch {
      /* malformed payload — skip the picker */
    }
  }

  if (ev.toolName === "ask_choice" && ev.content.includes('"ChoiceRequestedError:')) {
    try {
      const parsed = JSON.parse(ev.content) as {
        question?: unknown;
        options?: unknown;
        allowCustom?: unknown;
      };
      const question = typeof parsed.question === "string" ? parsed.question.trim() : "";
      const options = Array.isArray(parsed.options)
        ? (parsed.options as ChoiceOption[]).filter(
            (o) =>
              o &&
              typeof o.id === "string" &&
              o.id.trim() &&
              typeof o.title === "string" &&
              o.title.trim(),
          )
        : [];
      if (question && options.length >= 2) {
        ctx.setPendingChoice({
          question,
          options,
          allowCustom: parsed.allowCustom === true,
        });
      }
    } catch {
      /* malformed payload — skip the picker */
    }
  }

  if (ev.toolName === "mark_step_complete") {
    try {
      const parsed = JSON.parse(ev.content) as Partial<StepCompletion>;
      const stepId = parsed.stepId;
      if (parsed.kind === "step_completed" && typeof stepId === "string") {
        ctx.completedStepIdsRef.current.add(stepId);
        ctx.persistPlanState();
        ctx.log.completePlanStep(stepId);
        const total = ctx.planStepsRef.current?.length ?? 0;
        const completed = ctx.completedStepIdsRef.current.size;
        const stepFromPlan = ctx.planStepsRef.current?.find((s) => s.id === stepId);
        const title = parsed.title ?? stepFromPlan?.title;
        if (title) ctx.log.pushStepProgress(completed, total, title);
        if (ctx.session && total > 0 && completed >= total) {
          const archive = archivePlanState(ctx.session);
          if (archive) {
            ctx.log.pushInfo(
              `▸ plan complete — all ${total} step${total === 1 ? "" : "s"} done · archived`,
            );
          }
        }
      }
    } catch {
      /* malformed payload — skip the progress row */
    }
  }
}
