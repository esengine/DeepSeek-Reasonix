import { useLayoutEffect, useRef } from "react";
import type { Section } from "./prefsnav";
import { hiddenAnchors } from "./settingPages";
import { LOOK_FAMILY, SETTINGS } from "./prefsnav";

export function SettingsPageStyle({ section, page }: { section: Section; page: string | undefined }) {
  const style = useRef<HTMLStyleElement>(null);
  useLayoutEffect(() => {
    const panel = style.current?.closest<HTMLElement>(".prefs-main");
    if (panel) panel.scrollTop = 0;
  }, [section, page]);
  const hidden = LOOK_FAMILY.includes(section)
    ? SETTINGS.filter((e) => LOOK_FAMILY.includes(e.section) && e.section !== section).map((e) => e.anchor)
    : page ? hiddenAnchors(section, page) : [];
  if (hidden.length === 0) return null;
  const blocks = hidden.map((id) => `.prefs-col [data-setting="${id}"]`).join(",");
  const wrappers = hidden.map((id) => `.prefs-col details:has(> [data-setting="${id}"])`).join(",");
  return <style ref={style}>{`${blocks}{display:none}${wrappers}{display:none}`}</style>;
}

/** A section drawn as a page of its own keeps only its own blocks out of the
 *  page it shares, and drops the cards left with none of them. */
export function SoloStyle({ section }: { section: Section }) {
  const own = SETTINGS.filter((e) => e.section === section).map((e) => `[data-setting="${e.anchor}"]`).join(",");
  return <style>{`.prefs-col [data-setting]:not(${own}){display:none}.prefs-col .prefs-card:not(:has(${own})){display:none}.prefs-col .prefs-card > header{display:none}`}</style>;
}
