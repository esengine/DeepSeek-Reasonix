import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onShowsSessionTabsChange, setShowsSessionTabs, showsSessionTabs } from "../state/prefs";
import { Group } from "./Group";
import { Switch } from "./Switch";

export function SessionTabsSetting() {
  const sessionTabs = useSyncExternalStore(onShowsSessionTabsChange, showsSessionTabs, showsSessionTabs);

  return (
    <Group id="session-tabs" title={t("session.session_tabs")}>
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("session.show_session_tabs")}</span>
          <span className="ds">{t("session.show_tabs_at_the_top")}</span>
        </span>
        <Switch data-action="appearance.session-tabs" on={sessionTabs} label={t("session.show_session_tabs")}
          onClick={() => setShowsSessionTabs(!sessionTabs)} />
      </div>
    </Group>
  );
}
