import { describe, expect, it } from "vitest";

const CSS = import.meta.glob("./*.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

const MIN_LINE_HEIGHT = 1.2;

interface Rule {
  file: string;
  selector: string;
  body: string;
}

function rules(): Rule[] {
  const out: Rule[] = [];
  for (const [file, css] of Object.entries(CSS)) {
    for (const m of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) out.push({ file, selector: m[1].trim(), body: m[2] });
  }
  return out;
}

function lineHeight(body: string): number | undefined {
  const longhand = body.match(/(?:^|[;\s])line-height\s*:\s*([\d.]+)\s*(?:;|$)/);
  if (longhand) return Number(longhand[1]);
  const shorthand = body.match(/(?:^|[;\s])font\s*:[^;]*?\/\s*([\d.]+)(?=\s|;|$)/);
  return shorthand ? Number(shorthand[1]) : undefined;
}

const clips = (body: string) => /overflow(?:-x)?\s*:\s*hidden|text-overflow\s*:/.test(body);

describe("a clipped single line keeps room for the glyphs of any UI font", () => {
  it("never pairs overflow clipping with a unitless line-height below 1.2", () => {
    const tight = rules()
      .filter((r) => clips(r.body))
      .filter((r) => (lineHeight(r.body) ?? Infinity) < MIN_LINE_HEIGHT)
      .map((r) => `${r.file} ${r.selector}`);
    expect(tight).toEqual([]);
  });

  it("gives the composer's model name its own line-height, since it inherits a tight one from the picker", () => {
    const names = rules().filter((r) => /\.model-picker \.nm$/.test(r.selector) && clips(r.body));
    expect(names.length).toBeGreaterThan(0);
    const bare = names.filter((r) => (lineHeight(r.body) ?? 0) < MIN_LINE_HEIGHT).map((r) => `${r.file} ${r.selector}`);
    expect(bare).toEqual([]);
  });
});
