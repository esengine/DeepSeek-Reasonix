import { describe, expect, it } from "vitest";
import { type RenderPools, renderToScreen } from "../../src/renderer/layout/layout.js";
import type { LayoutNode } from "../../src/renderer/layout/node.js";
import { CharPool } from "../../src/renderer/pools/char-pool.js";
import { HyperlinkPool } from "../../src/renderer/pools/hyperlink-pool.js";
import { type AnsiCode, StylePool } from "../../src/renderer/pools/style-pool.js";
import { CellWidth } from "../../src/renderer/screen/cell.js";

const RED: AnsiCode = { apply: "\x1b[31m", revert: "\x1b[39m" };

function pools(): RenderPools {
  return {
    char: new CharPool(),
    style: new StylePool(),
    hyperlink: new HyperlinkPool(),
  };
}

function read(screen: ReturnType<typeof renderToScreen>, p: ReturnType<typeof pools>): string[] {
  const lines: string[] = [];
  for (let y = 0; y < screen.height; y++) {
    let line = "";
    for (let x = 0; x < screen.width; x++) {
      const cell = screen.cellAt(x, y);
      if (!cell) break;
      if (cell.width === CellWidth.SpacerTail) continue;
      line += p.char.get(cell.charId);
    }
    lines.push(line.trimEnd());
  }
  return lines;
}

describe("renderToScreen — text only", () => {
  it("single text node fills one row", () => {
    const p = pools();
    const node: LayoutNode = { kind: "text", content: "hello" };
    const s = renderToScreen(node, 10, p);
    expect(s.height).toBe(1);
    expect(s.width).toBe(10);
    expect(read(s, p)).toEqual(["hello"]);
  });

  it("embedded \\n in content splits to multiple rows", () => {
    const p = pools();
    const node: LayoutNode = { kind: "text", content: "hi\nthere" };
    const s = renderToScreen(node, 10, p);
    expect(read(s, p)).toEqual(["hi", "there"]);
  });

  it("char-level wraps when content exceeds width", () => {
    const p = pools();
    const node: LayoutNode = { kind: "text", content: "abcdefghij" };
    const s = renderToScreen(node, 4, p);
    expect(read(s, p)).toEqual(["abcd", "efgh", "ij"]);
  });

  it("blank line in content produces an empty row", () => {
    const p = pools();
    const node: LayoutNode = { kind: "text", content: "a\n\nb" };
    const s = renderToScreen(node, 10, p);
    expect(read(s, p)).toEqual(["a", "", "b"]);
  });
});

describe("renderToScreen — box", () => {
  it("vertically stacks box children", () => {
    const p = pools();
    const node: LayoutNode = {
      kind: "box",
      children: [
        { kind: "text", content: "one" },
        { kind: "text", content: "two" },
        { kind: "text", content: "three" },
      ],
    };
    const s = renderToScreen(node, 10, p);
    expect(read(s, p)).toEqual(["one", "two", "three"]);
  });

  it("nested boxes flatten in source order", () => {
    const p = pools();
    const node: LayoutNode = {
      kind: "box",
      children: [
        { kind: "text", content: "a" },
        {
          kind: "box",
          children: [
            { kind: "text", content: "b" },
            { kind: "text", content: "c" },
          ],
        },
        { kind: "text", content: "d" },
      ],
    };
    const s = renderToScreen(node, 5, p);
    expect(read(s, p)).toEqual(["a", "b", "c", "d"]);
  });

  it("empty box yields zero rows", () => {
    const p = pools();
    const node: LayoutNode = { kind: "box", children: [] };
    const s = renderToScreen(node, 5, p);
    expect(s.height).toBe(0);
  });
});

describe("renderToScreen — style + hyperlink threading", () => {
  it("text style is interned and applied to its cells", () => {
    const p = pools();
    const node: LayoutNode = { kind: "text", content: "x", style: [RED] };
    const s = renderToScreen(node, 5, p);
    const cell = s.cellAt(0, 0)!;
    expect(cell.styleId).toBe(p.style.intern([RED]));
    expect(p.style.transition(p.style.none, cell.styleId)).toBe("\x1b[31m");
  });

  it("hyperlink uri is interned and applied", () => {
    const p = pools();
    const node: LayoutNode = {
      kind: "text",
      content: "z",
      hyperlink: "https://example.com",
    };
    const s = renderToScreen(node, 5, p);
    const cell = s.cellAt(0, 0)!;
    expect(p.hyperlink.get(cell.hyperlinkId)).toBe("https://example.com");
  });
});

describe("renderToScreen — wide-char support", () => {
  it("CJK glyph occupies two cells with a SpacerTail", () => {
    const p = pools();
    const node: LayoutNode = { kind: "text", content: "你好" };
    const s = renderToScreen(node, 4, p);
    expect(s.width).toBe(4);
    expect(s.cellAt(0, 0)?.width).toBe(CellWidth.Wide);
    expect(s.cellAt(1, 0)?.width).toBe(CellWidth.SpacerTail);
    expect(s.cellAt(2, 0)?.width).toBe(CellWidth.Wide);
    expect(s.cellAt(3, 0)?.width).toBe(CellWidth.SpacerTail);
    expect(p.char.get(s.cellAt(0, 0)!.charId)).toBe("你");
    expect(p.char.get(s.cellAt(2, 0)!.charId)).toBe("好");
  });

  it("wide char wraps when it does not fit in remaining columns", () => {
    const p = pools();
    const node: LayoutNode = { kind: "text", content: "a你" };
    const s = renderToScreen(node, 2, p);
    expect(s.height).toBe(2);
    expect(read(s, p)).toEqual(["a", "你"]);
  });
});

describe("renderToScreen — edge cases", () => {
  it("zero width yields zero rows", () => {
    const p = pools();
    const node: LayoutNode = { kind: "text", content: "abc" };
    const s = renderToScreen(node, 0, p);
    expect(s.height).toBe(0);
  });

  it("damage is reset on the returned Screen", () => {
    const p = pools();
    const node: LayoutNode = { kind: "text", content: "hello" };
    const s = renderToScreen(node, 5, p);
    expect(s.damage).toBeUndefined();
  });
});
