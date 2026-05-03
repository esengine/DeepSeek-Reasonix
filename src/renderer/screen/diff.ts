import { type Cell, EMPTY_CELL, cellsEqual } from "./cell.js";
import type { Screen } from "./screen.js";

export type DiffCallback = (
  x: number,
  y: number,
  prev: Cell | undefined,
  next: Cell | undefined,
) => boolean | undefined;

export function diffEach(prev: Screen, next: Screen, cb: DiffCallback): boolean {
  const w = Math.max(prev.width, next.width);
  const h = Math.max(prev.height, next.height);
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      const a = prev.cellAt(x, y);
      const b = next.cellAt(x, y);
      if (a === b) continue;
      const ac = a ?? EMPTY_CELL;
      const bc = b ?? EMPTY_CELL;
      if (cellsEqual(ac, bc)) continue;
      if (cb(x, y, a, b) === true) return true;
    }
  }
  return false;
}
