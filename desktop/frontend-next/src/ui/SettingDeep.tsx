import { createContext, useContext, type ReactNode } from "react";
import { t } from "../i18n";
import { ApplyNote, ScopeTag } from "./Group";
import { SETTING_AT } from "./prefsnav";
import { StudioIcon } from "./StudioIcon";

interface Deep {
  // False outside the settings screen, where there is no page to go to and a
  // block draws in place.
  pages: boolean;
  open: string | null;
  section: string;
  go: (anchor: string | null) => void;
}

export const SettingDeepContext = createContext<Deep>({ pages: false, open: null, section: "", go: () => {} });

/** A block the catalogue gives a page of its own: on the section it is one row
 *  saying what it holds, and opened it is the whole column under a breadcrumb.
 *  Given `act`, the row is a setting row with a pill, and `ctl` sits beside it;
 *  otherwise a link card.
 *  Its contents mount only once it is opened. */
export function SettingDeep({ id, title, now, hint, act, ctl, children }: { id: string; title: string; now?: string; hint?: string; act?: string; ctl?: ReactNode; children: ReactNode }) {
  const deep = useContext(SettingDeepContext);
  if (!deep.pages || !SETTING_AT(id)?.page) return <>{children}</>;
  if (deep.open !== id && act) {
    return (
      <div className="srow" id={`set-${id}`} data-setting={id}>
        <div className="srow-meta">
          <span className="srow-name">{title}<ScopeTag id={id} /></span>
          {hint && <span className="srow-desc" title={hint}>{hint}</span>}
          <ApplyNote id={id} />
        </div>
        <div className="srow-ctl">
          {ctl}
          <button type="button" className="spill" data-action="settings.open-page" data-target={id} onClick={() => deep.go(id)}>{act}</button>
        </div>
      </div>
    );
  }
  if (deep.open !== id) {
    return (
      <section className="grp grp-link" id={`set-${id}`} data-setting={id}>
        <button type="button" className="grp-row" data-action="settings.open-page" data-target={id} onClick={() => deep.go(id)}>
          <span className="grp-row-t"><b>{title}</b>{hint && <small>{hint}</small>}</span>
          {now && <span className="now">{now}</span>}
          <StudioIcon name="chevron" />
        </button>
      </section>
    );
  }
  return (
    <div className="deep-page" data-deep-open={id} data-setting={id}>
      <nav className="deep-crumb" aria-label={t("settings.where_you_are")}>
        <button type="button" data-action="settings.close-page" onClick={() => deep.go(null)}>{deep.section}</button>
        <span aria-hidden="true">/</span>
        <b aria-current="page">{title}</b>
      </nav>
      {children}
    </div>
  );
}
