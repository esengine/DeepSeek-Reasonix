import { describe, expect, it } from "vitest";

const TOKENS = import.meta.glob("./tokens.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const SHELL = import.meta.glob("./studio.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

const tokens = Object.values(TOKENS)[0];
const shell = Object.values(SHELL)[0];

// The eight steps, in order, each with the distance it names. A ninth step is a
// change to the scale, not a component.
const STEPS: [string, number][] = [
  ["--sp-1", 2],
  ["--sp-2", 4],
  ["--sp-3", 6],
  ["--sp-4", 8],
  ["--sp-5", 10],
  ["--sp-6", 12],
  ["--sp-7", 16],
  ["--sp-8", 24],
];

function block(css: string, selector: string): string {
  const at = css.indexOf(`${selector} {`);
  return at < 0 ? "" : css.slice(at, css.indexOf("}", at) + 1);
}

function literalDistances(css: string, selector: string): string[] {
  const declared = [...block(css, selector).matchAll(/(?:^|[;{]\s*)(?:padding|gap|margin)[\w-]*\s*:\s*([^;]+)/g)];
  return declared.flatMap((d) => [...d[1].matchAll(/(\d+(?:\.\d+)?)px/g)].map((px) => px[1]));
}

describe("the spacing scale", () => {
  it("declares eight steps, each at its own distance", () => {
    for (const [name, px] of STEPS) expect(tokens, name).toMatch(new RegExp(`${name}:\\s*${px}px`));
  });

  // Steps that do not widen are a list of numbers, which is what they replaced.
  it("widens as it goes", () => {
    const px = STEPS.map(([, value]) => value);
    const gaps = px.slice(1).map((value, i) => value - px[i]);
    expect(gaps).toEqual([...gaps].sort((a, b) => a - b));
    expect(new Set(gaps).size).toBeGreaterThan(1);
  });
});

describe("a migrated surface", () => {
  it("takes every distance from the scale", () => {
    for (const selector of [".activity-group", ".activity-group > summary"]) {
      expect(literalDistances(shell, selector), selector).toEqual([]);
    }
  });
});
