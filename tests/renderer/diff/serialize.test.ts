import { describe, expect, it } from "vitest";
import type { Patch } from "../../../src/renderer/diff/patch.js";
import { serializePatches } from "../../../src/renderer/diff/serialize.js";

const ESC = "\x1b";
const CSI = `${ESC}[`;
const ST = `${ESC}\\`;

describe("serializePatches — stdout / control", () => {
  it("stdout writes content verbatim", () => {
    expect(serializePatches([{ type: "stdout", content: "hello" }])).toBe("hello");
  });

  it("carriageReturn emits \\r", () => {
    expect(serializePatches([{ type: "carriageReturn" }])).toBe("\r");
  });

  it("styleStr writes the precomputed string verbatim", () => {
    expect(serializePatches([{ type: "styleStr", str: `${CSI}31m` }])).toBe(`${CSI}31m`);
  });

  it("a sequence of patches concatenates in order", () => {
    const seq: Patch[] = [
      { type: "stdout", content: "a" },
      { type: "carriageReturn" },
      { type: "stdout", content: "b" },
    ];
    expect(serializePatches(seq)).toBe("a\rb");
  });
});

describe("serializePatches — cursorMove", () => {
  it("dx > 0 emits CSI <n> C", () => {
    expect(serializePatches([{ type: "cursorMove", dx: 3, dy: 0 }])).toBe(`${CSI}3C`);
  });

  it("dx < 0 emits CSI <n> D", () => {
    expect(serializePatches([{ type: "cursorMove", dx: -2, dy: 0 }])).toBe(`${CSI}2D`);
  });

  it("dy > 0 emits CSI <n> B", () => {
    expect(serializePatches([{ type: "cursorMove", dx: 0, dy: 4 }])).toBe(`${CSI}4B`);
  });

  it("dy < 0 emits CSI <n> A", () => {
    expect(serializePatches([{ type: "cursorMove", dx: 0, dy: -1 }])).toBe(`${CSI}1A`);
  });

  it("dx and dy both 0 emits nothing", () => {
    expect(serializePatches([{ type: "cursorMove", dx: 0, dy: 0 }])).toBe("");
  });

  it("dx and dy both nonzero emits vertical first, then horizontal", () => {
    expect(serializePatches([{ type: "cursorMove", dx: 2, dy: -1 }])).toBe(`${CSI}1A${CSI}2C`);
  });
});

describe("serializePatches — cursorTo", () => {
  it("emits CSI <col+1> G (CHA is 1-based)", () => {
    expect(serializePatches([{ type: "cursorTo", col: 0 }])).toBe(`${CSI}1G`);
    expect(serializePatches([{ type: "cursorTo", col: 9 }])).toBe(`${CSI}10G`);
  });
});

describe("serializePatches — hyperlink (OSC 8)", () => {
  it("opens with the URI between the marker and ST", () => {
    expect(serializePatches([{ type: "hyperlink", uri: "https://example.com" }])).toBe(
      `${ESC}]8;;https://example.com${ST}`,
    );
  });

  it("closes with an empty URI", () => {
    expect(serializePatches([{ type: "hyperlink", uri: "" }])).toBe(`${ESC}]8;;${ST}`);
  });
});

describe("serializePatches — clear", () => {
  it("count = 0 emits \\r + erase-from-cursor-down (CSI J)", () => {
    expect(serializePatches([{ type: "clear", count: 0 }])).toBe(`\r${CSI}J`);
  });

  it("count > 0 moves cursor up that many rows then erases to end of screen", () => {
    expect(serializePatches([{ type: "clear", count: 3 }])).toBe(`\r${CSI}3A${CSI}J`);
  });
});

describe("serializePatches — clearTerminal", () => {
  it("emits CSI 2J + CSI H (erase entire display + cursor home)", () => {
    expect(serializePatches([{ type: "clearTerminal" }])).toBe(`${CSI}2J${CSI}H`);
  });
});

describe("serializePatches — realistic frame", () => {
  it("paints a single styled cell at (3, 1) and resets style at the end", () => {
    const seq: Patch[] = [
      { type: "carriageReturn" },
      { type: "cursorMove", dx: 0, dy: 1 },
      { type: "cursorMove", dx: 3, dy: 0 },
      { type: "styleStr", str: `${CSI}31m` },
      { type: "stdout", content: "x" },
      { type: "styleStr", str: `${CSI}39m` },
    ];
    const expected = `\r${CSI}1B${CSI}3C${CSI}31mx${CSI}39m`;
    expect(serializePatches(seq)).toBe(expected);
  });

  it("empty patch list emits nothing", () => {
    expect(serializePatches([])).toBe("");
  });
});
