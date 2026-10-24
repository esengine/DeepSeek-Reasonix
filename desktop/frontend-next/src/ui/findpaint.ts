import { afterMatch, finderFor, type Finder, type FindMode } from "../state/find";

// Ranges, not wrapper elements: marking matches by editing the tree would mean
// rewriting markdown React owns, inside cards that re-render on every delta.
const ALL = "rx-find";
const NOW = "rx-find-now";

/** The occurrence being looked at: its row, and which occurrence inside that
 *  row, counted the way the search counts. */
export interface FindAt {
  id: string;
  nth: number;
}

// Absent on older engines. The count and the scroll still work without it, so
// what is lost is the paint and nothing else.
const registry = (): HighlightRegistry | null =>
  typeof CSS !== "undefined" && "highlights" in CSS && typeof Highlight === "function" ? CSS.highlights : null;

export function clearFind(): void {
  const reg = registry();
  if (!reg) return;
  reg.delete(ALL);
  reg.delete(NOW);
}

/** Every occurrence the finder sees that `row` itself draws. */
export function rangesIn(row: HTMLElement, finder: Finder): Range[] {
  const out: Range[] = [];
  // A row can hold other rows (work under the sentence it followed); each text
  // belongs to its nearest row, or a hit is painted twice, once as the wrong row.
  const walk = document.createTreeWalker(row, NodeFilter.SHOW_TEXT, {
    acceptNode: (node) =>
      node.parentElement?.closest("[data-item]") === row ? NodeFilter.FILTER_ACCEPT : NodeFilter.FILTER_REJECT,
  });
  for (let node = walk.nextNode(); node; node = walk.nextNode()) {
    const text = node.nodeValue ?? "";
    const hay = finder.verbatim ? text : text.toLocaleLowerCase();
    // A range is addressed in the original's units, and lowercasing changes
    // length in some scripts. Skipping leaves the hit unpainted, never wrong.
    if (hay.length !== text.length) continue;
    for (let m = finder.match(hay, 0); m; m = afterMatch(finder, hay, m)) {
      // An empty match (`^`, a lookahead) covers nothing to paint.
      if (m.length === 0) continue;
      const range = document.createRange();
      range.setStart(node, m.at);
      range.setEnd(node, m.at + m.length);
      out.push(range);
    }
  }
  return out;
}

/** The range for the occurrence being looked at. A row can draw fewer
 *  occurrences than its text holds (a folded part, a shortened argument), so
 *  the count is clamped to what is drawn. */
export function currentRange(row: HTMLElement, finder: Finder, nth: number): Range | null {
  const drawn = rangesIn(row, finder);
  return drawn.length ? drawn[Math.min(nth, drawn.length - 1)] : null;
}

/** Paints occurrences under `root`, with the current one painted apart. Only
 *  mounted rows can be painted; the count speaks for the rest. */
export function paintFind(root: HTMLElement | null, query: string, current: FindAt | null, mode: FindMode = {}): void {
  const reg = registry();
  if (!reg) return;
  const finder = finderFor(query, mode);
  if (!root || !finder) return clearFind();

  const rest: Range[] = [];
  let now: Range | null = null;
  for (const row of root.querySelectorAll<HTMLElement>("[data-item]")) {
    const drawn = rangesIn(row, finder);
    if (current && row.dataset.item === current.id && drawn.length) {
      [now] = drawn.splice(Math.min(current.nth, drawn.length - 1), 1);
    }
    rest.push(...drawn);
  }
  reg.set(ALL, new Highlight(...rest));
  reg.set(NOW, now ? new Highlight(now) : new Highlight());
}
