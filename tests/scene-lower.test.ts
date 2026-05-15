import { Box, Text } from "ink";
import { createElement } from "react";
import { describe, expect, it } from "vitest";
import { lowerInkToScene } from "../src/cli/ui/scene/lower.js";

describe("lowerInkToScene", () => {
  it("lowers a plain Text", () => {
    expect(lowerInkToScene(createElement(Text, null, "hello"))).toEqual({
      kind: "text",
      runs: [{ text: "hello" }],
    });
  });

  it("lowers Text with color + bold", () => {
    expect(lowerInkToScene(createElement(Text, { color: "green", bold: true }, "ok"))).toEqual({
      kind: "text",
      runs: [{ text: "ok", style: { color: "green", bold: true } }],
    });
  });

  it("lowers hex color to { hex } form", () => {
    expect(lowerInkToScene(createElement(Text, { color: "#aabbcc" }, "x"))).toEqual({
      kind: "text",
      runs: [{ text: "x", style: { color: { hex: "#aabbcc" } } }],
    });
  });

  it("flattens nested Text into runs", () => {
    const el = createElement(
      Text,
      null,
      createElement(Text, { color: "red" }, "a"),
      "b",
      createElement(Text, { bold: true }, "c"),
    );
    expect(lowerInkToScene(el)).toEqual({
      kind: "text",
      runs: [
        { text: "a", style: { color: "red" } },
        { text: "b" },
        { text: "c", style: { bold: true } },
      ],
    });
  });

  it("merges parent + child Text styles, child wins on conflict", () => {
    const el = createElement(
      Text,
      { bold: true, color: "blue" },
      createElement(Text, { color: "red" }, "x"),
    );
    expect(lowerInkToScene(el)).toEqual({
      kind: "text",
      runs: [{ text: "x", style: { bold: true, color: "red" } }],
    });
  });

  it("lowers a Box with children", () => {
    const el = createElement(
      Box,
      { flexDirection: "column", paddingX: 1 },
      createElement(Text, null, "a"),
      createElement(Text, null, "b"),
    );
    expect(lowerInkToScene(el)).toEqual({
      kind: "box",
      layout: { direction: "column", paddingX: 1 },
      children: [
        { kind: "text", runs: [{ text: "a" }] },
        { kind: "text", runs: [{ text: "b" }] },
      ],
    });
  });

  it("maps Ink padding to symmetric paddingX/paddingY", () => {
    const el = createElement(Box, { padding: 2 });
    expect(lowerInkToScene(el)).toEqual({
      kind: "box",
      layout: { paddingX: 2, paddingY: 2 },
      children: [],
    });
  });

  it("maps alignItems / justifyContent vocabulary", () => {
    const el = createElement(Box, {
      alignItems: "flex-start",
      justifyContent: "space-between",
    });
    expect(lowerInkToScene(el)).toEqual({
      kind: "box",
      layout: { align: "start", justify: "between" },
      children: [],
    });
  });

  it("maps width 100% to fill", () => {
    const el = createElement(Box, { width: "100%", height: 5 });
    expect(lowerInkToScene(el)).toEqual({
      kind: "box",
      layout: { width: "fill", height: 5 },
      children: [],
    });
  });

  it("skips falsy children (null / undefined / false)", () => {
    const el = createElement(Box, null, null, undefined, false, createElement(Text, null, "x"));
    expect(lowerInkToScene(el)).toEqual({
      kind: "box",
      children: [{ kind: "text", runs: [{ text: "x" }] }],
    });
  });

  it("throws on a function component", () => {
    function MyComponent() {
      return createElement(Text, null, "x");
    }
    expect(() => lowerInkToScene(createElement(MyComponent))).toThrow(
      /only Ink Text\/Box supported/,
    );
  });

  it("throws on an unsupported Text prop", () => {
    expect(() => lowerInkToScene(createElement(Text, { fontSize: 12 } as never, "x"))).toThrow(
      /unsupported Text prop "fontSize"/,
    );
  });

  it("throws on an unsupported Box prop", () => {
    expect(() => lowerInkToScene(createElement(Box, { display: "flex" } as never))).toThrow(
      /unsupported Box prop "display"/,
    );
  });

  it("throws on a Text child that is not a string or nested Text", () => {
    const el = createElement(Text, null, createElement(Box, null) as never);
    expect(() => lowerInkToScene(el)).toThrow(/Text children must be/);
  });
});
