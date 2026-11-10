import { t } from "../i18n";
import type { SettingEntry } from "./prefsnav";
import type { PageCardEntry } from "./settingPageIndex";

/** What a page holds, as a row of buttons under its title: one per card. Several
 *  pages carry others folded into them, and a card that lives there has no
 *  other sign of it until someone scrolls past it. */
export function PageIndex({ items, onGo }: { items: readonly PageCardEntry[]; onGo: (entry: SettingEntry) => void }) {
  if (items.length < 2) return null;
  return (
    <nav className="page-index" aria-label={t("settings.on_this_page")}>
      <span className="pi-label" aria-hidden="true">{t("settings.on_this_page")}</span>
      {items.map((e) => (
        <button key={e.anchor} type="button" className="spill" data-action="settings.page-index" data-target={e.anchor} onClick={() => onGo(e.entry)}>
          {t(e.title)}
        </button>
      ))}
    </nav>
  );
}
