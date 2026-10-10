import { useState } from "react";
import { t } from "../i18n";
import { currentStep, stepDone, stepLabel, type PlanStep } from "../state/session";
import { Plan } from "./Plan";
import { StudioIcon } from "./StudioIcon";

/** The list belongs above the line that narrates the turn, because that is
 *  what it is about. Mounted whether or not it is shown, so the fold survives a
 *  visit to another view. */
export function PlanFold({ plan, shown, paused }: { plan: PlanStep[]; shown: boolean; paused?: boolean }) {
  const [open, setOpen] = useState(false);
  if (plan.length === 0 || !shown) return null;
  return (
    <div className="studio-todo" data-open={open ? "" : undefined}>
      {/* One line by default. The composer already carries the run line,
          the outbox and its own toolbar; a plan opened over all of them
          pushes the box a person types in off the bottom of a laptop. */}
      <button
        type="button"
        className="studio-todo-sum"
        data-action="plan.fold"
        aria-expanded={open}
        onClick={() => setOpen((on) => !on)}
      >
        <StudioIcon name="list" />
        <b>{t("计划")}</b>
        <span>{stepLabel(plan[currentStep(plan)] ?? plan[0])}</span>
        <small>{plan.filter(stepDone).length}/{plan.length}</small>
        <StudioIcon name="down" className="studio-todo-fold" />
      </button>
      {open && <div className="studio-todo-body"><Plan steps={plan} paused={paused} /></div>}
    </div>
  );
}
