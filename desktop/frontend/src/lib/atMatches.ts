// atMatches filters the @-menu candidates shown in the Composer. It is
// extracted from the Composer component so it can be unit-tested without
// mounting the full React tree.
//
// The match logic mirrors the v2 fuzzy @-search behavior: the user's
// fragment is matched against each entry's full relative path (which the
// backend already returns as a slash-normalized string for search
// results, and as a single-segment name for ListDir results). This
// allows entries like "src/planind/index.tsx" to surface when the user
// types a directory segment such as "planind", not just the basename
// "index.tsx".
//
// Entries from the local ListDir (`entries`) and the fuzzy Search
// (`searchEntries`) are merged with stable de-duplication keyed on
// `entry.name` so a result that appears in both lists is shown only
// once. The local list takes precedence (it represents the user's
// current directory and is more immediate).

import type { DirEntry } from "./types";

export function filterAtMatches(
  entries: readonly DirEntry[],
  searchEntries: readonly DirEntry[],
  atFrag: string,
): DirEntry[] {
  const frag = atFrag.toLowerCase();
  const local = entries.filter((e) => e.name.toLowerCase().includes(frag));
  const seen = new Set(local.map((e) => e.name));
  const searched = searchEntries.filter((e) => {
    if (seen.has(e.name)) return false;
    return e.name.toLowerCase().includes(frag);
  });
  return [...local, ...searched];
}
