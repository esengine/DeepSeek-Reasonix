// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { Fragment } from "react";
import { describe, expect, it } from "vitest";
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
    lines.push(line.trimEnd());
  }
  return lines;
}

describe("render — host elements", () => {
  it("renders <Text> content", () => {
    const p = pools();
    const s = render(<Text>hello</Text>, { width: 10, pools: p });
    expect(read(s, p)).toEqual(["hello"]);
  });

  it("renders <Box> with multiple <Text> children stacked", () => {
    const p = pools();
    const s = render(
      <Box>
        <Text>one</Text>
        <Text>two</Text>
      </Box>,
      { width: 10, pools: p },
    );
    expect(read(s, p)).toEqual(["one", "two"]);
  });

  it("supports nested boxes", () => {
    const p = pools();
    const s = render(
      <Box>
        <Text>a</Text>
        <Box>
          <Text>b</Text>
          <Text>c</Text>
        </Box>
        <Text>d</Text>
      </Box>,
      { width: 5, pools: p },
    );
    expect(read(s, p)).toEqual(["a", "b", "c", "d"]);
  });
});

describe("render — text content forms", () => {
  it("string + number children concatenate", () => {
    const p = pools();
    const s = render(<Text>{`count: ${3}`}</Text>, { width: 20, pools: p });
    expect(read(s, p)).toEqual(["count: 3"]);
  });

  it("mixed array children of <Text> concatenate", () => {
    const p = pools();
    const s = render(
      <Text>
        {"a"}
        {"b"}
        {"c"}
      </Text>,
      { width: 5, pools: p },
    );
    expect(read(s, p)).toEqual(["abc"]);
  });

  it("null / undefined / false children are skipped", () => {
    const p = pools();
    const s = render(
      <Box>
        <Text>x</Text>
        {null}
        {undefined}
        {false}
        <Text>y</Text>
      </Box>,
      { width: 5, pools: p },
    );
    expect(read(s, p)).toEqual(["x", "y"]);
  });
});

describe("render — composition", () => {
  function Hi() {
    return <Text>hi</Text>;
  }
  function Group() {
    return (
      <Box>
        <Hi />
        <Text>there</Text>
      </Box>
    );
  }

  it("calls a stateless function component and uses its returned tree", () => {
    const p = pools();
    const s = render(<Hi />, { width: 5, pools: p });
    expect(read(s, p)).toEqual(["hi"]);
  });

  it("nested function components compose", () => {
    const p = pools();
    const s = render(<Group />, { width: 5, pools: p });
    expect(read(s, p)).toEqual(["hi", "there"]);
  });

  it("React.Fragment passes through to children", () => {
    const p = pools();
    const s = render(
      <Box>
        <>
          <Text>a</Text>
          <Text>b</Text>
        </>
      </Box>,
      { width: 5, pools: p },
    );
    expect(read(s, p)).toEqual(["a", "b"]);
  });

  it("Fragment with one child unwraps directly", () => {
    const p = pools();
    const s = render(
      <Fragment>
        <Text>solo</Text>
      </Fragment>,
      { width: 10, pools: p },
    );
    expect(read(s, p)).toEqual(["solo"]);
  });
});

describe("render — style + hyperlink threading", () => {
  it("style prop on <Text> propagates to its cells", () => {
    const p = pools();
    const s = render(<Text style={[RED]}>x</Text>, { width: 5, pools: p });
    const cell = s.cellAt(0, 0)!;
    expect(p.style.transition(p.style.none, cell.styleId)).toBe("\x1b[31m");
  });

  it("hyperlink prop on <Text> is interned and applied", () => {
    const p = pools();
    const s = render(<Text hyperlink="https://example.com">link</Text>, { width: 10, pools: p });
    expect(p.hyperlink.get(s.cellAt(0, 0)!.hyperlinkId)).toBe("https://example.com");
  });
});

describe("render — direct host invocation throws", () => {
  it("calling Box() directly throws (host marker, not real component)", () => {
    expect(() => Box({})).toThrow(/host element/i);
  });

  it("calling Text() directly throws", () => {
    expect(() => Text({})).toThrow(/host element/i);
  });
});
