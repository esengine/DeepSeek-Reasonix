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

describe("flex direction row", () => {
  it("places sibling Texts side-by-side at intrinsic widths", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row">
        <Text>hi </Text>
        <Text>there</Text>
      </Box>,
      { width: 20, pools: p },
    );
    expect(read(s, p)).toEqual(["hi there"]);
  });

  it("aligns children of differing heights at the top, leaving blanks on the right", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row">
        <Text>{"a\nb\nc"}</Text>
        <Text>X</Text>
      </Box>,
      { width: 5, pools: p },
    );
    expect(read(s, p)).toEqual(["aX", "b", "c"]);
  });

  it("nested column inside row stacks vertically within its slot", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row">
        <Box>
          <Text>1</Text>
          <Text>2</Text>
        </Box>
        <Text>|R</Text>
      </Box>,
      { width: 10, pools: p },
    );
    expect(read(s, p)).toEqual(["1|R", "2"]);
  });

  it("scales children proportionally when intrinsic widths exceed available", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row">
        <Text>aaaa</Text>
        <Text>bbbb</Text>
      </Box>,
      { width: 4, pools: p },
    );
    // Each child gets 2 cols (4/2). Text wraps to fit.
    expect(read(s, p)).toEqual(["aabb", "aabb"]);
  });
});

describe("flexGrow — distributes leftover space", () => {
  it("flexGrow=1 spacer between fixed children fills the gap", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row">
        <Text>L</Text>
        <Box flexGrow={1} />
        <Text>R</Text>
      </Box>,
      { width: 10, pools: p },
    );
    expect(read(s, p)).toEqual(["L        R"]);
  });

  it("two flexGrow children split slack proportionally", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row">
        <Box flexGrow={1}>
          <Text>aaa</Text>
        </Box>
        <Box flexGrow={3}>
          <Text>bbbbbbbbb</Text>
        </Box>
      </Box>,
      { width: 12, pools: p },
    );
    // Both children request their intrinsic widths first (3, 9 = 12), leaving 0 slack.
    expect(read(s, p)).toEqual(["aaabbbbbbbbb"]);
  });

  it("flexGrow only kicks in when there is slack", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row">
        <Text>fixed</Text>
        <Box flexGrow={1}>
          <Text>x</Text>
        </Box>
      </Box>,
      { width: 10, pools: p },
    );
    // 'fixed' takes 5, x takes 1, slack = 4 distributed to flexGrow=1 → grows to 5
    expect(read(s, p)).toEqual(["fixedx"]);
  });
});

describe("flex row + padding", () => {
  it("paddingX of an outer row Box reduces the available row width", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row" paddingX={1}>
        <Text>L</Text>
        <Box flexGrow={1} />
        <Text>R</Text>
      </Box>,
      { width: 10, pools: p },
    );
    // paddingLeft = paddingRight = 1 → inner = 8 → "L      R", shifted by 1
    expect(read(s, p)).toEqual([" L      R"]);
  });
});

describe("default direction is column", () => {
  it("Box without flexDirection prop still stacks children vertically", () => {
    const p = pools();
    const s = render(
      <Box>
        <Text>top</Text>
        <Text>bottom</Text>
      </Box>,
      { width: 10, pools: p },
    );
    expect(read(s, p)).toEqual(["top", "bottom"]);
  });
});
