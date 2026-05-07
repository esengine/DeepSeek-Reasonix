import { describe, expect, it } from "vitest";
import { diffFrames } from "../../../src/renderer/diff/diff-frames.js";
import { type Frame, emptyFrame } from "../../../src/renderer/diff/frame.js";
import type { Patch } from "../../../src/renderer/diff/patch.js";
import { CharPool } from "../../../src/renderer/pools/char-pool.js";
import { HyperlinkPool } from "../../../src/renderer/pools/hyperlink-pool.js";
import { type AnsiCode, StylePool } from "../../../src/renderer/pools/style-pool.js";
import { type Cell, CellWidth, EMPTY_CELL } from "../../../src/renderer/screen/cell.js";
import { Screen } from "../../../src/renderer/screen/screen.js";

const RED: AnsiCode = { apply: "\x1b[31m", revert: "\x1b[39m" };
const BOLD: AnsiCode = { apply: "\x1b[1m", revert: "\x1b[22m" };

interface Pools {
  char: CharPool;
  style: StylePool;
  hyperlink: HyperlinkPool;
}

function pools(): Pools {
  return {
    char: new CharPool(),
    style: new StylePool(),
    hyperlink: new HyperlinkPool(),
  };
}

function frame(width: number, height: number, fill?: (s: Screen, p: Pools) => void): Frame {
  const p = (frame as unknown as { _pools?: Pools })._pools;
  if (!p) throw new Error("set frame._pools first");
  const s = new Screen(width, height);
  if (fill) fill(s, p);
  return {
    screen: s,
    viewportWidth: width,
    viewportHeight: height,
    cursor: { x: 0, y: 0, visible: true },
  };
}

function withPools<T>(p: Pools, fn: () => T): T {
  (frame as unknown as { _pools?: Pools })._pools = p;
  try {
    return fn();
  } finally {
    (frame as unknown as { _pools?: Pools })._pools = undefined;
  }
}

function cell(p: Pools, char: string, style?: AnsiCode[], hyperlink?: string): Cell {
  const w = char.length === 0 ? CellWidth.Single : CellWidth.Single;
  return {
    charId: p.char.intern(char),
    styleId: style ? p.style.intern(style) : p.style.none,
    hyperlinkId: p.hyperlink.intern(hyperlink),
    width: w,
  };
}

describe("diffFrames — empty diff", () => {
  it("returns no patches when frames are identical", () => {
    const p = pools();
    withPools(p, () => {
      const a = frame(5, 2);
      const b = frame(5, 2);
      expect(diffFrames(a, b, p)).toEqual([]);
    });
  });
});

describe("diffFrames — single-cell change", () => {
  it("positions cursor + writes stdout for one changed cell", () => {
    const p = pools();
    withPools(p, () => {
      const a = frame(5, 2);
      const b = frame(5, 2, (s) => s.writeCell(2, 0, cell(p, "x")));
      const out = diffFrames(a, b, p);
      const stdout = out.filter((pp): pp is Patch & { type: "stdout" } => pp.type === "stdout");
      expect(stdout.map((p) => p.content)).toEqual(["x"]);
      // Column targeting goes through CHA absolute (cursorTo) — relative CUF would
      // accumulate drift across frames if any earlier write miscounted cell width.
      const cursorTos = out.filter(
        (pp): pp is Patch & { type: "cursorTo" } => pp.type === "cursorTo",
      );
      expect(cursorTos.map((c) => c.col)).toContain(2);
    });
  });

  it("a row of consecutive changes does not need cursor moves between adjacent cells", () => {
    const p = pools();
    withPools(p, () => {
      const a = frame(5, 1);
      const b = frame(5, 1, (s) => {
        s.writeCell(0, 0, cell(p, "a"));
        s.writeCell(1, 0, cell(p, "b"));
        s.writeCell(2, 0, cell(p, "c"));
      });
      const out = diffFrames(a, b, p);
      const positioningPatches = out.filter(
        (pp) => pp.type === "cursorMove" || pp.type === "cursorTo",
      );
      // First write needs at most one position; subsequent writes ride on
      // the terminal's natural cursor advance, so no extra moves between.
      expect(positioningPatches.length).toBeLessThanOrEqual(1);
    });
  });
});

describe("diffFrames — cursor positioning hardening (claude-code/issues/14208)", () => {
  it("never emits relative CUF/CUB (column dx) for cell positioning — CHA only", () => {
    const p = pools();
    withPools(p, () => {
      const a = frame(20, 5, (s) => {
        for (const [x, ch] of "hello".split("").entries()) s.writeCell(x + 2, 1, cell(p, ch));
        for (const [x, ch] of "world".split("").entries()) s.writeCell(x + 7, 3, cell(p, ch));
      });
      const b = frame(20, 5, (s) => {
        for (const [x, ch] of "HELLO".split("").entries()) s.writeCell(x + 2, 1, cell(p, ch));
        for (const [x, ch] of "WORLD".split("").entries()) s.writeCell(x + 7, 3, cell(p, ch));
      });
      const out = diffFrames(a, b, p);
      const horizMoves = out.filter(
        (pp): pp is Patch & { type: "cursorMove" } => pp.type === "cursorMove" && pp.dx !== 0,
      );
      expect(horizMoves).toEqual([]);
    });
  });
});

describe("diffFrames — style transitions", () => {
  it("emits styleStr only when style changes", () => {
    const p = pools();
    withPools(p, () => {
      const a = frame(5, 1);
      const b = frame(5, 1, (s) => {
        s.writeCell(0, 0, cell(p, "a", [RED]));
        s.writeCell(1, 0, cell(p, "b", [RED]));
      });
      const out = diffFrames(a, b, p);
      const styleStrs = out.filter(
        (pp): pp is Patch & { type: "styleStr" } => pp.type === "styleStr",
      );
      // One transition into RED at the start. Then back out at the end (reset).
      expect(styleStrs.length).toBeGreaterThanOrEqual(1);
      expect(styleStrs.length).toBeLessThanOrEqual(2);
      expect(styleStrs[0]!.str).toContain("\x1b[31m");
    });
  });

  it("transitions cleanly between distinct styles in adjacent cells", () => {
    const p = pools();
    withPools(p, () => {
      const a = frame(3, 1);
      const b = frame(3, 1, (s) => {
        s.writeCell(0, 0, cell(p, "x", [RED]));
        s.writeCell(1, 0, cell(p, "y", [BOLD]));
      });
      const out = diffFrames(a, b, p);
      const styleStrs = out.filter(
        (pp): pp is Patch & { type: "styleStr" } => pp.type === "styleStr",
      );
      // Enter RED, transition RED→BOLD, exit BOLD at end.
      expect(styleStrs.length).toBeGreaterThanOrEqual(2);
    });
  });

  it("resets style to none at the end of the diff so subsequent terminal output is unstyled", () => {
    const p = pools();
    withPools(p, () => {
      const a = frame(2, 1);
      const b = frame(2, 1, (s) => s.writeCell(0, 0, cell(p, "x", [RED])));
      const out = diffFrames(a, b, p);
      const styleStrs = out.filter((pp) => pp.type === "styleStr");
      const lastStyle = styleStrs[styleStrs.length - 1] as Patch & { type: "styleStr" };
      expect(lastStyle.str).toContain("\x1b[39m");
    });
  });
});

describe("diffFrames — hyperlink transitions", () => {
  it("emits hyperlink open + close around linked cells", () => {
    const p = pools();
    withPools(p, () => {
      const a = frame(3, 1);
      const b = frame(3, 1, (s) => {
        s.writeCell(0, 0, cell(p, "h", undefined, "https://example.com"));
        s.writeCell(1, 0, cell(p, "i", undefined, "https://example.com"));
      });
      const out = diffFrames(a, b, p);
      const links = out.filter(
        (pp): pp is Patch & { type: "hyperlink" } => pp.type === "hyperlink",
      );
      expect(links[0]!.uri).toBe("https://example.com");
      expect(links[links.length - 1]!.uri).toBe("");
    });
  });
});

describe("diffFrames — wide-char (CJK) handling", () => {
  it("skips SpacerTail; emits compensation around wide glyph (leading space + cha + char + cha)", () => {
    const p = pools();
    withPools(p, () => {
      const a = frame(4, 1);
      const b = frame(4, 1, (s) => {
        s.writeCell(0, 0, {
          charId: p.char.intern("你"),
          styleId: p.style.none,
          hyperlinkId: 0,
          width: CellWidth.Wide,
        });
        s.writeCell(1, 0, {
          charId: 1,
          styleId: p.style.none,
          hyperlinkId: 0,
          width: CellWidth.SpacerTail,
        });
      });
      const out = diffFrames(a, b, p);
      const stdout = out.filter((pp): pp is Patch & { type: "stdout" } => pp.type === "stdout");
      expect(stdout.map((p) => p.content)).toEqual([" ", "你"]);
    });
  });
});

describe("diffFrames — viewport resize → full reset", () => {
  it("emits clearTerminal when viewport dimensions differ", () => {
    const p = pools();
    withPools(p, () => {
      const a: Frame = {
        ...frame(5, 2),
        viewportWidth: 5,
        viewportHeight: 2,
      };
      const b: Frame = {
        ...frame(7, 3, (s) => s.writeCell(0, 0, cell(p, "z"))),
        viewportWidth: 7,
        viewportHeight: 3,
      };
      const out = diffFrames(a, b, p);
      expect(out[0]).toEqual({ type: "clearTerminal" });
    });
  });
});

describe("diffFrames — cell removed", () => {
  it("writes a space where a cell used to be", () => {
    const p = pools();
    withPools(p, () => {
      const a = frame(3, 1, (s) => s.writeCell(1, 0, cell(p, "x")));
      const b = frame(3, 1);
      const out = diffFrames(a, b, p);
      const stdout = out.filter((pp): pp is Patch & { type: "stdout" } => pp.type === "stdout");
      expect(stdout.map((p) => p.content)).toContain(" ");
    });
  });
});

describe("diffFrames — cursor restore", () => {
  it("repositions the cursor to the next.cursor position when no cells changed", () => {
    const p = pools();
    withPools(p, () => {
      const a = frame(5, 2);
      const b: Frame = { ...a, cursor: { x: 3, y: 1, visible: true } };
      const out = diffFrames(a, b, p);
      // No cell changes, so the only thing we'd emit is a cursor move + style/hyperlink reset.
      const moves = out.filter(
        (pp): pp is Patch & { type: "cursorMove" } => pp.type === "cursorMove",
      );
      expect(moves.length).toBeGreaterThan(0);
    });
  });
});

describe("emptyFrame", () => {
  it("returns a frame with a 0×0 screen and visible cursor", () => {
    const f = emptyFrame(80, 24);
    expect(f.screen.width).toBe(0);
    expect(f.screen.height).toBe(0);
    expect(f.viewportWidth).toBe(80);
    expect(f.viewportHeight).toBe(24);
    expect(f.cursor).toEqual({ x: 0, y: 0, visible: true });
  });

  it("first-render diff against an empty frame writes the full screen", () => {
    const p = pools();
    withPools(p, () => {
      const prev = emptyFrame(5, 2);
      // Match viewport so we don't trigger fullReset.
      const next: Frame = {
        ...frame(5, 2, (s) => {
          s.writeCell(0, 0, cell(p, "h"));
          s.writeCell(1, 0, cell(p, "i"));
        }),
        viewportWidth: 5,
        viewportHeight: 2,
      };
      const out = diffFrames({ ...prev, viewportWidth: 5, viewportHeight: 2 }, next, p);
      const stdout = out.filter((pp): pp is Patch & { type: "stdout" } => pp.type === "stdout");
      expect(stdout.map((p) => p.content).join("")).toContain("hi");
    });
  });
});

describe("diffFrames — issue #330 row-shrink trail clear", () => {
  it("emits clearToEOL when prev row had content past next's last visible cell", () => {
    const p = pools();
    withPools(p, () => {
      const a = frame(40, 1, (s) => {
        const text = "  run this command, ask again next time";
        for (let x = 0; x < text.length; x++) s.writeCell(x, 0, cell(p, text[x]!));
      });
      const b = frame(40, 1, (s) => {
        const text = "  allow always";
        for (let x = 0; x < text.length; x++) s.writeCell(x, 0, cell(p, text[x]!));
      });
      const out = diffFrames(a, b, p);
      const trail = out.filter((pp) => pp.type === "clearToEOL");
      expect(trail).toHaveLength(1);
    });
  });

  it("does NOT emit clearToEOL when next row reaches the same width as prev", () => {
    const p = pools();
    withPools(p, () => {
      const a = frame(10, 1, (s) => {
        for (const [x, ch] of "abcdefghij".split("").entries()) s.writeCell(x, 0, cell(p, ch));
      });
      const b = frame(10, 1, (s) => {
        for (const [x, ch] of "ABCDEFGHIJ".split("").entries()) s.writeCell(x, 0, cell(p, ch));
      });
      const out = diffFrames(a, b, p);
      expect(out.filter((pp) => pp.type === "clearToEOL")).toHaveLength(0);
    });
  });
});

// Silence the unused-EMPTY_CELL import lint by referencing it.
// eslint-disable-next-line
void EMPTY_CELL;
