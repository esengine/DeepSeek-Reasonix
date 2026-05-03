export const CellWidth = {
  Single: 1,
  Wide: 2,
  /** Occupies the column following a Wide glyph; renderer must skip it. */
  SpacerTail: 3,
} as const;

export type CellWidth = (typeof CellWidth)[keyof typeof CellWidth];

export interface Cell {
  charId: number;
  styleId: number;
  hyperlinkId: number;
  width: CellWidth;
}

export const EMPTY_CELL: Cell = Object.freeze({
  charId: 0,
  styleId: 0,
  hyperlinkId: 0,
  width: CellWidth.Single,
});

export function cellsEqual(a: Cell, b: Cell): boolean {
  return (
    a.charId === b.charId &&
    a.styleId === b.styleId &&
    a.hyperlinkId === b.hyperlinkId &&
    a.width === b.width
  );
}
