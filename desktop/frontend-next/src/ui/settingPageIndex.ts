import { pageHome, type Section, type SettingEntry } from "./prefsnav";

/** The settings a page's index lists: everything on the page, folded-in pages
 *  included, except what is drawn inside another setting's own block. */
export const pageIndexOf = (visible: readonly SettingEntry[], section: Section): SettingEntry[] =>
  visible.filter((e) => !e.within && pageHome(e.section) === section);
