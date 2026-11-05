import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onPlanProgressChange, setShowsPlanProgress, showsPlanProgress } from "../state/prefs";
import { Group } from "./Group";
import { Switch } from "./Switch";

export function PlanProgressSection() {
  const on = useSyncExternalStore(onPlanProgressChange, showsPlanProgress, showsPlanProgress);
  return (
    <Group id="plan-progress" inline title={t("plan.show_collapsed_plan_progress")} hint={t("settings.plan_progress_beside_title")}>
      <Switch data-action="appearance.plan-progress" on={on} label={t("plan.show_collapsed_plan_progress")} onClick={() => setShowsPlanProgress(!on)} />
    </Group>
  );
}
