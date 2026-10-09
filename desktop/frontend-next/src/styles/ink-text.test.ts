import { describe, expect, it } from "vitest";

const SHEETS = import.meta.glob("./{app,studio,feedback,community,chart}.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

const GLYPHS = [
  /\.sym\b/, /\.rmark\b/, /\.mk\b/, /\.rc-tick\b/, /\.ic$/,
  /\.steps \.s\[data-done\] \.b$/, /\.comp-l \.row \.k$/,
  /\.shot \.x:hover$/, /\.ptab-x:hover$/, /\.wsdel:hover$/,
  /\.copy\[data-state="done"\](:hover)?$/, /\.code-act\[data-state="done"\]$/,
  /\.commit-files li i\[/,
];

const BARE = /(?:^|[;\s])color\s*:[^;]*var\(--(warn|ok|err)\)/;

function offenders(css: string): string[] {
  const out: string[] = [];
  for (const m of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (!BARE.test(m[2])) continue;
    const selectors = m[1].replace(/\s+/g, " ").split(/,(?![^(]*\))/).map((s) => s.trim());
    for (const s of selectors) if (!GLYPHS.some((g) => g.test(s))) out.push(s);
  }
  return out;
}

describe("semantic hues read as text through their ink token", () => {
  it("never sets a text colour to a bare --warn, --ok or --err outside a glyph", () => {
    for (const [file, css] of Object.entries(SHEETS)) {
      expect(offenders(css), `${file}: use var(--warn-ink) / --ok-ink / --err-ink for text`).toEqual([]);
    }
  });

  it("catches a bare hue used as text", () => {
    expect(offenders(".a .lb { color: var(--warn); }")).toEqual([".a .lb"]);
    expect(offenders(".a .lb { border-color: var(--warn); }")).toEqual([]);
    expect(offenders(".a .lb { color: var(--warn-ink); }")).toEqual([]);
    expect(offenders(".a .sym { color: var(--err); }")).toEqual([]);
  });
});
