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

function readLine(
  screen: ReturnType<typeof render>,
  p: ReturnType<typeof pools>,
  y: number,
): string {
  let line = "";
  for (let x = 0; x < screen.width; x++) {
    const cell = screen.cellAt(x, y);
    if (!cell) break;
    if (cell.width === CellWidth.SpacerTail) continue;
    line += p.char.get(cell.charId);
  }
  return line;
}

describe("layout — gap in column flow", () => {
  it("gap=1 inserts a single empty row between siblings", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="column" gap={1}>
        <Text>A</Text>
        <Text>B</Text>
        <Text>C</Text>
      </Box>,
      { width: 5, pools: p },
    );
    expect(readLine(s, p, 0)).toContain("A");
    expect(readLine(s, p, 1).trim()).toBe("");
    expect(readLine(s, p, 2)).toContain("B");
    expect(readLine(s, p, 3).trim()).toBe("");
    expect(readLine(s, p, 4)).toContain("C");
  });

  it("no gap on a single child", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="column" gap={2}>
        <Text>solo</Text>
      </Box>,
      { width: 6, pools: p },
    );
    expect(s.height).toBe(1);
  });
});

describe("layout — gap in row flow", () => {
  it("gap=2 leaves two columns of space between siblings", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row" gap={2}>
        <Text>A</Text>
        <Text>B</Text>
        <Text>C</Text>
      </Box>,
      { width: 9, pools: p },
    );
    expect(readLine(s, p, 0).slice(0, 7)).toBe("A  B  C");
  });

  it("gap reduces remaining width before flexGrow distribution", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row" gap={1}>
        <Box flexGrow={1}>
          <Text>L</Text>
        </Box>
        <Box flexGrow={1}>
          <Text>R</Text>
        </Box>
      </Box>,
      { width: 9, pools: p },
    );
    const line = readLine(s, p, 0);
    expect(line.startsWith("L")).toBe(true);
    expect(line[5]).toBe("R");
  });

  it("gap interacts with justifyContent=space-between", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row" gap={1} justifyContent="space-between">
        <Text>L</Text>
        <Text>M</Text>
        <Text>R</Text>
      </Box>,
      { width: 11, pools: p },
    );
    const line = readLine(s, p, 0);
    expect(line.startsWith("L")).toBe(true);
    expect(line.endsWith("R")).toBe(true);
    expect(line.indexOf("M")).toBeGreaterThan(0);
  });
});

describe("layout — gap counted in intrinsicWidth", () => {
  it("row-flow intrinsic accounts for gap between siblings", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row">
        <Box>
          <Text>start</Text>
        </Box>
        <Box flexDirection="row" gap={3}>
          <Text>A</Text>
          <Text>B</Text>
        </Box>
      </Box>,
      { width: 30, pools: p },
    );
    const line = readLine(s, p, 0);
    expect(line.startsWith("startA   B")).toBe(true);
  });
});
