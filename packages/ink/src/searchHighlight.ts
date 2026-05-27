import {
  CellWidth,
  cellAtIndex,
  type Screen,
  type StylePool,
  setCellStyleId,
} from './screen.js';

/** Invert every visible occurrence of `query` in the screen buffer. */
export function applySearchHighlight(
  screen: Screen,
  query: string,
  stylePool: StylePool,
): boolean {
  if (!query) return false;

  const needle = query.toLowerCase();
  const needleLen = needle.length;
  const width = screen.width;
  const height = screen.height;
  const { noSelect } = screen;

  let highlighted = false;

  for (let row = 0; row < height; row++) {
    const rowOffset = row * width;
    let lowerText = '';
    const colByCellIndex: number[] = [];
    const codeUnitToCell: number[] = [];

    for (let col = 0; col < width; col++) {
      const idx = rowOffset + col;
      const cell = cellAtIndex(screen, idx);
      if (
        cell.width === CellWidth.SpacerTail ||
        cell.width === CellWidth.SpacerHead ||
        noSelect[idx] === 1
      ) {
        continue;
      }
      const lower = cell.char.toLowerCase();
      const cellIndex = colByCellIndex.length;
      for (let i = 0; i < lower.length; i++) {
        codeUnitToCell.push(cellIndex);
      }
      lowerText += lower;
      colByCellIndex.push(col);
    }

    let pos = lowerText.indexOf(needle);
    while (pos >= 0) {
      highlighted = true;
      const startCell = codeUnitToCell[pos]!;
      const endCell = codeUnitToCell[pos + needleLen - 1]!;
      for (let cellIdx = startCell; cellIdx <= endCell; cellIdx++) {
        const col = colByCellIndex[cellIdx]!;
        const cell = cellAtIndex(screen, rowOffset + col);
        setCellStyleId(screen, col, row, stylePool.withInverse(cell.styleId));
      }
      // Advance past the match (not by one) — `aaa` should yield one
      // highlight starting at index 0, not two starting at 0 and 1, so
      // a cell isn't double-inverted (which would cancel the invert).
      pos = lowerText.indexOf(needle, pos + needleLen);
    }
  }

  return highlighted;
}
