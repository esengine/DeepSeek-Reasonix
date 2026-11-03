import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onShowsSessionTabsChange, setShowsSessionTabs, showsSessionTabs } from "../state/prefs";
import { Group } from "./Group";
import { Switch } from "./Switch";

export function SessionTabsSetting() {
  const sessionTabs = useSyncExternalStore(onShowsSessionTabsChange, showsSessionTabs, showsSessionTabs);

  return (
    <Group id="session-tabs" title={t("common.sessionTabs")}>
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("sessionTabsSetting.showSessionTabs")}</span>
          <span className="ds">{t("sessionTabsSetting.showTabsAtTheTop")}</span>
        </span>
        <Switch data-action="appearance.session-tabs" on={sessionTabs} label={t("sessionTabsSetting.showSessionTabs")}
          onClick={() => setShowsSessionTabs(!sessionTabs)} />
      </div>
    </Group>
  );
}
