import { describe, expect, it } from "vitest";

const SHEETS = import.meta.glob("./*.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const KIT = SHEETS["./kit.css"];

function rules(css: string): { selector: string; body: string }[] {
  const out: { selector: string; body: string }[] = [];
  const re = /([^{}]+)\{([^{}]*)\}/g;
  for (let m = re.exec(css.replace(/\/\*[\s\S]*?\*\//g, "")); m; m = re.exec(css)) {
    out.push({ selector: m[1].trim(), body: m[2] });
  }
  return out;
}

describe("single-choice drop-downs", () => {
  it("draw one arrow, set in from the right border, with room for it beside the text", () => {
    const base = rules(KIT).find((r) => r.selector === "select:not([multiple])")?.body ?? "";
    expect(base, "kit.css has no rule for drop-downs").not.toBe("");
    expect(base).toMatch(/appearance:\s*none/);
    expect(base).toMatch(/no-repeat\s+right\s+10px\s+center\s*\/\s*12px\s+12px/);
    expect(base).toMatch(/height:\s*32px/);
    expect(base).toMatch(/border-radius:\s*var\(--r-sm\)/);
    const right = Number(base.match(/padding:\s*0\s+(\d+)px\s+0\s+\d+px/)?.[1] ?? 0);
    expect(right).toBeGreaterThanOrEqual(10 + 12 + 6);
    const small = rules(KIT).find((r) => r.selector === 'select[data-size="sm"]')?.body ?? "";
    expect(small).toMatch(/height:\s*24px/);
  });

  it("take their size, corners and arrow from kit.css alone", () => {
    const owned = /(^|[;\s])(height|min-height|padding|border-radius|appearance|background(-image|-position)?|font-size)\s*:/;
    const offenders: string[] = [];
    for (const [file, css] of Object.entries(SHEETS)) {
      if (file === "./kit.css") continue;
      for (const r of rules(css)) {
        const targets = r.selector.split(",").map((s) => s.trim()).filter((s) => /(^|[\s>+~])select(?![-\w])/.test(s));
        if (targets.length && owned.test(r.body)) offenders.push(`${file} ${targets.join(", ")}`);
      }
    }
    expect(offenders).toEqual([]);
  });
});
