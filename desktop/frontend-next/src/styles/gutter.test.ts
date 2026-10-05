import { describe, expect, it } from "vitest";

const CSS = import.meta.glob("./app.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

const SEMANTIC = ["--net", "--deleg", "--warn", "--accent", "--ok", "--err"];

// host draws its line as a dash rather than a hue: the gutter's colour says
// which category, its texture says who authored the step.
const TEXTURE_ONLY = new Set(["host"]);

type Part = "sym" | "line";

function huesByKind(css: string, part: Part): Map<string, string[]> {
  const out = new Map<string, string[]>();
  const rule = new RegExp(String.raw`\.call\[data-k="([\w-]+)"\][^{]*\.g\s+\.${part}\s*\{([^}]*)\}`, "g");
  for (const m of css.matchAll(rule)) {
    const found = SEMANTIC.filter((token) => m[2].includes(`var(${token})`) || m[2].includes(`var(${token}-`));
    if (found.length) out.set(m[1], found);
  }
  return out;
}

describe("the gutter names one category with one hue", () => {
  const css = Object.values(CSS)[0];
  const syms = huesByKind(css, "sym");
  const lines = huesByKind(css, "line");

  it("reads both halves of the gutter", () => {
    expect(syms.size, "no data-k sym rules found; this file matches nothing").toBeGreaterThan(3);
    expect(lines.size, "no data-k line rules found").toBeGreaterThan(3);
  });

  it("gives every categorised mark a line of the same hue", () => {
    const split: string[] = [];
    for (const [kind, hues] of syms) {
      if (TEXTURE_ONLY.has(kind)) continue;
      const line = lines.get(kind);
      if (!line) {
        split.push(`[data-k="${kind}"] mark is ${hues.join("/")} and its line has no hue`);
        continue;
      }
      const base = (h: string) => h.replace(/-(ink|wash)$/, "");
      if (base(hues[0]) !== base(line[0])) {
        split.push(`[data-k="${kind}"] mark is ${hues[0]} and its line is ${line[0]}`);
      }
    }
    expect(split, "the mark says one category and the line says another").toEqual([]);
  });

  it("keeps every texture-only exception in use", () => {
    const stale = [...TEXTURE_ONLY].filter((kind) => !syms.has(kind));
    expect(stale, "listed as texture-only but no longer draws a categorised mark").toEqual([]);
  });
});
