import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { t } from "../i18n";
import { currentStep, stepDone, stepLabel, type PlanStep } from "../state/session";
import { onPlanProgressChange, showsPlanProgress } from "../state/prefs";
import { PlanSteps, stepOrdinal } from "./PlanSteps";
import type { GoalRuntime } from "../port/session";
import { goalStatusText, goalStopText, goalUsageText } from "./goalusage";
import { StudioIcon } from "./StudioIcon";
import { RMark } from "./RMark";
import { useDismiss } from "./dismiss";

interface Props {
  plan: PlanStep[];
  goal?: string;
  goalStatus?: string;
  goalRuntime?: GoalRuntime;
  shown: boolean;
  running?: boolean;
  paused?: boolean;
  /** The plan's own card is on screen, so this strip would say it twice. */
  cardInView?: boolean;
  /** Brings the plan's card back; absent when the transcript holds none. */
  onJump?: () => void;
}

/** The plan's stand-in above the input box while its card is out of view. A
 *  question waiting on a person is read at the transcript's end, so the strip
 *  gives way to it. Mounted whether or not it is shown, so the fold survives a
 *  visit to another view. */
export function PlanFold({ plan, goal, goalStatus, goalRuntime, shown, running, paused, cardInView, onJump }: Props) {
  const [open, setOpen] = useState(false);
  const progress = useSyncExternalStore(onPlanProgressChange, showsPlanProgress, showsPlanProgress);
  const bar = useRef<HTMLButtonElement>(null);
  const root = useRef<HTMLDivElement>(null);
  const refocus = useRef(false);
  const put = () => { refocus.current = true; setOpen(false); };
  useDismiss(open, root, put);
  const yields = plan.length > 0 && (!!cardInView || !!paused);
  const hidden = (plan.length === 0 && !goal) || !shown || yields;
  useEffect(() => {
    if (hidden) setOpen(false);
  }, [hidden]);
  // The bar is not drawn while the list is open, so focus returns once it is.
  useEffect(() => {
    if (open || !refocus.current) return;
    refocus.current = false;
    bar.current?.focus();
  }, [open]);
  if (hidden) return null;
  const done = plan.filter(stepDone).length;
  const now = currentStep(plan);
  const title = plan.length ? t("计划 · 第 {n} / {total} 步", { n: stepOrdinal(plan), total: plan.length }) : t("目标");
  const moving = !!running && !paused;
  return (
    <div className="studio-todo" data-open={open ? "" : undefined} ref={root}>
      <button
        type="button"
        ref={bar}
        className="studio-todo-sum"
        data-action="plan.fold"
        aria-expanded={open}
        onClick={() => setOpen((on) => !on)}
      >
        {moving && plan.length > 0 ? <RMark className="plan-spin" /> : <StudioIcon name="list" />}
        <b>{title}</b>
        <span>{plan.length ? stepLabel(plan[now] ?? plan[0]) : goal}</span>
        {progress && plan.length > 0 && (
          <span className="studio-todo-steps" aria-hidden="true">
            {plan.map((step, i) => <i key={i} data-state={stepDone(step) ? "done" : i === now ? "current" : "todo"} />)}
          </span>
        )}
        <StudioIcon name="down" className="studio-todo-fold" />
      </button>
      {!open && progress && plan.length > 0 && (
        <div className="prog2 studio-todo-progress sr-only" role="progressbar" aria-label={t("计划进度")}
          aria-valuemin={0} aria-valuemax={plan.length} aria-valuenow={done}>
          <span className="track"><i style={{ width: `${Math.round((done / plan.length) * 100)}%` }} /></span>
        </div>
      )}
      {open && (
        <div className="studio-todo-body">
          <div className="studio-todo-head">
            {moving && plan.length > 0 && <RMark className="plan-spin" />}
            <b>{title}</b>
            {onJump && plan.length > 0 && (
              <button type="button" className="studio-todo-jump" data-action="plan.jump"
                onClick={() => { setOpen(false); onJump(); }}>
                {t("跳到卡片 ↑")}
              </button>
            )}
            <button type="button" className="studio-todo-close" data-action="plan.collapse" aria-label={t("收起计划")} title={t("收起计划")}
              onClick={put}>
              <StudioIcon name="down" />
            </button>
          </div>
          {goal && <p className="studio-todo-goal">{goal}</p>}
          {goal && goalRuntime && (
            <div className="studio-todo-usage" role="group" aria-label={t("目标用量")}>
              <p>{goalStatus && <b>{goalStatusText(goalStatus)}</b>}{goalStatus && " · "}{goalUsageText(goalRuntime)}</p>
              {goalRuntime.stopCause && goalStatus !== "running" && <p>{t("停下的原因：{cause}", { cause: goalStopText(goalRuntime.stopCause) })}</p>}
              {goalRuntime.lastReason && <p>{t("最近一次判断：{reason}", { reason: goalRuntime.lastReason })}</p>}
            </div>
          )}
          <PlanSteps steps={plan} state={moving ? "moving" : "still"} />
        </div>
      )}
    </div>
  );
}
