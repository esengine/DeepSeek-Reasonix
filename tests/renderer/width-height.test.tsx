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

describe("layout — fixed width", () => {
  it("a Box with width=5 inside a width-20 row uses 5, not its intrinsic", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row">
        <Box width={5}>
          <Text>hi</Text>
        </Box>
        <Box>
          <Text>after</Text>
        </Box>
      </Box>,
      { width: 20, pools: p },
    );
    expect(readLine(s, p, 0).startsWith("hi")).toBe(true);
    expect(readLine(s, p, 0).slice(0, 10).includes("after")).toBe(true);
    expect(readLine(s, p, 0).indexOf("after")).toBe(5);
  });

  it("width clamps to availableWidth", () => {
    const p = pools();
    const s = render(
      <Box width={100}>
        <Text>x</Text>
      </Box>,
      { width: 8, pools: p },
    );
    expect(s.width).toBe(8);
  });
});

describe("layout — fixed height", () => {
  it("height=3 on an empty Box produces three empty rows", () => {
    const p = pools();
    const s = render(<Box height={3} />, { width: 5, pools: p });
    expect(s.height).toBe(3);
  });

  it("height=1 acts as a one-row spacer when content is empty", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="column">
        <Text>top</Text>
        <Box height={1} />
        <Text>bot</Text>
      </Box>,
      { width: 10, pools: p },
    );
    expect(readLine(s, p, 0)).toContain("top");
    expect(readLine(s, p, 1).trim()).toBe("");
    expect(readLine(s, p, 2)).toContain("bot");
  });

  it("content taller than height truncates to height", () => {
    const p = pools();
    const s = render(
      <Box height={2}>
        <Text>{"a\nb\nc\nd"}</Text>
      </Box>,
      { width: 5, pools: p },
    );
    expect(s.height).toBe(2);
    expect(readLine(s, p, 0)).toContain("a");
    expect(readLine(s, p, 1)).toContain("b");
  });

  it("content shorter than height pads bottom with empty rows", () => {
    const p = pools();
    const s = render(
      <Box height={4}>
        <Text>only</Text>
      </Box>,
      { width: 6, pools: p },
    );
    expect(s.height).toBe(4);
    expect(readLine(s, p, 0)).toContain("only");
    expect(readLine(s, p, 3).trim()).toBe("");
  });
});
