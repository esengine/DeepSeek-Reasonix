import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { onShowsSessionTabsChange, setShowsSessionTabs, showsSessionTabs } from "../state/prefs";
import { Group } from "./Group";
import { Switch } from "./Switch";

export function SessionTabsSetting() {
  const sessionTabs = useSyncExternalStore(onShowsSessionTabsChange, showsSessionTabs, showsSessionTabs);

  return (
    <Group id="session-tabs" title={t("会话标签栏")}>
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("显示会话标签栏")}</span>
          <span className="ds">{t("打开多个会话时，在顶部显示标签，方便切换会话。")}</span>
        </span>
        <Switch data-action="appearance.session-tabs" on={sessionTabs} label={t("显示会话标签栏")}
          onClick={() => setShowsSessionTabs(!sessionTabs)} />
      </div>
    </Group>
  );
}
