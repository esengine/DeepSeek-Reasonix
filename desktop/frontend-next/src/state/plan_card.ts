import type { Approval } from "../port/wire";
import type { Item, PlanStep } from "./session_types";

// The Tool name the kernel puts on a plan gate; `kind` says the same thing.
const PLAN_TOOL = "exit_plan_mode";

export const isPlanGate = (a: Approval) => a.kind === "plan" || a.tool === PLAN_TOOL;

/** The newest plan card, once started, is the plan the kernel's list belongs
 *  to. It keeps the last non-empty list reported, which outlives the pane's. */
export function stampPlan(items: Item[], plan: PlanStep[] | null): Item[] {
  if (!plan || plan.length === 0) return items;
  for (let i = items.length - 1; i >= 0; i--) {
    const it = items[i];
    if (it.t !== "approval" || !isPlanGate(it.a)) continue;
    if (it.verdict !== "start") return items;
    const next = items.slice();
    next[i] = { ...it, steps: plan };
    return next;
  }
  return items;
}
