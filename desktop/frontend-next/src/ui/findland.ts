import { useEffect, useMemo, useRef, type RefObject } from "react";
import type { Item } from "../state/session";
import { clearFind, currentRange, paintFind } from "./findpaint";

type Land = (
  block: number,
  into: number,
  selector: string,
  clear?: number,
  then?: (found: HTMLElement, root: HTMLElement) => boolean,
) => void;

// The find bar floats over the transcript's top edge, so a found row is landed
// this far down instead of the usual clearance, or it arrives under the bar.
const FIND_BAR = 56;

/** Scrolls to the row the find bar is on, each time it moves. The row may sit
 *  in a block that is not mounted, so it is found by block first, the same way
 *  the rail lands on a message. */
export function useFindLanding(
  find: { id: string; nth: number; n: number } | null | undefined,
  query: string,
  hidden: boolean,
  blocks: Item[][],
  live: Item | undefined,
  land: Land,
) {
  const rowBlock = useMemo(() => {
    const at = new Map<string, { block: number; into: number }>();
    blocks.forEach((block, b) =>
      block.forEach((it, i) => at.set(it.id, { block: b, into: block.length > 1 ? i / block.length : 0 })),
    );
    if (live) at.set(live.id, { block: blocks.length - 1, into: 1 });
    return at;
  }, [blocks, live]);

  const found = useRef(0);
  useEffect(() => {
    if (hidden || !find || found.current === find.n) return;
    found.current = find.n;
    const where = rowBlock.get(find.id);
    const needle = query.trim().toLocaleLowerCase();
    const nth = find.nth;
    const n = find.n;
    const mine = () => found.current === n;
    if (where) land(where.block, where.into, `[data-item="${CSS.escape(find.id)}"]`, FIND_BAR, (row, root) => reveal(row, root, needle, nth, mine));
  }, [hidden, find, query, rowBlock, land]);
}

/** Puts the occurrence being looked at where it is read, and says whether it
 *  could: a row can be taller than the view, so its top is not enough. Rows
 *  above it may still mount and move it, so it holds for a few frames. */
function reveal(row: HTMLElement, root: HTMLElement, needle: string, nth: number, mine: () => boolean, tries = 8): boolean {
  if (!needle || !mine()) return false;
  landingBox(row);
  const range = currentRange(row, needle, nth);
  if (!range) return false;
  for (let fold = range.startContainer.parentElement?.closest("details:not([open])"); fold && row.contains(fold);
    fold = fold.parentElement?.closest("details:not([open])")) {
    (fold as HTMLDetailsElement).open = true;
  }
  if (range.getBoundingClientRect().height === 0) return false;
  // A long output scrolls inside its own clip, and the view scrolling does not
  // reach an occurrence that clip holds below its fold.
  for (let el = range.startContainer.parentElement; el && el !== root; el = el.parentElement) {
    if (el.scrollHeight <= el.clientHeight + 1 || !/auto|scroll/.test(getComputedStyle(el).overflowY)) continue;
    const at = range.getBoundingClientRect();
    const clip = el.getBoundingClientRect();
    if (at.top < clip.top || at.bottom > clip.bottom) el.scrollTop += at.top - (clip.top + clip.height / 3);
  }
  const box = range.getBoundingClientRect();
  const view = root.getBoundingClientRect();
  const top = view.top + FIND_BAR;
  const bottom = view.bottom - view.height / 4;
  if (box.top < top || box.bottom > bottom) root.scrollTop += box.top - (top + (bottom - top) / 3);
  if (tries > 0) requestAnimationFrame(() => reveal(row, root, needle, nth, mine, tries - 1));
  return true;
}

/** What a landing scrolls to: the element with a box, and every fold around it
 *  opened, since a hit inside a folded group is one nobody can see. */
export function landingBox(el: HTMLElement): HTMLElement {
  for (let fold = el.closest("details:not([open])"); fold; fold = fold.parentElement?.closest("details:not([open])") ?? null) {
    (fold as HTMLDetailsElement).open = true;
  }
  if (el.getClientRects().length > 0) return el;
  return (el.firstElementChild as HTMLElement | null) ?? el;
}

/** Keeps the find paint on what is mounted. Rows mount as the reader scrolls and
 *  as answers stream in, and a paint taken before that misses them, so while a
 *  query stands it is taken again on either, at most once a frame. */
export function useFindPaint(
  flow: RefObject<HTMLElement | null>,
  hidden: boolean,
  query: string,
  current: string | null,
  nth: number,
) {
  useEffect(() => {
    if (hidden || !query.trim()) return clearFind();
    let frame = 0;
    const paint = () => {
      if (frame) return;
      frame = requestAnimationFrame(() => {
        frame = 0;
        paintFind(flow.current, query, current === null ? null : { id: current, nth });
      });
    };
    paint();
    addEventListener("scroll", paint, true);
    const watch = new MutationObserver(paint);
    if (flow.current) watch.observe(flow.current, { childList: true, subtree: true, characterData: true });
    return () => {
      cancelAnimationFrame(frame);
      removeEventListener("scroll", paint, true);
      watch.disconnect();
      clearFind();
    };
  }, [flow, hidden, query, current, nth]);
}
