import { StudioIcon } from "./StudioIcon";
import { Fragment, useEffect, useRef } from "react";
import { t } from "../i18n";
import { ICON, NAV, SECTION_DESC, SETTINGS, pageHome, type Section } from "./prefsnav";
import { arrowTabs } from "./tablist";
import { pagesOf } from "./settingPages";
import { UpdateDot } from "./UpdateDot";
import { useFolded } from "./usefold";
import { listenAction } from "./listen";

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

const countOf = (id: Section) => SETTINGS.filter((e) => !e.hidden && !e.within && pageHome(e.section) === id).length;

export function SettingsNav({ at, values, shown, danger, disabled, onSelect, page, onPage }: Props) {
  const strip = useRef<HTMLElement>(null);
  const phone = useFolded("phone");
  useEffect(() => {
    const nav = strip.current;
    if (!phone || !nav) return;
    const sideways = (e: Event) => {
      const wheel = e as WheelEvent;
      if (wheel.deltaX !== 0 || wheel.deltaY === 0) return;
      nav.scrollLeft += wheel.deltaY;
      wheel.preventDefault();
    };
    return listenAction(nav, "wheel", { action: "settings.sections-scroll", listener: sideways }, { passive: false });
  }, [phone]);
  useEffect(() => {
    if (!phone) return;
    strip.current?.querySelector("[aria-selected='true'], [data-open]")?.scrollIntoView?.({ inline: "nearest", block: "nearest" });
  }, [phone, at, page]);
  return (
    <nav ref={strip} className="prefs-nav" role="tablist" aria-label={t("settings.settings_sections")} onKeyDown={arrowTabs}>
      {NAV.map(([group, items]) => {
        const rows = items.filter(([id]) => shown(id));
        if (rows.length === 0) return null;
        return (
          <Fragment key={group}>
            {group && <div className="navsec">{t(group)}</div>}
            {rows.map(([id, name]) => (
              <Fragment key={id}>
              <button id={`prefs-${id}`} role="tab" title={t(name)}
                data-action="settings.section" data-value={id}
                tabIndex={at === id && pagesOf(id).length === 0 ? 0 : -1} data-open={at === id && pagesOf(id).length > 0 ? "" : undefined}
                aria-selected={at === id && pagesOf(id).length === 0} disabled={disabled} onClick={() => onSelect(id)}>
                {ICON[id]}
                {id === "account" && <UpdateDot />}
                <span className="nm">{t(name)}</span>
                <span className="nv" title={values[id] || undefined} data-danger={danger(id) ? "" : undefined}>
                  {countOf(id) || ""}
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

export function SettingsTitle({ at }: { at: Section }) {
  const group = NAV.find(([, items]) => items.some(([id]) => id === at));
  const name = group?.[1].find(([id]) => id === at)?.[1] ?? at;
  const count = SETTINGS.filter((e) => !e.hidden && !e.within && pageHome(e.section) === at).length;
  const desc = SECTION_DESC[at];
  return (
    <div className="prefs-pagetitle">
      {group?.[0] && <span className="group">{t(group[0])}<StudioIcon name="chevron" /></span>}
      <h2>{t(name)}</h2>
      {(desc || count > 0) && (
        <span className="desc">
          {desc && <span className="what">{t(desc)}</span>}
          {count > 0 && <span className="count">{t("settings.n_items", { n: count })}</span>}
        </span>
      )}
      <p className="usual">{t("settings.page_usual")}</p>
    </div>
  );
}
