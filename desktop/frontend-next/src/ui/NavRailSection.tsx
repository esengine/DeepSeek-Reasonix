import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { NAV_RAIL_MODES, navRailMode, onNavRailChange, setNavRailMode, type NavRailMode } from "../state/prefs";
import { Group } from "./Group";

const LABEL: Record<NavRailMode, string> = { on: "navRailSection.always", collapsed: "navRailSection.onlyWhenTheSidebarIs", off: "navRailSection.never" };

export function NavRailSection() {
  const mode = useSyncExternalStore(onNavRailChange, navRailMode, navRailMode);
  return (
    <Group id="navrail" title={t("common.iconRail")} hint={t("navRailSection.aColumnOfIconsLeft")}>
      <div className="seg" data-text role="group" aria-label={t("common.iconRail")}>
        {NAV_RAIL_MODES.map((m) => (
          <button key={m} data-action="appearance.nav-rail" data-value={m} aria-pressed={mode === m} onClick={() => setNavRailMode(m)}>
            {t(LABEL[m])}
          </button>
        ))}
      </div>
      <p className="note">{t("navRailSection.withItOffOrHidden")}</p>
    </Group>
  );
}
