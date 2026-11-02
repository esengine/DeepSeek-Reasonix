import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onPlanProgressChange, setShowsPlanProgress, showsPlanProgress } from "../state/prefs";
import { Group } from "./Group";

export function PlanProgressSection() {
  const on = useSyncExternalStore(onPlanProgressChange, showsPlanProgress, showsPlanProgress);
  return (
    <Group id="plan-progress" title={t("收起时显示计划进度")} hint={t("在收起的计划卡片下方显示进度条，随已完成步骤更新。默认关闭，仅保存在本机。")}>
      <div className="seg" data-text role="group" aria-label={t("收起时显示计划进度")}>
        <button data-action="appearance.plan-progress" data-value="off" aria-pressed={!on} onClick={() => setShowsPlanProgress(false)}>{t("关闭")}</button>
        <button data-action="appearance.plan-progress" data-value="on" aria-pressed={on} onClick={() => setShowsPlanProgress(true)}>{t("显示")}</button>
      </div>
    </Group>
  );
}
