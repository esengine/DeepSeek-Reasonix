import { t } from "../i18n";
import type { SettingEntry } from "./prefsnav";

/** What a page holds, as a row of buttons under its title. Several pages carry
 *  others folded into them, and a setting that lives there has no other sign
 *  of it until someone scrolls past it. */
export function PageIndex({ items, onGo }: { items: readonly SettingEntry[]; onGo: (entry: SettingEntry) => void }) {
  if (items.length < 3) return null;
  return (
    <nav className="page-index" aria-label={t("settings.on_this_page")}>
      <span className="pi-label" aria-hidden="true">{t("settings.on_this_page")}</span>
      {items.map((e) => (
        <button key={e.anchor} type="button" className="spill" data-action="settings.page-index" data-target={e.anchor} onClick={() => onGo(e)}>
          {t(e.title)}
        </button>
      ))}
    </nav>
  );
}
