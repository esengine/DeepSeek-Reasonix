import { describe, expect, it } from "vitest";
import css from "./notices.css?raw";

const rule = (selector: string) => {
  const at = css.indexOf(`${selector} {`);
  return at < 0 ? "" : css.slice(css.indexOf("{", at) + 1, css.indexOf("}", at));
};

describe("the notice strip follows the board", () => {
  it("is a 380px card on the float surface with the board's padding and radius", () => {
    const strip = rule(".errbar:not([data-cause])");
    expect(strip).toMatch(/width:\s*380px/);
    expect(strip).toMatch(/padding:\s*10px 10px 10px 12px/);
    expect(strip).toMatch(/border-radius:\s*var\(--r-md\)/);
    expect(strip).toMatch(/background:\s*var\(--float\)/);
  });

  it("draws its actions as 24px ghost pills, not underlined text", () => {
    const button = rule(".errbar:not([data-cause]) > button");
    expect(button).toMatch(/height:\s*24px/);
    expect(button).toMatch(/border-radius:\s*var\(--r-pill\)/);
    expect(button).toMatch(/text-decoration:\s*none/);
  });

  it("stacks above the composer, right-aligned, and keeps the newest three", () => {
    const stack = rule(".compose > .composenotes");
    expect(stack).toMatch(/bottom:\s*calc\(100% \+ 8px\)/);
    expect(stack).toMatch(/align-items:\s*flex-end/);
    expect(rule(".compose > .composenotes > :nth-last-child(n+4)")).toMatch(/opacity:\s*0/);
    expect(rule(".compose > .composenotes > :nth-last-child(n+4)")).toMatch(/max-height:\s*0/);
    expect(rule(".compose > .composenotes > *")).toMatch(/transition:/);
  });

  it("gives the toast row the board's card, its three action levels and the update's primary button", () => {
    expect(rule(".toast")).toMatch(/width:\s*380px/);
    expect(rule(".toast")).toMatch(/background:\s*var\(--float\)/);
    expect(rule(".toast-actions .act")).toMatch(/height:\s*32px/);
    expect(rule(".toast-actions .act.ghost")).toMatch(/height:\s*24px/);
    expect(rule(".toast-actions .act[data-primary]")).toMatch(/background:\s*var\(--accent\)/);
  });

  it("draws the leave dialog at 380 with its footer rule, and the settings placeholder at 440", () => {
    expect(rule(".preset-confirm")).toMatch(/width:\s*380px/);
    expect(rule(".preset-confirm .preset-confirm-actions")).toMatch(/border-top:\s*1px solid var\(--hair\)/);
    expect(rule(".prefs-sheet.gone")).toMatch(/width:\s*440px/);
  });
});
