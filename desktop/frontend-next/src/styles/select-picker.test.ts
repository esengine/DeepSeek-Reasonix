import { describe, expect, it } from "vitest";
import kit from "./kit.css?raw";

const block = (): string => {
  const at = kit.indexOf("@supports (appearance: base-select)");
  if (at < 0) return "";
  let depth = 0;
  for (let i = kit.indexOf("{", at); i < kit.length; i++) {
    if (kit[i] === "{") depth++;
    if (kit[i] === "}" && --depth === 0) return kit.slice(at, i + 1);
  }
  return "";
};

const rule = (css: string, selector: string): string => {
  const at = css.indexOf(selector + " {");
  return at < 0 ? "" : css.slice(at, css.indexOf("}", at));
};

// The list a select opens is drawn by the engine unless it is asked for the
// styleable one; one rule here covers every select in the app.
describe("a select's popup list", () => {
  it("is only restyled where the engine can draw it, so older ones keep the native list", () => {
    expect(block()).not.toBe("");
    expect(block()).toMatch(/appearance:\s*base-select/);
  });

  it("is a floating panel built from the app's own tokens", () => {
    const panel = rule(block(), "\n  select:not([multiple])::picker(select)");
    expect(panel).toMatch(/background:\s*var\(--float\)/);
    expect(panel).toMatch(/border:\s*1px solid var\(--border\)/);
    expect(panel).toMatch(/border-radius:\s*var\(--r-md\)/);
    expect(panel).toMatch(/box-shadow:\s*var\(--shadow-float\)/);
  });

  it("draws each option in the interface font, with hover and the chosen one told apart", () => {
    const css = block();
    expect(rule(css, "select:not([multiple]) option")).toMatch(/font:\s*var\(--w-reg\) var\(--fs-ui\)/);
    expect(rule(css, "select:not([multiple]) option:is(:hover, :focus-visible)")).toMatch(/background:\s*var\(--overlay\)/);
    expect(rule(css, "select:not([multiple]) option:checked")).toMatch(/font-weight:\s*var\(--w-med\)/);
  });

  it("does not draw a second arrow next to the one the field already has", () => {
    expect(rule(block(), "select:not([multiple])::picker-icon")).toMatch(/display:\s*none/);
  });

  it("follows the small field's size", () => {
    expect(rule(block(), 'select[data-size="sm"] option')).toMatch(/font-size:\s*var\(--fs-meta\)/);
  });

  it("gives every option a finger-sized row on a touch screen", () => {
    expect(block()).toMatch(/@media \(pointer: coarse\)\s*\{[^}]*select:not\(\[multiple\]\) option\s*\{[^}]*min-height:\s*40px/);
  });

  it("keeps the chosen text centred in the box, which the engine's own layout leaves at the top", () => {
    expect(rule(block(), "select:not([multiple])")).toMatch(/align-items:\s*center/);
  });
});
