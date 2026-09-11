// DockTabPicker is the dock's empty state: with no tab open the panel offers
// the views it can hold instead of showing nothing. The layout follows the
// host pane's own width via a container query — a list while the dock is
// narrow, a card grid once it is wide enough for two or more columns.
import { useT } from "../../lib/i18n";
import { availableDockEntries } from "../../lib/dockEntries";
import { desktopHost } from "../../lib/desktopHost";
import { DOCK_ENTRY_ICONS } from "../dockEntryIcons";

export function DockTabPicker({ onSelect }: { onSelect: (entryId: string) => void }) {
  const t = useT();
  const entries = availableDockEntries(desktopHost().browser !== undefined);

  return (
    <div className="tab-picker">
      <div className="tab-picker__content">
        <div className="tab-picker__heading">
          <h2 className="tab-picker__title">{t("rightDock.openTab")}</h2>
          <p className="tab-picker__hint">{t("rightDock.openTabHint")}</p>
        </div>
        <div className="tab-picker__list">
          {entries.map((entry) => {
            const Icon = DOCK_ENTRY_ICONS[entry.defaultTab];
            return (
              <button
                key={entry.id}
                type="button"
                className="tab-picker__item"
                data-dock-picker-entry={entry.id}
                onClick={() => onSelect(entry.id)}
              >
                <Icon size={16} className="tab-picker__icon" />
                <span className="tab-picker__label">{t(entry.labelKey as never)}</span>
              </button>
            );
          })}
        </div>
      </div>
    </div>
  );
}
