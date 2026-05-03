// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { describe, expect, it } from "vitest";
import { CharPool } from "../../src/renderer/pools/char-pool.js";
import { HyperlinkPool } from "../../src/renderer/pools/hyperlink-pool.js";
import { StylePool } from "../../src/renderer/pools/style-pool.js";
import { Box, Text } from "../../src/renderer/react/components.js";
import { render } from "../../src/renderer/react/render.js";
import { CellWidth } from "../../src/renderer/screen/cell.js";

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

describe("Box padding — vertical", () => {
  it("paddingTop adds blank rows above content", () => {
    const p = pools();
    const s = render(
      <Box paddingTop={2}>
        <Text>hi</Text>
      </Box>,
      { width: 5, pools: p },
    );
    expect(read(s, p)).toEqual(["", "", "hi"]);
  });

  it("paddingBottom adds blank rows below content", () => {
    const p = pools();
    const s = render(
      <Box paddingBottom={1}>
        <Text>hi</Text>
      </Box>,
      { width: 5, pools: p },
    );
    expect(read(s, p)).toEqual(["hi", ""]);
  });

  it("paddingY shorthand sets top + bottom", () => {
    const p = pools();
    const s = render(
      <Box paddingY={1}>
        <Text>x</Text>
      </Box>,
      { width: 5, pools: p },
    );
    expect(read(s, p)).toEqual(["", "x", ""]);
  });
});

describe("Box padding — horizontal", () => {
  it("paddingLeft offsets each child row to the right", () => {
    const p = pools();
    const s = render(
      <Box paddingLeft={3}>
        <Text>hi</Text>
      </Box>,
      { width: 10, pools: p },
    );
    expect(read(s, p)).toEqual(["   hi"]);
  });

  it("paddingRight reduces effective width so wrapping triggers earlier", () => {
    const p = pools();
    const s = render(
      <Box paddingRight={4}>
        <Text>abcdef</Text>
      </Box>,
      { width: 8, pools: p },
    );
    // inner width = 8 - 4 = 4 → wraps after 'abcd'
    expect(read(s, p)).toEqual(["abcd", "ef"]);
  });

  it("paddingX shorthand sets left + right", () => {
    const p = pools();
    const s = render(
      <Box paddingX={2}>
        <Text>hi</Text>
      </Box>,
      { width: 6, pools: p },
    );
    expect(read(s, p)).toEqual(["  hi"]);
  });
});

describe("Box padding — combined", () => {
  it("padding shorthand sets all four sides", () => {
    const p = pools();
    const s = render(
      <Box padding={1}>
        <Text>x</Text>
      </Box>,
      { width: 5, pools: p },
    );
    // 1 row top, then ' x' (1 col left) then bottom row
    expect(read(s, p)).toEqual(["", " x", ""]);
  });

  it("specific side overrides shorthand", () => {
    const p = pools();
    const s = render(
      <Box padding={1} paddingLeft={3}>
        <Text>x</Text>
      </Box>,
      { width: 8, pools: p },
    );
    expect(read(s, p)).toEqual(["", "   x", ""]);
  });

  it("nested boxes accumulate padding offsets", () => {
    const p = pools();
    const s = render(
      <Box paddingLeft={2}>
        <Box paddingLeft={1}>
          <Text>x</Text>
        </Box>
      </Box>,
      { width: 10, pools: p },
    );
    expect(read(s, p)).toEqual(["   x"]);
  });
});

describe("Box padding — degenerate cases", () => {
  it("padding consuming all width yields no content rows but keeps top/bottom", () => {
    const p = pools();
    const s = render(
      <Box paddingX={3} paddingY={1}>
        <Text>hi</Text>
      </Box>,
      { width: 6, pools: p },
    );
    // inner width = 6 - 6 = 0 → text is dropped; only padding rows remain
    expect(read(s, p)).toEqual(["", ""]);
  });

  it("negative or NaN padding clamps to 0", () => {
    const p = pools();
    const s = render(
      <Box paddingLeft={-3} paddingTop={Number.NaN}>
        <Text>hi</Text>
      </Box>,
      { width: 5, pools: p },
    );
    expect(read(s, p)).toEqual(["hi"]);
  });
});
