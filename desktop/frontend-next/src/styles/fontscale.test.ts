import { describe, expect, it } from "vitest";

const SHEETS = import.meta.glob(["./*.css", "../design/*.css"], { query: "?raw", import: "default", eager: true }) as Record<string, string>;

const STEP = /\bcalc\([0-9.]+px \* var\(--fs-scale, 1\)\)/g;
const LENGTH = /(?<![\w.-])-?[0-9]*\.?[0-9]+(px|pt)\b/;

// The text-size setting moves --fs-scale, so a size that reaches the page as a
// bare pixel count is one it can never move. Sizes ride the tokens, or a pixel
// count inside calc(<px> * var(--fs-scale, 1)); the transcript's own dial is
// --read, which a size may derive from by var().
function fontDeclarations(css: string): { at: number; text: string }[] {
  const found: { at: number; text: string }[] = [];
  for (const m of css.matchAll(/(?<![-\w])(font-size|font)\s*:\s*([^;}]*)/g)) found.push({ at: css.slice(0, m.index).split("\n").length, text: m[2] });
  return found;
}

describe("a size written into a stylesheet", () => {
  it("never reaches a font declaration as a bare pixel count", () => {
    const bare: string[] = [];
    for (const [name, css] of Object.entries(SHEETS)) {
      for (const d of fontDeclarations(css)) if (LENGTH.test(d.text.replace(STEP, ""))) bare.push(`${name}:${d.at} ${d.text.trim().slice(0, 60)}`);
    }
    expect(bare).toEqual([]);
  });

  it("never reaches one through a custom property that is only a pixel count", () => {
    const all = Object.values(SHEETS).join("\n");
    const used = new Set<string>();
    for (const d of fontDeclarations(all)) for (const m of d.text.matchAll(/var\((--[\w-]+)/g)) used.add(m[1]);
    const bare: string[] = [];
    for (const [name, css] of Object.entries(SHEETS)) {
      for (const m of css.matchAll(/(--[\w-]+)\s*:\s*([^;}]*)/g)) {
        if (used.has(m[1]) && m[1] !== "--read" && /^-?[0-9.]+px$/.test(m[2].trim())) bare.push(`${name} ${m[1]}: ${m[2].trim()}`);
      }
    }
    expect(bare).toEqual([]);
  });

  it("moves the shared tokens with the scale", () => {
    const tokens = SHEETS["./tokens.css"];
    for (const t of ["--fs-micro", "--fs-meta", "--fs-ui", "--fs-lead", "--fs-title", "--fs-display"]) {
      expect(tokens).toMatch(new RegExp(`${t}: calc\\([0-9.]+px \\* var\\(--fs-scale, 1\\)\\)`));
    }
  });
});
