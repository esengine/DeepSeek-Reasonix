import type { Dispatch, MutableRefObject, SetStateAction } from "react";
import { archivePlanState } from "../../../code/plan-store.js";
import { t } from "../../../i18n/index.js";
import type { LoopEvent } from "../../../loop.js";
import type { ChoiceOption } from "../../../tools/choice.js";
import type { PlanStep, StepCompletion, StepEvidence } from "../../../tools/plan.js";
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
  stepCompletionsRef?: MutableRefObject<Map<string, StepCompletion>>;
  pendingStepCompletionsRef?: MutableRefObject<Map<string, StepCompletion>>;
  planBodyRef: MutableRefObject<string | null>;
  planSummaryRef: MutableRefObject<string | null>;
  persistPlanState: () => void;
  onPlanStepCompleted?: (stepId: string) => void;
  log: Scrollback;
  session: string | null;
  codeModeOn: boolean;
}

export function handleToolEvent(ev: LoopEvent, ctx: ToolEventContext): void {
  ctx.flush();
  ctx.setOngoingTool(null);
  ctx.setToolProgress(null);
  ctx.translator.toolEnd(ev.content);

  ctx.toolStartedAtRef.current = null;

  if (ev.toolName === "mark_step_complete") {
    try {
      const parsed = JSON.parse(ev.content) as Partial<StepCompletion>;
      const stepId = parsed.stepId;
      if (parsed.kind === "step_completed" && typeof stepId === "string") {
        const fullCompletion = ctx.pendingStepCompletionsRef?.current.get(stepId);
        ctx.pendingStepCompletionsRef?.current.delete(stepId);
        const completion = fullCompletion ?? (parsed as StepCompletion);
        ctx.completedStepIdsRef.current.add(stepId);
        ctx.stepCompletionsRef?.current.set(stepId, completion);
        ctx.persistPlanState();
        ctx.log.completePlanStep(stepId);
        ctx.onPlanStepCompleted?.(stepId);
        const total = ctx.planStepsRef.current?.length ?? 0;
        const completed = ctx.completedStepIdsRef.current.size;
        const stepFromPlan = ctx.planStepsRef.current?.find((s) => s.id === stepId);
        const title = completion.title ?? parsed.title ?? stepFromPlan?.title;
        if (title) ctx.log.pushStepProgress(completed, total, title);
        const compactEvidenceSummary =
          typeof parsed.evidenceSummary === "string" && parsed.evidenceSummary.trim()
            ? `evidence: ${parsed.evidenceSummary.trim()}`
            : null;
        const evidenceSummary =
          formatStepEvidenceSummary(completion.evidence) ?? compactEvidenceSummary;
        if (evidenceSummary) ctx.log.pushInfo(evidenceSummary, "ghost");
        if (ctx.session && total > 0 && completed >= total) {
          const archive = archivePlanState(ctx.session);
          if (archive) {
            ctx.log.pushInfo(t("planFlow.completeMsg", { total, s: total === 1 ? "" : "s" }));
          }
        }
      }
    } catch {
      /* malformed payload — skip the progress row */
    }
  }
}

function formatStepEvidenceSummary(evidence: StepCompletion["evidence"]): string | null {
  if (!evidence || evidence.length === 0) return null;
  const parts = evidence.map(formatStepEvidenceItem).filter((part) => part.length > 0);
  if (parts.length === 0) return null;
  return `evidence: ${parts.join("; ")}`;
}

function formatStepEvidenceItem(evidence: StepEvidence): string {
  const extras: string[] = [];
  if (evidence.command) extras.push(evidence.command);
  if (evidence.paths && evidence.paths.length > 0)
    extras.push(evidence.paths.slice(0, 3).join(", "));
  const suffix = extras.length > 0 ? ` (${extras.join("; ")})` : "";
  return `${evidence.kind} - ${evidence.summary}${suffix}`;
}
