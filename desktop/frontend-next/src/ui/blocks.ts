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
  const previousByFirst = new Map(prev.map((block) => [block[0]?.id, block]));
  const blocks: Item[][] = [];
  for (let at = 0, end = 0; at < cut; at = end) {
    const candidate = previousByFirst.get(items[at].id);
    const keep = candidate && at + candidate.length <= cut && candidate.every((item, index) => item === items[at + index]);
    const boundary = keep && (at + candidate.length === cut || opensTurn(items[at + candidate.length]));
    if (keep && boundary && (candidate !== prev[prev.length - 1] || at + candidate.length === cut || candidate.length >= BLOCK)) {
      blocks.push(candidate);
      end = at + candidate.length;
      continue;
    }
    end = turnEnd(items, at, cut);
    while (end < cut && end - at < BLOCK && !previousByFirst.has(items[end].id)) end = turnEnd(items, end, cut);
    const old = candidate;
    const same = old?.length === end - at && old.every((item, index) => item === items[at + index]);
    blocks.push(same && old ? old : items.slice(at, end));
  }
  held.current = { at: revision, cut, blocks };
  return blocks;
}

// A late history read can put older rows before an existing form in its block.
// The surviving rows own that mounting identity, not the new first row.
export function useBlockKeys(blocks: Item[][]): string[] {
  const held = useRef<{ blocks: Item[][] | null; keys: string[]; owners: Map<string, string>; next: number }>({ blocks: null, keys: [], owners: new Map(), next: 0 });
  if (held.current.blocks === blocks) return held.current.keys;
  const used = new Set<string>();
  const next = new Map<string, string>();
  const keys = blocks.map((block) => {
    let retained: string | undefined;
    for (const item of block) {
      const prior = held.current.owners.get(item.id);
      if (prior !== undefined && !used.has(prior)) { retained = prior; break; }
    }
    const key = retained ?? `block:${++held.current.next}`;
    used.add(key);
    for (const item of block) next.set(item.id, key);
    return key;
  });
  held.current = { ...held.current, blocks, keys, owners: next };
  return keys;
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
