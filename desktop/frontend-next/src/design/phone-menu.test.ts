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
    expect(sheet).toMatch(/border-radius:\s*var\(--r-lg\) var\(--r-lg\) 0 0/);
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
    expect(row).toMatch(/border-radius:\s*var\(--r-sm\)/);
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

describe("the reasoning depth line", () => {
  const track = (selector: string): string => rule(selector);

  it("is a card whose header sets the level on the left and the model on the right", () => {
    expect(track("\n.effort-slider")).toMatch(/background:\s*var\(--overlay\)/);
    expect(track("\n.effort-slider")).toMatch(/border-radius:\s*var\(--r-lg\)/);
    const head = track(".effort-slider .es-head");
    expect(head).toMatch(/display:\s*flex/);
    expect(head).toMatch(/justify-content:\s*space-between/);
    expect(track(".effort-slider .es-name")).toMatch(/color:\s*var\(--text\)/);
    expect(track(".effort-slider .es-name")).toMatch(/font:\s*var\(--w-semi\) var\(--fs-lead\)/);
    expect(track(".effort-slider .es-model")).toMatch(/color:\s*var\(--muted\)/);
  });

  it("is a thin line, filled in the product's own colour as far as the thumb", () => {
    const line = track(".effort-slider .es-line");
    expect(line).toMatch(/height:\s*4px/);
    expect(line).toMatch(/background:\s*var\(--border\)/);
    const fill = track(".effort-slider .es-fill");
    expect(fill).toMatch(/height:\s*4px/);
    expect(fill).toMatch(/width:\s*calc\(var\(--p\) \* \(100% - 28px\) \+ 14px\)/);
    expect(fill).toMatch(/var\(--brand\)/);
  });

  it("carries a current along the filled part toward the thumb, and holds still under reduced motion", () => {
    expect(track(".effort-slider .es-fill")).toMatch(/animation:\s*es-flow [\d.]+s linear infinite/);
    expect(css).toMatch(/@keyframes es-flow\s*\{[^}]*background-position-x:\s*\d+px/);
    expect(css).toMatch(/prefers-reduced-motion: reduce\)[^@]*\.es-fill[^{]*\{[^}]*animation:\s*none/);
    expect(css).toMatch(/:root\[data-effects="reduced"\] \.effort-slider \.es-fill[^{]*\{[^}]*animation:\s*none/);
  });

  it("marks every level with a node: hollow ahead of the thumb, solid once passed", () => {
    const node = track(".effort-slider .es-tick");
    expect(node).toMatch(/left:\s*calc\(14px \+ var\(--k\) \* \(100% - 28px\)\)/);
    expect(node).toMatch(/width:\s*10px/);
    expect(node).toMatch(/border-radius:\s*50%/);
    expect(track(".effort-slider .es-tick[data-on]")).toMatch(/background:\s*var\(--brand\)/);
  });

  it("has a thumb that wears the product's own mark inside a ring of its colour", () => {
    const thumb = track(".effort-slider .es-thumb");
    expect(thumb).toMatch(/left:\s*calc\(var\(--p\) \* \(100% - 28px\)\)/);
    expect(thumb).toMatch(/width:\s*28px/);
    expect(thumb).toMatch(/border:\s*2px solid var\(--brand\)/);
    expect(thumb).toMatch(/background:\s*var\(--raised\)/);
    expect(track(".effort-slider .es-r")).toMatch(/stroke:\s*var\(--brand\)/);
  });

  it("names each level under its node, centred on it, the current one in the text colour", () => {
    const mark = track(".effort-slider .es-mark");
    expect(mark).toMatch(/left:\s*calc\(14px \+ var\(--k\) \* \(100% - 28px\)\)/);
    expect(mark).toMatch(/transform:\s*translateX\(-50%\)/);
    expect(track(".effort-slider .es-mark[data-on]")).toMatch(/color:\s*var\(--text\)/);
  });

  it("keeps the native thumb as the hit area, as wide as the thumb, and draws nothing with it", () => {
    const thumb = track('.effort-slider input[type="range"]::-webkit-slider-thumb');
    expect(thumb).toMatch(/width:\s*28px/);
    expect(thumb).toMatch(/height:\s*28px/);
    expect(thumb).toMatch(/opacity:\s*0/);
  });

  it("registers its position as a number, so the fill, thumb and nodes ease together", () => {
    expect(css).toMatch(/@property --p\s*\{[^}]*syntax:\s*['"]<number>['"][^}]*initial-value:\s*0/);
    expect(track(".effort-slider .es-track")).toMatch(/transition:\s*--p var\(--m-card\)/);
    expect(track(".effort-slider .es-track[data-drag]")).toMatch(/transition:\s*none/);
  });

  it("keeps everything inside the card", () => {
    const card = track("\n.effort-slider");
    expect(card).toMatch(/overflow:\s*hidden/);
    expect(card).toMatch(/padding:\s*14px 18px/);
  });

  it("is made of the product's own colours alone: nothing borrowed from another product's slider", () => {
    const slider = css.slice(css.indexOf("@property --p"), css.indexOf("@keyframes sheet-up"));
    for (const borrowed of ["es-twinkle", "es-drift", "es-sweep", "es-glow", "radial-gradient", "#b36ef0", "#a15ff0", "#6552de", "#fff", "background-clip: text"]) {
      expect(slider).not.toContain(borrowed);
    }
  });
});

describe("the sheet's search box", () => {
  it("lines up with the rows' edges and keeps a gap above the list", () => {
    const find = rule(S + ' :is(.mfind, input[type="search"])');
    expect(find).toMatch(/width:\s*calc\(100% - 8px\)/);
    expect(find).toMatch(/margin:\s*0 4px 8px/);
    expect(find).toMatch(/box-sizing:\s*border-box/);
  });
});
