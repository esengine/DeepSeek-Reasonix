import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onPlanProgressChange, setShowsPlanProgress, showsPlanProgress } from "../state/prefs";
import { Group } from "./Group";

export function PlanProgressSection() {
  const on = useSyncExternalStore(onPlanProgressChange, showsPlanProgress, showsPlanProgress);
  return (
    <Group id="plan-progress" title={t("plan.show_collapsed_plan_progress")} hint={t("plan.show_each_steps_progress_as")}>
      <div className="seg" data-text role="group" aria-label={t("plan.show_collapsed_plan_progress")}>
        <button data-action="appearance.plan-progress" data-value="off" aria-pressed={!on} onClick={() => setShowsPlanProgress(false)}>{t("common.close")}</button>
        <button data-action="appearance.plan-progress" data-value="on" aria-pressed={on} onClick={() => setShowsPlanProgress(true)}>{t("common.show")}</button>
      </div>
    </Group>
  );
}
