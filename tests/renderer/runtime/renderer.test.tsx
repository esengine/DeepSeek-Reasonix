// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { describe, expect, it } from "vitest";
import { CharPool } from "../../../src/renderer/pools/char-pool.js";
import { HyperlinkPool } from "../../../src/renderer/pools/hyperlink-pool.js";
import { type AnsiCode, StylePool } from "../../../src/renderer/pools/style-pool.js";
import { Box, Text } from "../../../src/renderer/react/components.js";
import { Renderer } from "../../../src/renderer/runtime/renderer.js";
import { makeTestWriter } from "../../../src/renderer/runtime/test-writer.js";

const RED: AnsiCode = { apply: "\x1b[31m", revert: "\x1b[39m" };

function pools() {
  return {
    char: new CharPool(),
    style: new StylePool(),
    hyperlink: new HyperlinkPool(),
  };
}

describe("Renderer — first paint", () => {
  it("emits the cells of the initial tree to the writer", () => {
    const w = makeTestWriter();
    const r = new Renderer({
      viewportWidth: 5,
      viewportHeight: 2,
      pools: pools(),
      write: w.write,
    });
    r.update(<Text>hi</Text>);
    expect(w.output()).toContain("hi");
  });

  it("style transitions show up as SGR sequences in the byte stream", () => {
    const w = makeTestWriter();
    const r = new Renderer({
      viewportWidth: 3,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
    });
    r.update(<Text style={[RED]}>x</Text>);
    expect(w.output()).toContain("\x1b[31m");
    expect(w.output()).toContain("\x1b[39m");
    expect(w.output()).toContain("x");
  });
});

describe("Renderer — subsequent paints", () => {
  it("rendering the same tree twice writes nothing the second time", () => {
    const w = makeTestWriter();
    const r = new Renderer({
      viewportWidth: 5,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
    });
    r.update(<Text>same</Text>);
    w.flush();
    r.update(<Text>same</Text>);
    expect(w.output()).toBe("");
  });

  it("a single-character change writes only the new char + its cursor move", () => {
    const w = makeTestWriter();
    const r = new Renderer({
      viewportWidth: 5,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
    });
    r.update(<Text>aaaa</Text>);
    w.flush();
    r.update(<Text>aaba</Text>);
    const second = w.output();
    expect(second).toContain("b");
    expect(second).not.toContain("a");
  });
});

describe("Renderer — viewport resize", () => {
  it("emits a clearTerminal sequence when viewport changes", () => {
    const w = makeTestWriter();
    const r = new Renderer({
      viewportWidth: 5,
      viewportHeight: 2,
      pools: pools(),
      write: w.write,
    });
    r.update(<Text>hi</Text>);
    w.flush();
    r.resize(10, 3);
    r.update(<Text>hi</Text>);
    expect(w.output()).toContain("\x1b[2J");
    expect(w.output()).toContain("\x1b[H");
  });
});

describe("Renderer — destroy", () => {
  it("emits SGR reset and OSC 8 close on destroy", () => {
    const w = makeTestWriter();
    const r = new Renderer({
      viewportWidth: 5,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
    });
    r.update(<Text>x</Text>);
    w.flush();
    r.destroy();
    const out = w.output();
    expect(out).toContain("\x1b[0m");
    expect(out).toContain("\x1b]8;;\x1b\\");
  });

  it("subsequent updates after destroy are no-ops", () => {
    const w = makeTestWriter();
    const r = new Renderer({
      viewportWidth: 5,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
    });
    r.destroy();
    w.flush();
    r.update(<Text>ignored</Text>);
    expect(w.output()).toBe("");
  });
});

describe("Renderer — hyperlinks", () => {
  it("OSC 8 open + close show up around linked content", () => {
    const w = makeTestWriter();
    const r = new Renderer({
      viewportWidth: 10,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
    });
    r.update(<Text hyperlink="https://example.com">hi</Text>);
    expect(w.output()).toContain("\x1b]8;;https://example.com");
  });
});

describe("Renderer — Box composition", () => {
  it("paints a row Box with two Text children side-by-side", () => {
    const w = makeTestWriter();
    const r = new Renderer({
      viewportWidth: 10,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
    });
    r.update(
      <Box flexDirection="row">
        <Text>L</Text>
        <Text>R</Text>
      </Box>,
    );
    expect(w.output()).toContain("L");
    expect(w.output()).toContain("R");
  });
});
