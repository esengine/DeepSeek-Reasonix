import { describe, expect, it } from "vitest";
import { type Cell, CellWidth, EMPTY_CELL } from "../../src/renderer/screen/cell.js";
import { Screen } from "../../src/renderer/screen/screen.js";

function makeCell(overrides: Partial<Cell> = {}): Cell {
  return { ...EMPTY_CELL, ...overrides };
}

describe("Screen", () => {
  it("constructs with width × height empty cells", () => {
    const s = new Screen(4, 3);
    expect(s.width).toBe(4);
    expect(s.height).toBe(3);
    expect(s.cells).toHaveLength(12);
    for (const c of s.cells) expect(c).toBe(EMPTY_CELL);
  });

  it("clamps negative dimensions to 0", () => {
    const s = new Screen(-5, -2);
    expect(s.width).toBe(0);
    expect(s.height).toBe(0);
    expect(s.cells).toHaveLength(0);
  });

  it("cellAt returns undefined for out-of-bounds coordinates", () => {
    const s = new Screen(2, 2);
    expect(s.cellAt(-1, 0)).toBeUndefined();
    expect(s.cellAt(0, -1)).toBeUndefined();
    expect(s.cellAt(2, 0)).toBeUndefined();
    expect(s.cellAt(0, 2)).toBeUndefined();
  });

  it("writeCell stores at the row-major index", () => {
    const s = new Screen(3, 2);
    const cell = makeCell({ charId: 5 });
    s.writeCell(1, 1, cell);
    expect(s.cellAt(1, 1)).toBe(cell);
    expect(s.cellAt(0, 0)).toBe(EMPTY_CELL);
  });

  it("writeCell silently ignores out-of-bounds writes", () => {
    const s = new Screen(2, 2);
    s.writeCell(99, 99, makeCell({ charId: 1 }));
    expect(s.damage).toBeUndefined();
  });

  it("damage starts undefined and grows to union of all writes", () => {
    const s = new Screen(10, 10);
    expect(s.damage).toBeUndefined();
    s.writeCell(2, 2, makeCell());
    expect(s.damage).toEqual({ x: 2, y: 2, width: 1, height: 1 });
    s.writeCell(5, 4, makeCell());
    expect(s.damage).toEqual({ x: 2, y: 2, width: 4, height: 3 });
  });

  it("resetDamage clears the rect", () => {
    const s = new Screen(4, 4);
    s.writeCell(1, 1, makeCell());
    expect(s.damage).toBeDefined();
    s.resetDamage();
    expect(s.damage).toBeUndefined();
  });

  it("Wide-cell writes carry their CellWidth marker", () => {
    const s = new Screen(4, 1);
    const wide = makeCell({ charId: 7, width: CellWidth.Wide });
    const tail = makeCell({ width: CellWidth.SpacerTail });
    s.writeCell(0, 0, wide);
    s.writeCell(1, 0, tail);
    expect(s.cellAt(0, 0)?.width).toBe(CellWidth.Wide);
    expect(s.cellAt(1, 0)?.width).toBe(CellWidth.SpacerTail);
  });
});
