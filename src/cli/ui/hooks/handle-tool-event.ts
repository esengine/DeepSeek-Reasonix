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
    SetStateAction<{ id: number; command: string; kind: "run_command" | "run_background" } | null>
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
