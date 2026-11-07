import { describe, expect, it } from "vitest";
import menu from "./phone-menu.css?raw";
import index from "./design.css?raw";

const S = '[data-fold~="phone"] .composer-menu.studio-model-menu.menu-sheet';

const rule = (selector: string): string => {
  const at = menu.indexOf(selector + " {");
  return at < 0 ? "" : menu.slice(at, menu.indexOf("}", at));
};

describe("the model menu as a sheet on a phone", () => {
  it("is part of the design layer, after the phone rules", () => {
    expect(index).toMatch(/@import "\.\/phone\.css" layer\(design\)[\s\S]*@import "\.\/phone-menu\.css" layer\(design\)/);
  });

  it("is a panel along the bottom edge with rounded top corners and the board's padding", () => {
    const sheet = rule(S);
    expect(sheet).toMatch(/position:\s*fixed/);
    expect(sheet).toMatch(/inset:\s*auto 0 0 0/);
    expect(sheet).toMatch(/border-radius:\s*15px 15px 0 0/);
    expect(sheet).toMatch(/padding:\s*8px 10px 18px/);
    expect(sheet).toMatch(/background:\s*var\(--surface\)/);
    expect(sheet).toMatch(/overflow:\s*hidden/);
  });

  it("dims the page behind it with the board's 30% black, without a second element", () => {
    expect(rule(S)).toMatch(/box-shadow:\s*var\(--shadow-float\),\s*0 0 0 100vmax rgb\(0 0 0 \/ \.3\)/);
  });

  it("has the grab handle at the top, 36 by 4", () => {
    const handle = rule(S + "::before");
    expect(handle).toMatch(/width:\s*36px/);
    expect(handle).toMatch(/height:\s*4px/);
    expect(handle).toMatch(/background:\s*var\(--border\)/);
    expect(handle).toMatch(/margin:\s*0 auto 10px/);
  });

  it("stacks the model rows and the effort column instead of putting them side by side", () => {
    expect(rule(S + ".has-aside")).toMatch(/display:\s*flex/);
    expect(rule(S + ".has-aside")).toMatch(/flex-direction:\s*column/);
  });

  it("keeps every block its own height, so the effort row is not squeezed when the list is long", () => {
    expect(rule(S + " > *")).toMatch(/flex:\s*none/);
  });

  it("scrolls the model list by itself, so the effort row stays in view under a long list", () => {
    const list = rule(S + " > .menu-rows");
    expect(list).toMatch(/flex:\s*1 1 auto/);
    expect(list).toMatch(/min-height:\s*0/);
    expect(list).toMatch(/overflow-y:\s*auto/);
  });

  it("draws model rows 48px tall with the name in the monospace face", () => {
    expect(rule(S + " button.mi")).toMatch(/min-height:\s*48px/);
    expect(rule(S + " button.mi")).toMatch(/border-radius:\s*10px/);
    expect(rule(S + " button.mi .lb")).toMatch(/font:\s*var\(--w-med\) var\(--fs-lead\)\/1\.3 var\(--mono\)/);
  });

  it("lays the effort levels out as one row of 36px chips, selected one raised", () => {
    const column = rule(S + " .menu-aside");
    expect(column).toMatch(/display:\s*flex/);
    expect(column).toMatch(/flex-direction:\s*row/);
    expect(column).toMatch(/flex-wrap:\s*wrap/);
    const chip = rule(S + " .menu-aside > button.mi[data-strength]");
    expect(chip).toMatch(/flex:\s*1 1 0/);
    expect(chip).toMatch(/display:\s*flex/);
    expect(rule(S + " .menu-aside > button.mi[data-strength] .tx")).toMatch(/text-align:\s*center/);
    expect(chip).toMatch(/height:\s*36px/);
    expect(chip).toMatch(/border-radius:\s*7px/);
    expect(chip).toMatch(/background:\s*var\(--overlay\)/);
    expect(rule(S + " .menu-aside > button.mi[data-strength][data-on]")).toMatch(/background:\s*var\(--raised\)/);
    expect(rule(S + " .menu-aside > button.mi[data-strength][data-on]")).toMatch(/box-shadow:\s*0 1px 2px rgb\(0 0 0 \/ \.1\)/);
  });

  it("keeps only the level's name in each chip, and drops the explanations the board leaves out", () => {
    const hidden = rule(S + " .menu-aside > button.mi[data-strength] :is(.ds, .studio-item-meta, .studio-item-badge, .studio-effort-strength, .dot, .rt)");
    expect(hidden).toMatch(/display:\s*none/);
    expect(rule(S + " .menu-aside > .mi.head:last-child")).toMatch(/display:\s*none/);
  });

  it("slides up from the bottom, and not at all under reduced motion", () => {
    expect(rule(S)).toMatch(/animation:\s*sheet-up var\(--m-panel\)/);
    expect(menu).toMatch(/@keyframes sheet-up\s*\{\s*from\s*\{\s*transform:\s*translateY\(100%\)/);
    expect(menu).toMatch(/prefers-reduced-motion: reduce\)[^}]*menu-sheet[^{]*\{[^}]*animation:\s*none/);
    expect(menu).toMatch(/:root\[data-effects="reduced"\][^{]*menu-sheet[^{]*\{[^}]*animation:\s*none/);
  });
});
