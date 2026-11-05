import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onSessionQuickActionsChange, setShowsSessionQuickActions, showsSessionQuickActions } from "../state/sessionQuick";
import { Group } from "./Group";

export function SessionQuickSection() {
  const on = useSyncExternalStore(onSessionQuickActionsChange, showsSessionQuickActions, showsSessionQuickActions);
  return (
    <Group id="session-quick-actions" inline title={t("session.session_row_buttons")} hint={t("settings.buttons_shown_on_row_hover")}>
      <div className="seg" data-text role="group" aria-label={t("session.session_row_buttons")}>
        <button data-action="appearance.session-quick-actions" data-value="off" aria-pressed={!on} onClick={() => setShowsSessionQuickActions(false)}>{t("common.close")}</button>
        <button data-action="appearance.session-quick-actions" data-value="on" aria-pressed={on} onClick={() => setShowsSessionQuickActions(true)}>{t("common.show")}</button>
      </div>
    </Group>
  );
}
