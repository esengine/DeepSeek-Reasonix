import { describe, expect, it } from "vitest";

const SHEETS = import.meta.glob("./*.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

function veilDeclarations(css: string): string[] {
  const out: string[] = [];
  for (const m of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (/\.fbk-veil(?![\w-])/.test(m[1])) out.push(m[2]);
  }
  return out;
}

function violations(decls: string[]): string[] {
  const bad: string[] = [];
  for (const d of decls) {
    if (/backdrop-filter\s*:\s*(?!none)/.test(d)) bad.push("backdrop-filter");
    if (/animation(?:-iteration-count)?\s*:[^;]*\binfinite\b/.test(d)) bad.push("infinite animation");
  }
  return bad;
}

describe("feedback veil", () => {
  it("is declared somewhere and is a flat scrim", () => {
    const all = Object.values(SHEETS).flatMap(veilDeclarations);
    expect(all.length).toBeGreaterThan(0);
    expect(all.join(";")).toMatch(/background:\s*color-mix\(in srgb, var\(--page\)/);
  });

  it("no stylesheet gives the veil a backdrop filter or an endless animation", () => {
    for (const [file, css] of Object.entries(SHEETS)) {
      expect(violations(veilDeclarations(css)), file).toEqual([]);
    }
  });

  it("the guard fails on offending rules", () => {
    expect(violations(veilDeclarations(".fbk-veil { backdrop-filter: blur(4px) }"))).toEqual(["backdrop-filter"]);
    expect(violations(veilDeclarations("x .fbk-veil { -webkit-backdrop-filter: blur(4px) }"))).toEqual(["backdrop-filter"]);
    expect(violations(veilDeclarations(".fbk-veil { animation: pulse 2s ease infinite }"))).toEqual(["infinite animation"]);
    expect(violations(veilDeclarations(".fbk-veil { animation-iteration-count: infinite }"))).toEqual(["infinite animation"]);
    expect(violations(veilDeclarations(".fbk-veil-x { backdrop-filter: blur(4px) }"))).toEqual([]);
  });
});
