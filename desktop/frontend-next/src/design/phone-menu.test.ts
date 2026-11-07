import { describe, expect, it } from "vitest";
import menu from "./phone-menu.css?raw";
const css = menu;
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

  it("keeps model rows compact: 36px tall, tight padding, a 13px monospace name", () => {
    const row = rule(S + " button.mi");
    expect(row).toMatch(/min-height:\s*36px/);
    expect(row).toMatch(/padding:\s*0 10px/);
    expect(row).toMatch(/border-radius:\s*8px/);
    expect(rule(S + " button.mi .lb")).toMatch(/font:\s*var\(--w-med\) var\(--fs-ui\)\/1\.3 var\(--mono\)/);
    expect(rule(S + " :is(.vendor-mark, .mi-mono)")).toMatch(/width:\s*16px/);
    expect(rule(S + " .menu-rows .mi.head")).toMatch(/min-height:\s*26px/);
  });

  it("keeps the effort column a full-width block holding the slider", () => {
    const column = rule(S + " .menu-aside");
    expect(column).toMatch(/flex-direction:\s*row/);
    expect(column).toMatch(/flex-wrap:\s*wrap/);
    expect(rule(S + " .menu-aside > .effort-slider")).toMatch(/flex:\s*0 0 100%/);
  });

  it("slides up from the bottom, and not at all under reduced motion", () => {
    expect(rule(S)).toMatch(/animation:\s*sheet-up var\(--m-panel\)/);
    expect(menu).toMatch(/@keyframes sheet-up\s*\{\s*from\s*\{\s*transform:\s*translateY\(100%\)/);
    expect(menu).toMatch(/prefers-reduced-motion: reduce\)[^}]*menu-sheet[^{]*\{[^}]*animation:\s*none/);
    expect(menu).toMatch(/:root\[data-effects="reduced"\][^{]*menu-sheet[^{]*\{[^}]*animation:\s*none/);
  });
});

describe("the reasoning slider", () => {
  const track = (selector: string): string => rule(selector);

  it("is a card with the level named large above a thick rounded track", () => {
    expect(track("\n.effort-slider")).toMatch(/background:\s*var\(--overlay\)/);
    expect(track("\n.effort-slider")).toMatch(/border-radius:\s*14px/);
    expect(track(".effort-slider .es-name")).toMatch(/font:\s*var\(--w-semi\) 18px/);
    const input = track('.effort-slider input[type="range"]');
    expect(input).toMatch(/height:\s*30px/);
    expect(input).toMatch(/border-radius:\s*15px/);
  });

  it("fills with a gradient up to the thumb, in a layer of its own that the input sits over", () => {
    expect(track("\n.effort-slider")).toMatch(/--track:\s*linear-gradient\(90deg/);
    const fill = track(".effort-slider .es-fill");
    expect(fill).toMatch(/width:\s*calc\(var\(--p\) \* \(100% - 30px\) \+ 15px\)/);
    expect(fill).toMatch(/background:\s*var\(--track\)/);
    expect(fill).toMatch(/overflow:\s*hidden/);
    expect(track('.effort-slider input[type="range"]')).toMatch(/background:\s*transparent/);
    expect(track(".effort-slider .es-name")).toMatch(/background-clip:\s*text/);
  });

  it("is lit from inside: stars that twinkle out of step, and a glow that sweeps across", () => {
    expect(track(".effort-slider .es-fill::before")).toMatch(/animation:\s*es-twinkle [\d.]+s/);
    expect(track(".effort-slider .es-fill::after")).toMatch(/animation:\s*es-twinkle [\d.]+s/);
    expect(track(".effort-slider .es-fill::before")).toMatch(/radial-gradient/);
    expect(track(".effort-slider .es-glow")).toMatch(/animation:\s*es-sweep [\d.]+s/);
    expect(css).toMatch(/@keyframes es-twinkle\s*\{[^}]*opacity/);
    expect(css).toMatch(/@keyframes es-sweep\s*\{[^}]*translateX/);
  });

  it("holds still, and shows its stars, under reduced motion", () => {
    expect(css).toMatch(/prefers-reduced-motion: reduce\)[^@]*\.es-fill::before[^{]*\{[^}]*animation:\s*none/);
    expect(css).toMatch(/:root\[data-effects="reduced"\] \.effort-slider[^{]*\{[^}]*animation:\s*none/);
  });

  it("has a white round thumb as wide as the track is tall", () => {
    const thumb = track('.effort-slider input[type="range"]::-webkit-slider-thumb');
    expect(thumb).toMatch(/width:\s*30px/);
    expect(thumb).toMatch(/height:\s*30px/);
    expect(thumb).toMatch(/border-radius:\s*50%/);
    expect(thumb).toMatch(/background:\s*#fff/);
    expect(thumb).toMatch(/box-shadow:[^;]*0 0 \d+px/);
  });
});
