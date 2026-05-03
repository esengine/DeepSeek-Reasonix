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

describe("layout — justifyContent in row flow", () => {
  it("flex-start (default) keeps children at the left", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row">
        <Text>A</Text>
        <Text>B</Text>
      </Box>,
      { width: 10, pools: p },
    );
    expect(readLine(s, p, 0).startsWith("AB")).toBe(true);
  });

  it("flex-end pushes children to the right", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row" justifyContent="flex-end">
        <Text>A</Text>
        <Text>B</Text>
      </Box>,
      { width: 10, pools: p },
    );
    expect(readLine(s, p, 0).endsWith("AB")).toBe(true);
  });

  it("center splits remaining slack evenly on both sides", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row" justifyContent="center">
        <Text>AB</Text>
      </Box>,
      { width: 10, pools: p },
    );
    expect(readLine(s, p, 0)).toBe("    AB    ");
  });

  it("space-between pushes first to left and last to right", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row" justifyContent="space-between">
        <Text>L</Text>
        <Text>R</Text>
      </Box>,
      { width: 10, pools: p },
    );
    const line = readLine(s, p, 0);
    expect(line.startsWith("L")).toBe(true);
    expect(line.endsWith("R")).toBe(true);
  });

  it("space-between with three children distributes gaps evenly", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row" justifyContent="space-between">
        <Text>A</Text>
        <Text>B</Text>
        <Text>C</Text>
      </Box>,
      { width: 9, pools: p },
    );
    const line = readLine(s, p, 0);
    expect(line.startsWith("A")).toBe(true);
    expect(line.endsWith("C")).toBe(true);
    expect(line.indexOf("B")).toBe(4);
  });

  it("flexGrow consumes slack — justifyContent becomes a no-op", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row" justifyContent="center">
        <Text>L</Text>
        <Box flexGrow={1}>
          <Text>x</Text>
        </Box>
        <Text>R</Text>
      </Box>,
      { width: 10, pools: p },
    );
    expect(readLine(s, p, 0).startsWith("L")).toBe(true);
    expect(readLine(s, p, 0).endsWith("R")).toBe(true);
  });
});
