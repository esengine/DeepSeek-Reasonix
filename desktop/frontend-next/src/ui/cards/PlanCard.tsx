import { useEffect, useMemo, useRef, useState } from "react";
import { t } from "../../i18n";
import { currentStep, stepDone, stepLabel, type Item } from "../../state/session";
import type { PlanAction } from "../../port/session";
import { proposedSteps, usePlanLive } from "../planLive";
import { PlanSteps, stepOrdinal, type StepsState } from "../PlanSteps";
import { RMark } from "../RMark";

type Phase = "pending" | "settled" | "running" | "waiting" | "stopped" | "done";


const STEPS: Record<Phase, StepsState> = {
  pending: "proposed",
  settled: "proposed",
  running: "moving",
  waiting: "waiting",
  stopped: "still",
  done: "still",
};

function badgeOf(phase: Phase, n: number, total: number, settled: string): string {
  switch (phase) {
    case "pending": return t("待确认");
    case "running": return t("第 {n} / {total} 步", { n, total });
    case "waiting": return t("等你定");
    case "stopped": return t("已停下");
    case "done": return t("做完了");
    case "settled": return settled;
  }
}

function settledWords(verdict?: string): [string, string] {
  if (verdict === "exit") return [t("暂不执行"), t("已退出计划模式，可随时重新下达。")];
  if (verdict === "revise" || verdict === "deny") return [t("继续规划"), t("在下方说明需要修改的内容，规划者将据此重写该计划。")];
  return [t("已在其他窗口处理"), t("请以最新运行状态为准。")];
}

// One plan, one card: proposed, running, waiting on a person and finished are
// this card changing in place. The steps are the list approving seeds until it
// starts, then the kernel's live list, then the last one it reported.
export function PlanCard({ item, onPlan }: { item: Extract<Item, { t: "approval" }>; onPlan: (itemId: string, id: string, action: PlanAction) => Promise<void> }) {
  const live = usePlanLive();
  const sealed = item.verdict !== undefined;
  const started = item.verdict === "start";
  const latest = !!live && live.latest === item.id;
  const following = latest && started && live.plan.length > 0;
  const proposed = useMemo(() => proposedSteps(item.a), [item.a]);
  const steps = !started ? proposed : following ? live.plan : item.steps ?? proposed;
  const finished = steps.length > 0 && steps.every(stepDone);
  const phase: Phase = !sealed ? "pending" : !started ? "settled" : finished ? "done"
    : latest && live.blocked ? "waiting" : latest && live.running ? "running" : "stopped";

  const [submitting, setSubmitting] = useState<PlanAction | "">("");
  const decide = async (action: PlanAction) => {
    if (submitting) return;
    setSubmitting(action);
    try {
      await onPlan(item.id, item.a.id, action);
    } finally {
      setSubmitting("");
    }
  };

  const card = useRef<HTMLElement>(null);
  const onSeen = live?.onSeen;
  const current = latest && started;
  useEffect(() => {
    const node = card.current;
    if (!current || !node || !onSeen || typeof IntersectionObserver === "undefined") return;
    const watch = new IntersectionObserver(([entry]) => onSeen(!!entry?.isIntersecting), { root: node.closest(".scroll") });
    watch.observe(node);
    return () => {
      watch.disconnect();
      onSeen(false);
    };
  }, [current, onSeen]);

  const n = stepOrdinal(steps);
  const at = steps[currentStep(steps)] ?? steps[n - 1];
  const settled = settledWords(item.verdict);
  const badge = badgeOf(phase, n, steps.length, settled[0]);
  const sub = phase === "running" && at ? <><RMark className="plan-spin" />{t("正在做：{step}", { step: stepLabel(at) })}</>
    : (phase === "waiting" || phase === "stopped") && at ? t("停在第 {n} 步：{step}", { n, step: stepLabel(at) })
      : phase === "done" ? t("{n} 步全部完成", { n: steps.length })
        : phase === "settled" ? settled[1] : null;

  return (
    <div className="call" data-k="plan" data-prompt={sealed ? "settled" : "pending"}>
      <section className="plan-card" data-phase={phase} ref={card} aria-label={t("计划")} aria-busy={!!submitting}>
        <header className="plan-card-head">
          <div className="plan-card-title">
            <b>{t("计划")}</b>
            <span className="plan-card-badge">{badge}</span>
          </div>
          {sub && <div className="plan-card-sub">{sub}</div>}
        </header>
        <PlanSteps steps={steps} state={STEPS[phase]} />
        {!sealed && (
          <footer className="plan-card-foot">
            <button type="button" className="plan-card-btn" data-primary data-action="decision.plan" data-target={item.a.id} data-value="start"
              disabled={!!submitting} onClick={() => void decide("start")}>
              {submitting === "start" ? t("正在提交…") : t("开始执行")}
            </button>
            <button type="button" className="plan-card-btn" data-action="decision.plan" data-target={item.a.id} data-value="revise"
              disabled={!!submitting} onClick={() => void decide("revise")}>
              {t("修改计划")}
            </button>
            <button type="button" className="plan-card-btn" data-quiet data-action="decision.plan" data-target={item.a.id} data-value="exit"
              disabled={!!submitting} onClick={() => void decide("exit")}>
              {t("暂不执行")}
            </button>
            {/* Revising is not discarding: it keeps planning, which is how a plan gets changed. */}
            <span className="plan-card-hint">{t("「修改计划」将保持在计划模式中，请在下方说明需要修改之处。")}</span>
          </footer>
        )}
      </section>
    </div>
  );
}
