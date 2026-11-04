import { useRef, useState } from "react";
import { t } from "../i18n";
import { useDismiss } from "./dismiss";
import { StudioIcon, type StudioIconName } from "./StudioIcon";

export type ExtAddWay = "market" | "source" | "folder" | "service";

const WAYS: [ExtAddWay, StudioIconName, string, string][] = [
  ["market", "spark", "ext.add_from_market", "ext.add_from_market_hint"],
  ["source", "link", "ext.add_from_source", "ext.add_from_source_hint"],
  ["folder", "folder", "ext.add_from_folder", "ext.add_from_folder_hint"],
  ["service", "plug", "ext.add_service", "ext.add_service_hint"],
];

/** Every way something gets into the extensions list, behind one button. A way
 *  this window cannot take right now is left out rather than drawn disabled. */
export function ExtAddMenu({ offer, onPick, disabled }: { offer: ReadonlySet<ExtAddWay>; onPick: (way: ExtAddWay) => void; disabled?: boolean }) {
  const [open, setOpen] = useState(false);
  const box = useRef<HTMLDivElement>(null);
  useDismiss(open, box, () => setOpen(false));
  const ways = WAYS.filter(([way]) => offer.has(way));
  if (ways.length === 0) return null;
  return (
    <div className="ext-add" ref={box}>
      <button type="button" className="ext-add-btn" data-action="extensions.add" aria-haspopup="menu" aria-expanded={open} disabled={disabled}
        onClick={() => setOpen((v) => !v)}>
        <StudioIcon name="plus" />{t("ext.add_menu")}
      </button>
      {open && (
        <div className="ext-add-menu" role="menu" aria-label={t("ext.add_menu")}>
          {ways.map(([way, icon, title, hint]) => (
            <button key={way} type="button" role="menuitem" data-action="extensions.add" data-value={way}
              onClick={() => { setOpen(false); onPick(way); }}>
              <StudioIcon name={icon} />
              <span><b>{t(title)}</b><small>{t(hint)}</small></span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
