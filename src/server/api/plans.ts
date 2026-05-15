import { listAllPlanArchives } from "../../code/plan-store.js";
import type { PlanStep } from "../../tools/plan.js";
import type { DashboardContext } from "../context.js";
import type { ApiResult } from "../router.js";

interface PlanRow {
  session: string;
  path: string;
  completedAt: string;
  totalSteps: number;
  completedSteps: number;
  /** Computed completion ratio 0..1, surfaced so the SPA doesn't redo the math. */
  completionRatio: number;
  /** Plan summary (if the archive carried one). */
  summary?: string;
  /** Steps + completion ids — consumers render the step list inline. */
  steps: PlanStep[];
  completedStepIds: string[];
}

export async function handlePlans(
  method: string,
  _rest: string[],
  _body: string,
  _ctx: DashboardContext,
): Promise<ApiResult> {
  if (method !== "GET") {
    return { status: 405, body: { error: "GET only" } };
  }
  const out: PlanRow[] = listAllPlanArchives().map((a) => {
    const total = a.steps.length;
    const done = a.completedStepIds.length;
    const row: PlanRow = {
      session: a.sessionName,
      path: a.path,
      completedAt: a.completedAt,
      totalSteps: total,
      completedSteps: done,
      completionRatio: total > 0 ? done / total : 0,
      steps: a.steps,
      completedStepIds: a.completedStepIds,
    };
    if (a.summary) row.summary = a.summary;
    return row;
  });
  return { status: 200, body: { plans: out } };
}
