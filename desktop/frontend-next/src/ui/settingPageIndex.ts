import { PAGE_CARDS, pageHome, type Section, type SettingEntry } from "./prefsnav";

export interface PageCardEntry {
  title: string;
  anchor: string;
  entry: SettingEntry;
}

/** The cards a page's index lists: one per card that holds a visible setting,
 *  landing on the first of them in the order the page draws. */
export const pageIndexOf = (visible: readonly SettingEntry[], section: Section): PageCardEntry[] => {
  const here = visible.filter((e) => !e.within && pageHome(e.section) === section);
  return PAGE_CARDS.filter(([page]) => page === section).flatMap(([, title, anchors]) => {
    const entry = anchors.map((a) => here.find((e) => e.anchor === a)).find((e): e is SettingEntry => !!e);
    return entry ? [{ title, anchor: entry.anchor, entry }] : [];
  });
};
