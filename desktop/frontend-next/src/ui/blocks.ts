import { useRef } from "react";
import type { Item } from "../state/session";

// How many settled cards a mounting unit gathers before it closes at the next
// turn. Small enough that scrolling mounts a block within a frame, large enough
// that a long session holds a few hundred blocks rather than tens of thousands.
const BLOCK = 48;

// A streamed delta rewrites only the last card, so the settled head is cut into
// blocks that keep their array identity across frames and each block can memo.
// Anything that edits a card mid-list bumps the revision, so re-cutting on
// revision or cut alone walks the blocks once per turn rather than per frame.
export function useBlocks(items: Item[], cut: number, revision: number): Item[][] {
  const held = useRef<{ at: number; cut: number; blocks: Item[][] }>({ at: -1, cut: -1, blocks: [] });
  if (held.current.at === revision && held.current.cut === cut) return held.current.blocks;

  const prev = held.current.blocks;
  const blocks: Item[][] = [];
  for (let at = 0, end = 0; at < cut; at = end) {
    end = turnEnd(items, at, cut);
    while (end < cut && end - at < BLOCK) end = turnEnd(items, end, cut);
    const old = prev[blocks.length];
    let same = old !== undefined && old.length === end - at;
    for (let i = 0; same && i < end - at; i++) same = old[i] === items[at + i];
    blocks.push(same ? old : items.slice(at, end));
  }
  held.current = { at: revision, cut, blocks };
  return blocks;
}

// A block ends only where a turn does: transcriptRows hangs work under its
// sentence within one block, so a turn cut in two leaves half its work with no
// sentence to sit under.
function turnEnd(items: Item[], from: number, cut: number): number {
  let end = from + 1;
  while (end < cut && !opensTurn(items[end])) end++;
  return end;
}

// A queued line has not happened yet, and a steer was said into a turn already
// running; neither is a turn of its own.
export const opensTurn = (it: Item) => it.t === "user" && !it.pending && !it.steer;
