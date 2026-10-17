import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onSessionQuickActionsChange, setShowsSessionQuickActions, showsSessionQuickActions } from "../state/sessionQuick";
import { Group } from "./Group";

export function SessionQuickSection() {
  const on = useSyncExternalStore(onSessionQuickActionsChange, showsSessionQuickActions, showsSessionQuickActions);
  return (
    <Group id="session-quick-actions" title={t("会话行快捷按钮")} hint={t("悬停或键盘聚焦会话时，左侧显示置顶按钮，右侧显示归档按钮。默认显示；关闭后可通过会话菜单操作。")}>
      <div className="seg" data-text role="group" aria-label={t("会话行快捷按钮")}>
        <button data-action="appearance.session-quick-actions" data-value="off" aria-pressed={!on} onClick={() => setShowsSessionQuickActions(false)}>{t("关闭")}</button>
        <button data-action="appearance.session-quick-actions" data-value="on" aria-pressed={on} onClick={() => setShowsSessionQuickActions(true)}>{t("显示")}</button>
      </div>
    </Group>
  );
}
