import { describe, expect, it } from "vitest";
import { Bar } from "../src/cli/ui/primitives.js";
import { lowerInkToScene } from "../src/cli/ui/scene/lower.js";

describe("Bar lowering", () => {
  it("lowers a 50% bar to filled + empty runs", () => {
    expect(lowerInkToScene(Bar({ ratio: 0.5, color: "green", cells: 10 }))).toEqual({
      kind: "text",
      runs: [
        { text: "▰▰▰▰▰", style: { color: "green" } },
        { text: "▱▱▱▱▱", style: { dim: true } },
      ],
    });
  });

  it("lowers a fully empty bar (ratio=0) — filled run is an empty string", () => {
    expect(lowerInkToScene(Bar({ ratio: 0, color: "red", cells: 4 }))).toEqual({
      kind: "text",
      runs: [
        { text: "", style: { color: "red" } },
        { text: "▱▱▱▱", style: { dim: true } },
      ],
    });
  });

  it("lowers a fully filled bar (ratio=1) — empty run is an empty string", () => {
    expect(lowerInkToScene(Bar({ ratio: 1, color: "blue", cells: 3 }))).toEqual({
      kind: "text",
      runs: [
        { text: "▰▰▰", style: { color: "blue" } },
        { text: "", style: { dim: true } },
      ],
    });
  });

  it("applies dim to the filled run when dim=true", () => {
    expect(lowerInkToScene(Bar({ ratio: 0.5, color: "green", cells: 2, dim: true }))).toEqual({
      kind: "text",
      runs: [
        { text: "▰", style: { color: "green", dim: true } },
        { text: "▱", style: { dim: true } },
      ],
    });
  });
});
