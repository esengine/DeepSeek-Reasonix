import { Fragment } from "react";
import { t } from "../i18n";
import { ICON, NAV, type Section } from "./prefsnav";
import { arrowTabs } from "./tablist";
import { pagesOf } from "./settingPages";
import { UpdateDot } from "./UpdateDot";

interface Props {
  at: Section;
  values: Record<Section, string>;
  shown: (id: Section) => boolean;
  danger: (id: Section) => boolean;
  disabled: boolean;
  onSelect: (id: Section) => void;
  page?: string;
  onPage?: (section: Section, page: string) => void;
}

export function SettingsNav({ at, values, shown, danger, disabled, onSelect, page, onPage }: Props) {
  return (
    <nav className="prefs-nav" role="tablist" aria-label={t("设置分类")} onKeyDown={arrowTabs}>
      {NAV.map(([group, items]) => {
        const rows = items.filter(([id]) => shown(id));
        if (rows.length === 0) return null;
        return (
          <Fragment key={group}>
            <div className="navsec">{t(group)}</div>
            {rows.map(([id, name]) => (
              <Fragment key={id}>
              <button id={`prefs-${id}`} role="tab" title={t(name)}
                data-action="settings.section" data-value={id}
                tabIndex={at === id && pagesOf(id).length === 0 ? 0 : -1} data-open={at === id && pagesOf(id).length > 0 ? "" : undefined}
                aria-selected={at === id && pagesOf(id).length === 0} disabled={disabled} onClick={() => onSelect(id)}>
                <svg viewBox="0 0 16 16" aria-hidden="true">{ICON[id]}</svg>
                {id === "versions" && <UpdateDot />}
                <span className="nm">{t(name)}</span>
                <span className="nv" title={values[id] || undefined} data-danger={danger(id) ? "" : undefined}>
                  {values[id]}
                </span>
              </button>
              {at === id && pagesOf(id).map((p) => (
                <button key={p.id} id={`prefs-${id}-${p.id}`} role="tab" className="prefs-page" title={t(p.label)}
                  data-action="settings.page" data-value={`${id}:${p.id}`}
                  tabIndex={page === p.id ? 0 : -1} aria-selected={page === p.id} disabled={disabled}
                  onClick={() => onPage?.(id, p.id)}>
                  <span className="nm">{t(p.label)}</span>
                </button>
              ))}
              </Fragment>
            ))}
          </Fragment>
        );
      })}
    </nav>
  );
}
