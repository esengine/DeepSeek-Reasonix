import { useSyncExternalStore } from "react";
import { t } from "../i18n";
import { NAV_RAIL_MODES, navRailMode, onNavRailChange, setNavRailMode, type NavRailMode } from "../state/prefs";
import { Group } from "./Group";

const LABEL: Record<NavRailMode, string> = { on: "settings.always", collapsed: "settings.only_when_the_sidebar_is", off: "settings.never" };

export function NavRailSection() {
  const mode = useSyncExternalStore(onNavRailChange, navRailMode, navRailMode);
  return (
    <Group id="navrail" inline title={t("settings.icon_rail")} hint={t("settings.which_entries_the_left_rail_shows")}>
      <div className="seg" data-text role="group" aria-label={t("settings.icon_rail")}>
        {NAV_RAIL_MODES.map((m) => (
          <button key={m} data-action="appearance.nav-rail" data-value={m} aria-pressed={mode === m} onClick={() => setNavRailMode(m)}>
            {t(LABEL[m])}
          </button>
        ))}
      </div>
      <p className="note">{t("settings.with_it_off_or_hidden")}</p>
    </Group>
  );
}
