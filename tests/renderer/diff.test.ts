import { describe, expect, it, vi } from "vitest";
import { type Cell, CellWidth, EMPTY_CELL } from "../../src/renderer/screen/cell.js";
import { diffEach } from "../../src/renderer/screen/diff.js";
import { Screen } from "../../src/renderer/screen/screen.js";

function cell(overrides: Partial<Cell> = {}): Cell {
  return { ...EMPTY_CELL, ...overrides };
}

describe("diffEach", () => {
  it("identical screens produce no callbacks", () => {
    const a = new Screen(3, 2);
    const b = new Screen(3, 2);
    const cb = vi.fn();
    diffEach(a, b, cb);
    expect(cb).not.toHaveBeenCalled();
  });

  it("a single-cell difference produces a single callback at that position", () => {
    const a = new Screen(3, 2);
    const b = new Screen(3, 2);
    b.writeCell(1, 1, cell({ charId: 42 }));
    const calls: Array<[number, number]> = [];
    diffEach(a, b, (x, y) => {
      calls.push([x, y]);
    });
    expect(calls).toEqual([[1, 1]]);
  });

  it("returning true halts the walk early", () => {
    const a = new Screen(3, 3);
    const b = new Screen(3, 3);
    b.writeCell(0, 0, cell({ charId: 1 }));
    b.writeCell(2, 2, cell({ charId: 2 }));
    let visited = 0;
    const halted = diffEach(a, b, () => {
      visited++;
      return true;
    });
    expect(halted).toBe(true);
    expect(visited).toBe(1);
  });

  it("structurally identical cells with different object identity are NOT reported", () => {
    const a = new Screen(2, 1);
    const b = new Screen(2, 1);
    const c1 = cell({ charId: 5, styleId: 3 });
    const c2 = cell({ charId: 5, styleId: 3 });
    a.writeCell(0, 0, c1);
    b.writeCell(0, 0, c2);
    const cb = vi.fn();
    diffEach(a, b, cb);
    expect(cb).not.toHaveBeenCalled();
  });

  it("reports differing widths even when char and style match", () => {
    const a = new Screen(1, 1);
    const b = new Screen(1, 1);
    a.writeCell(0, 0, cell({ charId: 5 }));
    b.writeCell(0, 0, cell({ charId: 5, width: CellWidth.Wide }));
    const cb = vi.fn();
    diffEach(a, b, cb);
    expect(cb).toHaveBeenCalledTimes(1);
  });

  it("walks the union of differing dimensions when prev/next aren't the same size", () => {
    const a = new Screen(1, 1);
    const b = new Screen(2, 2);
    b.writeCell(1, 1, cell({ charId: 9 }));
    const calls: Array<[number, number]> = [];
    diffEach(a, b, (x, y) => {
      calls.push([x, y]);
    });
    expect(calls).toContainEqual([1, 1]);
  });
});
