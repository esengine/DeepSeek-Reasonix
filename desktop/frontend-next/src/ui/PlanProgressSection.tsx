import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onPlanProgressChange, setShowsPlanProgress, showsPlanProgress } from "../state/prefs";
import { Group } from "./Group";

export function PlanProgressSection() {
  const on = useSyncExternalStore(onPlanProgressChange, showsPlanProgress, showsPlanProgress);
  return (
    <Group id="plan-progress" title={t("收起时显示计划进度")} hint={t("收起的计划条上用小格子显示每一步的进度。默认显示，仅保存在本机。")}>
      <div className="seg" data-text role="group" aria-label={t("收起时显示计划进度")}>
        <button data-action="appearance.plan-progress" data-value="off" aria-pressed={!on} onClick={() => setShowsPlanProgress(false)}>{t("关闭")}</button>
        <button data-action="appearance.plan-progress" data-value="on" aria-pressed={on} onClick={() => setShowsPlanProgress(true)}>{t("显示")}</button>
      </div>
    </Group>
  );
}
