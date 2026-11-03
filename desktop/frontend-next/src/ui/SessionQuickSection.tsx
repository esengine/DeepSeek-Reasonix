import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onSessionQuickActionsChange, setShowsSessionQuickActions, showsSessionQuickActions } from "../state/sessionQuick";
import { Group } from "./Group";

export function SessionQuickSection() {
  const on = useSyncExternalStore(onSessionQuickActionsChange, showsSessionQuickActions, showsSessionQuickActions);
  return (
    <Group id="session-quick-actions" title={t("common.sessionRowButtons")} hint={t("sessionQuickSection.showPinOnTheLeft")}>
      <div className="seg" data-text role="group" aria-label={t("common.sessionRowButtons")}>
        <button data-action="appearance.session-quick-actions" data-value="off" aria-pressed={!on} onClick={() => setShowsSessionQuickActions(false)}>{t("common.close")}</button>
        <button data-action="appearance.session-quick-actions" data-value="on" aria-pressed={on} onClick={() => setShowsSessionQuickActions(true)}>{t("common.show")}</button>
      </div>
    </Group>
  );
}
