import { describe, expect, it } from "vitest";

const SHEET = Object.values(
  import.meta.glob("./studio.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>,
)[0];

interface Rule {
  selector: string;
  decls: Map<string, string>;
}

function rules(css: string): Rule[] {
  const out: Rule[] = [];
  for (const m of css.replace(/\/\*[\s\S]*?\*\//g, "").matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const decls = new Map<string, string>();
    for (const part of m[2].split(";")) {
      const at = part.indexOf(":");
      if (at > 0) decls.set(part.slice(0, at).trim(), part.slice(at + 1).trim());
    }
    out.push({ selector: m[1].trim(), decls });
  }
  return out;
}

describe("status rail figures hold their width", () => {
  const rail = rules(SHEET).filter((r) => r.selector.split(",").some((s) => s.includes(".studio-meterrail")));

  it("finds the rail rules", () => {
    expect(rail.length).toBeGreaterThan(0);
  });

  it("keeps tabular numerals on every rule that sets the font shorthand", () => {
    const reset = rail
      .filter((r) => r.decls.has("font") && r.decls.get("font-variant-numeric") !== "tabular-nums")
      .map((r) => r.selector.replace(/\s+/g, " "));
    expect(reset, "the font shorthand resets font-variant-numeric; restate tabular-nums in the same rule").toEqual([]);
  });

  it("reserves the width of a three-digit speed so 9.9 to 10.0 and 99.9 to 100.0 do not move its neighbours", () => {
    const speed = rail.find((r) => r.selector === ".studio-meterrail .studio-meter-speed b");
    expect(speed?.decls.get("min-width")).toBe("5ch");
  });
});
