import { termsOf, type SettingEntry } from "./prefsnav";
import { rankTerms } from "./settingRank";

/** The settings a query finds, best first. Ties keep the catalogue's order, so
 *  the page a setting lives on still reads as the page's own sequence. */
export function searchSettings(entries: readonly SettingEntry[], query: string): SettingEntry[] {
  return entries
    .map((entry, order) => ({ entry, order, score: rankTerms(termsOf(entry), query) }))
    .filter((hit) => hit.score > 0)
    .sort((a, b) => b.score - a.score || a.order - b.order)
    .map((hit) => hit.entry);
}
