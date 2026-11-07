import { useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from "react";
import { t } from "../i18n";
import { currentStep, stepDone, stepLabel, type PlanStep } from "../state/session";
import { onPlanProgressChange, showsPlanProgress } from "../state/prefs";
import { PlanSteps, stepOrdinal } from "./PlanSteps";
import type { GoalRuntime } from "../port/session";
import { goalStatusText, goalStopText, goalTone, goalUsageLine, goalUsageText, workTime } from "./goalusage";
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
  /** Pauses, resumes or ends the goal; absent where the window cannot send commands. */
  onGoal?: (command: "pause" | "resume" | "clear") => void;
}

/** The plan's stand-in above the input box while its card is out of view. A
 *  question waiting on a person is read at the transcript's end, so the strip
 *  gives way to it. Mounted whether or not it is shown, so the fold survives a
 *  visit to another view. */
export function PlanFold({ plan, goal, goalStatus, goalRuntime, shown, running, paused, cardInView, onJump, onGoal }: Props) {
  const [open, setOpen] = useState(false);
  const progress = useSyncExternalStore(onPlanProgressChange, showsPlanProgress, showsPlanProgress);
  const bar = useRef<HTMLButtonElement>(null);
  const root = useRef<HTMLDivElement>(null);
  const refocus = useRef(false);
  const list = useRef<HTMLDivElement>(null);
  const [asked, setAsked] = useState(false);
  const [leaving, setLeaving] = useState(false);
  const put = () => { refocus.current = true; setOpen(false); };
  useDismiss(open, root, put);
  const yields = plan.length > 0 && (!!cardInView || !!paused);
  const hidden = (plan.length === 0 && !goal) || !shown || yields;
  useEffect(() => {
    if (hidden) setOpen(false);
  }, [hidden]);
  // Closing is a state change the list answers before it goes: it stays, marked
  // as leaving, for as long as the stylesheet gives it an exit to play. Where
  // none plays (reduced motion, no animation support) it goes at once.
  if (asked !== open) {
    setAsked(open);
    setLeaving(!open);
  }
  const mounted = open || leaving;
  useLayoutEffect(() => {
    if (!leaving) return;
    const playing = list.current?.getAnimations?.() ?? [];
    if (playing.length === 0) { setLeaving(false); return; }
    let live = true;
    void Promise.allSettled(playing.map((a) => a.finished)).then(() => { if (live) setLeaving(false); });
    return () => { live = false; };
  }, [leaving]);
  // The open list floats above the composer, so the transcript has to be told
  // how much of its bottom edge it covers.
  useLayoutEffect(() => {
    const strip = root.current;
    const composer = strip?.closest<HTMLElement>(".compose");
    if (!mounted || hidden || !strip || !composer) return;
    const fit = () => composer.style.setProperty("--todo-h", `${Math.ceil(strip.getBoundingClientRect().height)}px`);
    fit();
    const watch = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(fit);
    watch?.observe(strip);
    return () => {
      watch?.disconnect();
      composer.style.removeProperty("--todo-h");
    };
  }, [mounted, hidden]);
  // The bar is not drawn while the list is open, so focus returns once it is.
  useEffect(() => {
    if (mounted || !refocus.current) return;
    refocus.current = false;
    bar.current?.focus();
  }, [mounted]);
  if (hidden) return null;
  const done = plan.filter(stepDone).length;
  const now = currentStep(plan);
  const title = plan.length ? t("plan.strip.title", { n: stepOrdinal(plan), total: plan.length }) : t("plan.strip.goal");
  const moving = !!running && !paused;
  const goalOnly = plan.length === 0 && !!goal;
  const status = goalOnly && goalStatus ? (
    <span className="goal-st" data-tone={goalTone(goalStatus)}><i aria-hidden="true" />{goalStatusText(goalStatus)}{goalRuntime && goalRuntime.workDurationMs > 0 ? ` · ${workTime(goalRuntime.workDurationMs)}` : ""}</span>
  ) : null;
  return (
    <div className="studio-todo" data-open={mounted ? "" : undefined} data-goal={goalOnly ? "" : undefined} ref={root}>
      <button
        type="button"
        ref={bar}
        className="studio-todo-sum"
        data-action="plan.fold"
        aria-expanded={open}
        onClick={() => setOpen((on) => !on)}
      >
        {moving && plan.length > 0 ? <RMark className="plan-spin" /> : <StudioIcon name={goalOnly ? "target" : "list"} />}
        <b>{title}</b>
        <span>{plan.length ? stepLabel(plan[now] ?? plan[0]) : goal}</span>
        {status}
        {progress && plan.length > 0 && (
          <span className="studio-todo-steps" aria-hidden="true">
            {plan.map((step, i) => <i key={i} data-state={stepDone(step) ? "done" : i === now ? "current" : "todo"} />)}
          </span>
        )}
        <StudioIcon name="down" className="studio-todo-fold" />
      </button>
      {!mounted && progress && plan.length > 0 && (
        <div className="prog2 studio-todo-progress sr-only" role="progressbar" aria-label={t("plan.strip.progress")}
          aria-valuemin={0} aria-valuemax={plan.length} aria-valuenow={done}>
          <span className="track"><i style={{ width: `${Math.round((done / plan.length) * 100)}%` }} /></span>
        </div>
      )}
      {mounted && (
        <div className="studio-todo-body" ref={list} data-closing={leaving ? "" : undefined}>
          <div className="studio-todo-head">
            {moving && plan.length > 0 ? <RMark className="plan-spin" /> : goalOnly && <StudioIcon name="target" />}
            <b>{title}</b>
            {status}
            {onJump && plan.length > 0 && (
              <button type="button" className="studio-todo-jump" data-action="plan.jump"
                onClick={() => { setOpen(false); onJump(); }}>
                {t("plan.strip.jump")}
              </button>
            )}
            <button type="button" className="studio-todo-close" data-action="plan.collapse" aria-label={t("plan.strip.collapse")} title={t("plan.strip.collapse")}
              onClick={put}>
              <StudioIcon name="down" />
            </button>
          </div>
          {goal && <p className="studio-todo-goal">{goal}</p>}
          {goal && goalRuntime && (
            <div className="studio-todo-usage" role="group" aria-label={t("plan.goal.usage")}>
              <p>
                {goalRuntime.stopCause && goalStatus !== "running" && <span className="goal-why">{goalStopText(goalRuntime.stopCause)}</span>}
                {goalRuntime.stopCause && goalStatus !== "running" && " · "}{goalOnly ? goalUsageLine(goalRuntime) : <>{goalStatus && <b>{goalStatusText(goalStatus)}</b>}{goalStatus && " · "}{goalUsageText(goalRuntime)}</>}
              </p>
              {goalRuntime.lastReason && <p>{t("plan.goal.last_reason", { reason: goalRuntime.lastReason })}</p>}
            </div>
          )}
          {goalOnly && onGoal && goalStatus && goalStatus !== "complete" && (
            <div className="studio-todo-acts">
              {goalStatus === "running"
                ? <button type="button" data-action="goal.pause" onClick={() => onGoal("pause")}>{t("plan.goal.pause")}</button>
                : <button type="button" data-action="goal.resume" onClick={() => onGoal("resume")}>{t("plan.goal.resume")}</button>}
              <button type="button" data-action="goal.clear" onClick={() => onGoal("clear")}>{t("plan.goal.end")}</button>
            </div>
          )}
          <PlanSteps steps={plan} state={moving ? "moving" : "still"} />
        </div>
      )}
    </div>
  );
}
