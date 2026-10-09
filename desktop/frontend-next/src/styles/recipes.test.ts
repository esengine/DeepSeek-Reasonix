import { describe, expect, it } from "vitest";

const TOKENS = import.meta.glob("./tokens.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const tokens = Object.values(TOKENS)[0];

// What a pack's recipes step. Each of these keeps its own value as the factor's
// default, so a window with no pack on it is laid out at exactly the numbers
// this file writes.
const SCALED = [
  ["--r-xs", "r"],
  ["--r-sm", "r"],
  ["--r-md", "r"],
  ["--r-lg", "r"],
  ["--pane-gutter", "d"],
  ["--call-gap", "d"],
  ["--call-sym", "d"],
  ["--close-h", "d"],
] as const;

describe("the shape and rhythm scales", () => {
  it("takes every stepped declaration from its own recipe factor", () => {
    for (const [name, kind] of SCALED) {
      expect(tokens, name).toMatch(new RegExp(`${name}:\\s*calc\\([^)]*var\\(--${kind}-scale, 1\\)`));
    }
  });

  // A pill is a shape rather than a size, so no recipe may round it into
  // something else — the same reason a pack has no token for it.
  it("leaves the pill out of the shape scale", () => {
    expect(tokens).toMatch(/--r-pill:\s*999px/);
  });

  it("steps no type and no layout width", () => {
    for (const name of ["--fs-ui", "--fs-meta", "--read", "--rail-open", "--side-open", "--dock-open", "--column"]) {
      expect(tokens, name).not.toMatch(new RegExp(`${name}:\\s*calc\\(`));
    }
  });
});
