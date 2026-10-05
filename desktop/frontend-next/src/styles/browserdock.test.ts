import { describe, expect, it } from "vitest";

const CSS = import.meta.glob("./app.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

// A docked panel reaches the bottom of the window only as a child of the pane:
// inside the body it stops at the composer's top edge and leaves the rest of
// that row empty. Both halves are read here because either one alone is a panel
// that stops short — the grid, and the column that spans the two rows.
describe("a docked browser panel spans the composer's row", () => {
  const css = Object.values(CSS)[0];

  it("turns the pane into a grid while the pages are docked", () => {
    expect(css, "no docked-pane grid rule found; this file matches nothing")
      .toMatch(/\.pane:has\(> \.pbody\[data-dock\]\)\s*\{[^}]*display:\s*grid/);
  });

  it("gives the panel the column spanning the body's row and the composer's", () => {
    expect(css).toMatch(/grid-template:[^;]*"body side"[^;]*"compose side"/);
    expect(css).toMatch(/\.pane:has\(> \.pbody\[data-dock\]\)\s*>\s*\.scroll\[data-pane="browser"\]\s*\{[^}]*grid-area:\s*side/);
  });

  it("hides the conversation column rather than the page when the pages go full width", () => {
    expect(css).toMatch(/\.pbody\[data-full\]\s*\{\s*display:\s*none/);
  });
});
