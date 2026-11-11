import { describe, expect, it } from "vitest";
import app from "./app.css?raw";

const rule = (selector: string): string => {
  const at = app.indexOf(selector + " {");
  return at < 0 ? "" : app.slice(at, app.indexOf("}", at));
};

const em = (value: string): number => (value.endsWith("em") ? parseFloat(value) : NaN);

describe("a display formula in a message", () => {
  const block = rule(".md .katex-display");

  it("scrolls sideways when it is too wide", () => {
    expect(block).toMatch(/overflow-x:\s*auto/);
  });

  it("leaves room above and below for what a raised exponent or a tall delimiter draws past the line", () => {
    expect(block).toMatch(/overflow-y:\s*hidden/);
    const pad = block.match(/padding:\s*([\d.]+em)\s+0/);
    expect(pad, "vertical padding in em on both sides").not.toBeNull();
    expect(em(pad![1])).toBeGreaterThanOrEqual(0.55);
  });

  it("is not given a bottom-only allowance that leaves the top flush with the clip edge", () => {
    expect(block).not.toMatch(/padding-bottom/);
  });
});
