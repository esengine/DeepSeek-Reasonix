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
// (`searchEntries`) are merged with stable de-duplication keyed on the
// submitted path (`entry.path` when present, otherwise `entry.name`) so a result
// that appears in both lists is shown only once. The local list takes
// precedence (it represents the user's current directory and is more immediate).

import type { DirEntry } from "./types";

function entryKey(entry: DirEntry): string {
  return entry.path || entry.name;
}

function searchableText(entry: DirEntry): string {
  return [entry.name, entry.path, entry.displayName, entry.displayPath].filter(Boolean).join(" ").toLowerCase();
}

export function filterAtMatches(
  entries: readonly DirEntry[],
  searchEntries: readonly DirEntry[],
  atFrag: string,
): DirEntry[] {
  const frag = atFrag.toLowerCase();
  const local = entries.filter((e) => searchableText(e).includes(frag));
  const seen = new Set(local.map(entryKey));
  const searched = searchEntries.filter((e) => {
    if (seen.has(entryKey(e))) return false;
    return searchableText(e).includes(frag);
  });
  return [...local, ...searched];
}
