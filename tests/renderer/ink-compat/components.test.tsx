// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { describe, expect, it } from "vitest";
import { Box, Spacer, Text } from "../../../src/renderer/ink-compat/index.js";
import { CharPool } from "../../../src/renderer/pools/char-pool.js";
import { HyperlinkPool } from "../../../src/renderer/pools/hyperlink-pool.js";
import { StylePool } from "../../../src/renderer/pools/style-pool.js";
import { render } from "../../../src/renderer/react/render.js";
import { CellWidth } from "../../../src/renderer/screen/cell.js";

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
    lines.push(line);
  }
  return lines;
}

describe("ink-compat Text — color and styles translate to SGR", () => {
  it("color='cyan' applies SGR 36 to the cell's style", () => {
    const p = pools();
    const s = render(<Text color="cyan">hi</Text>, { width: 5, pools: p });
    const styleId = s.cellAt(0, 0)?.styleId ?? -1;
    const expected = p.style.intern([{ apply: "\x1b[36m", revert: "\x1b[39m" }]);
    expect(styleId).toBe(expected);
  });

  it("bold + dimColor stack into a non-empty style id", () => {
    const p = pools();
    const s = render(
      <Text bold dimColor>
        hi
      </Text>,
      { width: 5, pools: p },
    );
    const styleId = s.cellAt(0, 0)?.styleId ?? -1;
    expect(styleId).not.toBe(p.style.none);
    const expected = p.style.intern([
      { apply: "\x1b[1m", revert: "\x1b[22m" },
      { apply: "\x1b[2m", revert: "\x1b[22m" },
    ]);
    expect(styleId).toBe(expected);
  });

  it("no styling produces the none style id", () => {
    const p = pools();
    const s = render(<Text>plain</Text>, { width: 10, pools: p });
    expect(s.cellAt(0, 0)?.styleId).toBe(p.style.none);
  });
});

describe("ink-compat Box — padding / borders", () => {
  it("paddingX adds left + right padding", () => {
    const p = pools();
    const s = render(
      <Box paddingX={2}>
        <Text>x</Text>
      </Box>,
      { width: 10, pools: p },
    );
    const lines = read(s, p);
    expect(lines[0]?.startsWith("  x")).toBe(true);
  });

  it("padding shorthand applies all four sides", () => {
    const p = pools();
    const s = render(
      <Box padding={1}>
        <Text>z</Text>
      </Box>,
      { width: 10, pools: p },
    );
    const lines = read(s, p);
    expect(lines.length).toBeGreaterThanOrEqual(3);
    expect(lines[1]?.startsWith(" z")).toBe(true);
  });

  it("borderStyle='single' draws four corners", () => {
    const p = pools();
    const s = render(
      <Box borderStyle="single">
        <Text>x</Text>
      </Box>,
      { width: 5, pools: p },
    );
    const lines = read(s, p);
    expect(lines[0]?.startsWith("┌")).toBe(true);
    expect(lines[0]?.includes("┐")).toBe(true);
    expect(lines[lines.length - 1]?.includes("└")).toBe(true);
  });

  it("borderColor='red' applies SGR 31 to border cells", () => {
    const p = pools();
    const s = render(
      <Box borderStyle="single" borderColor="red">
        <Text>x</Text>
      </Box>,
      { width: 5, pools: p },
    );
    const cornerStyle = s.cellAt(0, 0)?.styleId ?? -1;
    const expected = p.style.intern([{ apply: "\x1b[31m", revert: "\x1b[39m" }]);
    expect(cornerStyle).toBe(expected);
  });
});

describe("ink-compat Box — margins translate to wrapper padding", () => {
  it("marginTop=2 leaves two empty rows above content", () => {
    const p = pools();
    const s = render(
      <Box marginTop={2}>
        <Text>hello</Text>
      </Box>,
      { width: 10, pools: p },
    );
    const lines = read(s, p);
    expect(lines[0]?.trim()).toBe("");
    expect(lines[1]?.trim()).toBe("");
    expect(lines[2]).toContain("hello");
  });

  it("marginLeft=3 indents the box", () => {
    const p = pools();
    const s = render(
      <Box marginLeft={3}>
        <Text>hi</Text>
      </Box>,
      { width: 10, pools: p },
    );
    expect(read(s, p)[0]?.startsWith("   hi")).toBe(true);
  });

  it("marginY=1 adds one row above and below", () => {
    const p = pools();
    const s = render(
      <Box marginY={1}>
        <Text>z</Text>
      </Box>,
      { width: 5, pools: p },
    );
    const lines = read(s, p);
    expect(lines[0]?.trim()).toBe("");
    expect(lines[1]).toContain("z");
    expect(lines[2]?.trim()).toBe("");
  });
});

describe("ink-compat Spacer — flexGrow fills remaining row space", () => {
  it("Spacer between two Text nodes pushes them apart in row flow", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row">
        <Text>L</Text>
        <Spacer />
        <Text>R</Text>
      </Box>,
      { width: 10, pools: p },
    );
    const line = read(s, p)[0] ?? "";
    expect(line.startsWith("L")).toBe(true);
    expect(line.endsWith("R")).toBe(true);
  });
});

describe("ink-compat Box — width / height pass through", () => {
  it("width={N} clips content past the box's outer width", () => {
    const p = pools();
    const s = render(
      <Box width={6}>
        <Text>abcdefghij</Text>
      </Box>,
      { width: 20, pools: p },
    );
    expect(read(s, p)[0]?.startsWith("abcdef ")).toBe(true);
    expect(read(s, p)[0]?.includes("ghij")).toBe(false);
  });

  it("height={1} on an empty Box renders a single empty spacer row", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="column">
        <Text>up</Text>
        <Box height={1} />
        <Text>dn</Text>
      </Box>,
      { width: 5, pools: p },
    );
    const lines = read(s, p);
    expect(lines[0]).toContain("up");
    expect(lines[1]?.trim()).toBe("");
    expect(lines[2]).toContain("dn");
  });
});

describe("ink-compat Box — justifyContent passes through", () => {
  it("space-between distributes children to both edges", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row" justifyContent="space-between">
        <Text>L</Text>
        <Text>R</Text>
      </Box>,
      { width: 12, pools: p },
    );
    const line = read(s, p)[0] ?? "";
    expect(line.startsWith("L")).toBe(true);
    expect(line.endsWith("R")).toBe(true);
  });

  it("center centers a single child", () => {
    const p = pools();
    const s = render(
      <Box flexDirection="row" justifyContent="center">
        <Text>X</Text>
      </Box>,
      { width: 7, pools: p },
    );
    expect(read(s, p)[0]).toBe("   X   ");
  });
});
