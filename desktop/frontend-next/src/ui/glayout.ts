import type { GraphNode } from "../port/wire";
import type { Lane } from "../state/graph";

// Node boxes are a fixed size, so a lane's geometry follows from its ranks with
// nothing measured. That is what lets the edge layer and the cards agree: both
// read the same numbers instead of one of them reading the DOM after paint.
export const W = 190;
export const H = 58;
const COLGAP = 64;
const ROWGAP = 12;
// A row a wire passes through rather than a card. Reserving one is what keeps a
// long edge off the cards it flies over, and a wire needs far less of it.
const WIRE = 10;
// A sweep that made the picture worse is discarded, so more rounds can never
// hurt — they just stop paying for themselves on a fan-out this size.
const ROUNDS = 4;

export interface Spot {
  x: number;
  y: number;
}

export interface PlacedNode {
  node: GraphNode;
  x: number;
  y: number;
}

// One link between two nodes, folded from the two facts the kernel publishes
// about them. Ordering and delivery share a line because they share a pair;
// drawing them as two lines would say there are two relationships.
export interface Wire {
  from: string;
  to: string;
  carried: boolean;
  points: Spot[];
}

export interface Layout {
  nodes: PlacedNode[];
  wires: Wire[];
  w: number;
  h: number;
}

// One key per ordered pair. A node id may hold a slash or a space, so joining
// two of them with either is ambiguous; encoding the pair is not.
export const pairKey = (from: string, to: string) => JSON.stringify([from, to]);

// A cell is one row of a column: either a node or a wire passing through. The
// leading tag keeps the two namespaces apart whatever a node id spells.
const nodeCell = (id: string) => "n" + id;
const wireCell = (key: string, rank: number) => `w${rank} ${key}`;
const isWire = (cell: string) => cell[0] === "w";
const idOf = (cell: string) => cell.slice(1);

interface Link {
  from: string;
  to: string;
  carried: boolean;
}

function foldLinks(lane: Lane, rankOf: Map<string, number>): Link[] {
  const pairs = new Map<string, Link>();
  for (const e of lane.edges) {
    // An edge naming a node this lane does not hold has nowhere to land.
    if (!rankOf.has(e.from) || !rankOf.has(e.to)) continue;
    const key = pairKey(e.from, e.to);
    const held = pairs.get(key) ?? { from: e.from, to: e.to, carried: false };
    if (e.kind === "context") held.carried = true;
    pairs.set(key, held);
  }
  return [...pairs.values()];
}

/** layout arranges one lane: which row each node takes, and the path each wire
 *  travels to reach it. Ranks alone place nothing — they say which column a
 *  node sits in and leave the row to whatever order the deltas arrived in. */
export function layout(lane: Lane): Layout {
  const rankOf = new Map<string, number>();
  lane.ranks.forEach((rank, i) => rank.forEach((n) => rankOf.set(n.id, i)));
  const nodeAt = new Map(lane.ranks.flat().map((n) => [n.id, n]));
  const links = foldLinks(lane, rankOf);

  // A waypoint is reserved in every rank a long edge crosses. Without them the
  // edge is drawn straight over whatever cards stand in between, which reads as
  // a line pointing at a node it has nothing to do with.
  const cols = lane.ranks.map((rank) => rank.map((n) => nodeCell(n.id)));
  const steps: [string, string][] = [];
  for (const link of links) {
    const key = pairKey(link.from, link.to);
    let prev = nodeCell(link.from);
    for (let r = rankOf.get(link.from)! + 1; r < rankOf.get(link.to)!; r++) {
      const cell = wireCell(key, r);
      cols[r].push(cell);
      steps.push([prev, cell]);
      prev = cell;
    }
    steps.push([prev, nodeCell(link.to)]);
  }

  const ordered = minimizeCrossings(cols, steps);
  const high = (cell: string) => (isWire(cell) ? WIRE : H);
  const deep = (col: string[]) => col.reduce((h, cell, i) => h + high(cell) + (i ? ROWGAP : 0), 0);
  const tallest = Math.max(H, ...ordered.map(deep));

  // Columns are centred against the tallest. Hanging every column from the top
  // pins a lone node to a corner while its neighbours spread past it, which
  // reads as a step that ran early rather than one that ran by itself.
  const y = new Map<string, number>();
  for (const col of ordered) {
    let at = (tallest - deep(col)) / 2;
    for (const cell of col) {
      y.set(cell, at);
      at += high(cell) + ROWGAP;
    }
  }
  const colX = (rank: number) => rank * (W + COLGAP);

  const nodes: PlacedNode[] = [];
  ordered.forEach((col, rank) => {
    for (const cell of col) {
      const node = isWire(cell) ? undefined : nodeAt.get(idOf(cell));
      if (node) nodes.push({ node, x: colX(rank), y: y.get(cell)! });
    }
  });

  const wires = links.map((link) => {
    const key = pairKey(link.from, link.to);
    const from = rankOf.get(link.from)!;
    const to = rankOf.get(link.to)!;
    const points: Spot[] = [{ x: colX(from) + W, y: y.get(nodeCell(link.from))! + H / 2 }];
    for (let r = from + 1; r < to; r++) {
      points.push({ x: colX(r) + W / 2, y: y.get(wireCell(key, r))! + WIRE / 2 });
    }
    points.push({ x: colX(to), y: y.get(nodeCell(link.to))! + H / 2 });
    return { from: link.from, to: link.to, carried: link.carried, points };
  });

  return {
    nodes,
    wires,
    w: ordered.length * W + Math.max(0, ordered.length - 1) * COLGAP,
    h: tallest,
  };
}

// Sugiyama's middle step, which this panel never had. A rank says which column
// a node is in and nothing about its row, so the order the deltas arrived in
// decided it: wires crossed for no reason a reader could act on, and the same
// run redrawn came out different. Sweeping each column by the median of its
// neighbours' rows is the standard answer, and keeping the best sweep is what
// stops a round from making the picture worse.
function minimizeCrossings(cols: string[][], steps: [string, string][]): string[][] {
  const up = new Map<string, string[]>();
  const down = new Map<string, string[]>();
  for (const [a, b] of steps) {
    down.set(a, [...(down.get(a) ?? []), b]);
    up.set(b, [...(up.get(b) ?? []), a]);
  }
  let best = cols.map((c) => c.slice());
  let cheapest = crossings(best, steps);
  const cur = cols.map((c) => c.slice());
  for (let round = 0; round < ROUNDS && cheapest > 0; round++) {
    sweep(cur, up, 1);
    sweep(cur, down, -1);
    const cost = crossings(cur, steps);
    if (cost < cheapest) {
      cheapest = cost;
      best = cur.map((c) => c.slice());
    }
  }
  return best;
}

function sweep(cols: string[][], nbrs: Map<string, string[]>, dir: 1 | -1): void {
  const first = dir === 1 ? 1 : cols.length - 2;
  const last = dir === 1 ? cols.length : -1;
  for (let i = first; i !== last; i += dir) {
    const fixed = new Map(cols[i - dir].map((cell, row) => [cell, row] as const));
    // A cell with no neighbour in the fixed column keeps its own row as its key,
    // so it holds its place instead of sinking to one end.
    const key = new Map(cols[i].map((cell, row) => [cell, median(nbrs.get(cell), fixed, row)] as const));
    cols[i] = cols[i].slice().sort((a, b) => key.get(a)! - key.get(b)!);
  }
}

function median(of: string[] | undefined, fixed: Map<string, number>, fallback: number): number {
  const rows = (of ?? [])
    .map((cell) => fixed.get(cell))
    .filter((row): row is number => row !== undefined)
    .sort((a, b) => a - b);
  if (rows.length === 0) return fallback;
  const mid = rows.length >> 1;
  return rows.length % 2 ? rows[mid] : (rows[mid - 1] + rows[mid]) / 2;
}

// Two wires between the same pair of columns cross when their endpoints run in
// opposite directions. Counting them is what makes a sweep more than a guess.
function crossings(cols: string[][], steps: [string, string][]): number {
  let total = 0;
  for (let i = 0; i + 1 < cols.length; i++) {
    const left = new Map(cols[i].map((cell, row) => [cell, row] as const));
    const right = new Map(cols[i + 1].map((cell, row) => [cell, row] as const));
    const spans: [number, number][] = [];
    for (const [a, b] of steps) {
      const from = left.get(a);
      const to = right.get(b);
      if (from !== undefined && to !== undefined) spans.push([from, to]);
    }
    for (let a = 0; a < spans.length; a++) {
      for (let b = a + 1; b < spans.length; b++) {
        if ((spans[a][0] - spans[b][0]) * (spans[a][1] - spans[b][1]) < 0) total++;
      }
    }
  }
  return total;
}

/** wirePath draws one wire through its waypoints. Every leg leaves and enters
 *  horizontally, so a chain of them reads as one line and not a zigzag. */
export function wirePath(points: Spot[]): string {
  let d = `M ${points[0].x} ${points[0].y}`;
  for (let i = 1; i < points.length; i++) {
    const a = points[i - 1];
    const b = points[i];
    const bend = Math.max(18, (b.x - a.x) / 2);
    d += ` C ${a.x + bend} ${a.y}, ${b.x - bend} ${b.y}, ${b.x} ${b.y}`;
  }
  return d;
}
