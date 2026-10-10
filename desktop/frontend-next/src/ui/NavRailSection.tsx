import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { NAV_RAIL_MODES, navRailMode, onNavRailChange, setNavRailMode, type NavRailMode } from "../state/prefs";
import { Group } from "./Group";

const LABEL: Record<NavRailMode, string> = { on: "始终显示", collapsed: "仅侧栏收起时显示", off: "不显示" };

export function NavRailSection() {
  const mode = useSyncExternalStore(onNavRailChange, navRailMode, navRailMode);
  return (
    <Group id="navrail" title={t("图标栏")} hint={t("会话侧栏左边的一列图标，直达用量、工具、扩展、记忆、远程、反馈、账号和设置。窗口窄到手机宽度时不显示。")}>
      <div className="seg" data-text role="group" aria-label={t("图标栏")}>
        {NAV_RAIL_MODES.map((m) => (
          <button key={m} data-action="appearance.nav-rail" data-value={m} aria-pressed={mode === m} onClick={() => setNavRailMode(m)}>
            {t(LABEL[m])}
          </button>
        ))}
      </div>
      <p className="note">{t("关闭或收起时，这些页面仍可从设置和侧栏打开")}</p>
    </Group>
  );
}
