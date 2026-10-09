import { describe, expect, it } from "vitest";

const SHEETS = import.meta.glob(["./app.css", "./studio.css"], { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const css = Object.values(SHEETS).join("\n");
const HOSTS = ["studio-attach", "studio-refine"];

interface Rule {
  selector: string;
  decls: Map<string, string>;
}

function rules(): Rule[] {
  const out: Rule[] = [];
  for (const m of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const decls = new Map<string, string>();
    for (const part of m[2].split(";")) {
      const at = part.indexOf(":");
      if (at > 0) decls.set(part.slice(0, at).trim(), part.slice(at + 1).trim());
    }
    for (const selector of m[1].split(",")) out.push({ selector: selector.trim(), decls });
  }
  return out;
}

const subject = (selector: string) => selector.split(/[\s>+~]+/).pop() ?? "";

describe("the shared control tooltip", () => {
  const base = rules().filter((r) => r.selector === ".studio-control-tip");

  it("wraps inside its max-width instead of inheriting the host button's nowrap", () => {
    expect(base.some((r) => r.decls.get("max-width"))).toBe(true);
    expect(base.some((r) => r.decls.get("white-space") === "normal")).toBe(true);
    expect(base.some((r) => r.decls.get("overflow-wrap") === "anywhere")).toBe(true);
  });

  it("never sits under a dimmed host: a disabled host dims its glyph, not itself", () => {
    const dimmed = rules().filter(
      (r) => HOSTS.some((h) => subject(r.selector).includes(`.${h}`)) && /disabled/.test(subject(r.selector)) && r.decls.has("opacity"),
    );
    expect(dimmed.map((r) => r.selector)).toEqual([]);
  });

  it("keeps a disabled refine button's glyph dimmed", () => {
    const glyph = rules().find((r) => r.selector === ".studio-refine[disabled] > .studio-icon");
    expect(glyph?.decls.get("opacity")).toBe(".35");
  });
});
