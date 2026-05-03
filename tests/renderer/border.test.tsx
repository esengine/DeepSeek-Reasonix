// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { describe, expect, it } from "vitest";
import type { BorderStyle } from "../../src/renderer/layout/borders.js";
import { CharPool } from "../../src/renderer/pools/char-pool.js";
import { HyperlinkPool } from "../../src/renderer/pools/hyperlink-pool.js";
import { type AnsiCode, StylePool } from "../../src/renderer/pools/style-pool.js";
import { Box, Text } from "../../src/renderer/react/components.js";
import { render } from "../../src/renderer/react/render.js";
import { CellWidth } from "../../src/renderer/screen/cell.js";

const RED: AnsiCode = { apply: "\x1b[31m", revert: "\x1b[39m" };

function pools() {
  return {
    char: new CharPool(),
    style: new StylePool(),
    hyperlink: new HyperlinkPool(),
  };
}

function read(screen: ReturnType<typeof render>, p: ReturnType<typeof pools>): string[] {
  const lines: string[] = [];
  for (let y = 0; y < screen.height; y++) {
    let line = "";
    for (let x = 0; x < screen.width; x++) {
      const cell = screen.cellAt(x, y);
      if (!cell) break;
      if (cell.width === CellWidth.SpacerTail) continue;
      line += p.char.get(cell.charId);
    }
    lines.push(line.replace(/ +$/, ""));
  }
  return lines;
}

describe("Box border — preset 'single'", () => {
  it("draws a complete frame around content", () => {
    const p = pools();
    const s = render(
      <Box borderStyle="single">
        <Text>hi</Text>
      </Box>,
      { width: 6, pools: p },
    );
    expect(read(s, p)).toEqual(["┌────┐", "│hi  │", "└────┘"]);
  });

  it("disabling a single side suppresses just that edge", () => {
    const p = pools();
    const s = render(
      <Box borderStyle="single" borderTop={false} borderRight={false} borderBottom={false}>
        <Text>x</Text>
      </Box>,
      { width: 5, pools: p },
    );
    expect(read(s, p)).toEqual(["│x"]);
  });
});

describe("Box border — preset 'round'", () => {
  it("uses round corner glyphs", () => {
    const p = pools();
    const s = render(
      <Box borderStyle="round">
        <Text>x</Text>
      </Box>,
      { width: 5, pools: p },
    );
    expect(read(s, p)).toEqual(["╭───╮", "│x  │", "╰───╯"]);
  });
});

describe("Box border — custom style object", () => {
  it("CardBox-style left bar with whitespace corners", () => {
    const p = pools();
    const BAR: BorderStyle = {
      topLeft: " ",
      top: " ",
      topRight: " ",
      right: " ",
      bottomLeft: " ",
      bottom: " ",
      bottomRight: " ",
      left: "▎",
    };
    const s = render(
      <Box borderStyle={BAR} borderTop={false} borderRight={false} borderBottom={false}>
        <Text>{"line1\nline2"}</Text>
      </Box>,
      { width: 8, pools: p },
    );
    expect(read(s, p)).toEqual(["▎line1", "▎line2"]);
  });
});

describe("Box border — width / wrapping interaction", () => {
  it("inner content wraps to width minus borders", () => {
    const p = pools();
    const s = render(
      <Box borderStyle="single">
        <Text>abcdef</Text>
      </Box>,
      { width: 6, pools: p },
    );
    // total width 6, borders eat 2 → inner = 4
    expect(read(s, p)).toEqual(["┌────┐", "│abcd│", "│ef  │", "└────┘"]);
  });

  it("border + padding compose: padding is inside the border", () => {
    const p = pools();
    const s = render(
      <Box borderStyle="single" paddingX={1}>
        <Text>x</Text>
      </Box>,
      { width: 7, pools: p },
    );
    expect(read(s, p)).toEqual(["┌─────┐", "│ x   │", "└─────┘"]);
  });
});

describe("Box border — color threading", () => {
  it("borderColor applies to all borders", () => {
    const p = pools();
    const s = render(
      <Box borderStyle="single" borderColor={[RED]}>
        <Text>x</Text>
      </Box>,
      { width: 5, pools: p },
    );
    const topLeft = s.cellAt(0, 0)!;
    expect(p.style.transition(p.style.none, topLeft.styleId)).toBe("\x1b[31m");
    const sideBar = s.cellAt(0, 1)!;
    expect(p.style.transition(p.style.none, sideBar.styleId)).toBe("\x1b[31m");
  });

  it("per-side color overrides the catch-all", () => {
    const p = pools();
    const BLUE: AnsiCode = { apply: "\x1b[34m", revert: "\x1b[39m" };
    const s = render(
      <Box borderStyle="single" borderColor={[RED]} borderLeftColor={[BLUE]}>
        <Text>x</Text>
      </Box>,
      { width: 5, pools: p },
    );
    const leftBar = s.cellAt(0, 1)!;
    expect(p.style.transition(p.style.none, leftBar.styleId)).toBe("\x1b[34m");
  });
});

describe("Box border — degenerate cases", () => {
  it("no borderStyle = no borders even if borderTop is true", () => {
    const p = pools();
    const s = render(
      <Box borderTop={true}>
        <Text>x</Text>
      </Box>,
      { width: 5, pools: p },
    );
    expect(read(s, p)).toEqual(["x"]);
  });

  it("border on a width-1 box collapses inner area to 0 but still renders edges if both sides set", () => {
    const p = pools();
    const s = render(
      <Box borderStyle="single">
        <Text>x</Text>
      </Box>,
      { width: 2, pools: p },
    );
    expect(read(s, p)).toEqual(["┌┐", "└┘"]);
  });
});
