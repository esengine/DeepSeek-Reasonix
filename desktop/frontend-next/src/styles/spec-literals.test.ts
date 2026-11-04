import { describe, expect, it } from "vitest";

const SHEETS = import.meta.glob(["./*.css", "../design/*.css"], { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const line = (css: string, at: number) => css.slice(0, at).split("\n").length;

describe("a stylesheet outside the token file", () => {
  it("draws every corner from the four radius tokens, the pill or a hairline", () => {
    const bare: string[] = [];
    for (const [name, css] of Object.entries(SHEETS)) {
      if (name === "./tokens.css") continue;
      for (const m of css.matchAll(/border(?:-[a-z]+)*-radius\s*:\s*([^;}]+)/g)) {
        if (/var\(/.test(m[1])) continue;
        for (const n of m[1].matchAll(/(?<![\w.-])([0-9.]+)px/g)) {
          const px = Number(n[1]);
          if (px >= 3 && px < 999) bare.push(`${name}:${line(css, m.index!)} ${m[0].trim().slice(0, 50)}`);
        }
      }
    }
    expect(bare).toEqual([]);
  });

  it("times a transition from --t-line, --t-card or --t-panel, never a bare 100–150ms", () => {
    const bare: string[] = [];
    for (const [name, css] of Object.entries(SHEETS)) {
      if (name === "./tokens.css") continue;
      for (const m of css.matchAll(/(?<![\w-])(?:transition|animation)(?:-duration)?\s*:\s*([^;}]+)/g)) {
        for (const n of m[1].matchAll(/(?<![\w.-])(1[0-5][0-9])ms\b/g)) bare.push(`${name}:${line(css, m.index!)} ${n[0]}`);
      }
    }
    expect(bare).toEqual([]);
  });

  it("sets type from the six size tokens, never a bare calc of 7–23px", () => {
    const bare: string[] = [];
    for (const [name, css] of Object.entries(SHEETS)) {
      if (name === "./tokens.css") continue;
      for (const m of css.matchAll(/(?<![-\w])(?:font-size|font)\s*:\s*[^;}]*/g)) {
        for (const n of m[0].matchAll(/calc\(([0-9.]+)px \* var\(--fs-scale, 1\)\)/g)) {
          const px = Number(n[1]);
          if (px >= 7 && px < 24 && px !== 16) bare.push(`${name}:${line(css, m.index!)} ${n[0]}`);
        }
      }
    }
    expect(bare).toEqual([]);
  });

  it("takes weights from --w-reg, --w-med or --w-semi, never a bare number", () => {
    const bare: string[] = [];
    for (const [name, css] of Object.entries(SHEETS)) {
      if (name === "./tokens.css") continue;
      const outside = css.replace(/@font-face\s*\{[^}]*\}/g, (face) => face.replace(/[^\n]/g, " "));
      for (const m of outside.matchAll(/(?<![-\w])font-weight\s*:\s*([0-9]{3})(?![0-9.])|(?<![-\w])font\s*:\s*([0-9]{3}) /g)) bare.push(`${name}:${line(outside, m.index!)} ${m[0].trim()}`);
    }
    expect(bare).toEqual([]);
  });
});
