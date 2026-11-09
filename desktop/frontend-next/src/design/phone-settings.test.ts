import { describe, expect, it } from "vitest";
import settings from "./phone-settings.css?raw";
import index from "./design.css?raw";

const S = '[data-fold~="phone"] .prefs-sheet';

const rule = (selector: string): string => {
  const at = settings.indexOf(selector + " {");
  return at < 0 ? "" : settings.slice(at, settings.indexOf("}", at));
};

describe("the settings sheet on a phone", () => {
  it("is part of the design layer, after the base settings styles", () => {
    expect(index).toMatch(/@import "\.\/settings\.css" layer\(design\)[\s\S]*@import "\.\/phone-settings\.css" layer\(design\)/);
  });

  it("is one column, so the page gets the whole width instead of what a nav column leaves", () => {
    const sheet = rule(S);
    expect(sheet).toMatch(/grid-template-columns:\s*minmax\(0, 1fr\)/);
    expect(sheet).toMatch(/grid-template-rows:\s*auto auto auto minmax\(0, 1fr\)/);
  });

  it("stacks title, search, the category strip and the page, in that order", () => {
    expect(rule(S + " .prefs-hd h1")).toMatch(/grid-column:\s*1/);
    expect(rule(S + " .prefs-hd h1")).toMatch(/grid-row:\s*1/);
    expect(rule(S + " .prefs-find")).toMatch(/grid-row:\s*2/);
    expect(rule(S + " .prefs-nav")).toMatch(/grid-row:\s*3/);
    expect(rule(S + " .prefs-main")).toMatch(/grid-row:\s*4/);
    expect(rule(S + " .prefs-main")).toMatch(/grid-column:\s*1/);
  });

  it("turns the category list into a strip that scrolls sideways without a scrollbar", () => {
    const nav = rule(S + " .prefs-nav");
    expect(nav).toMatch(/display:\s*flex/);
    expect(nav).toMatch(/flex-direction:\s*row/);
    expect(nav).toMatch(/overflow-x:\s*auto/);
    expect(nav).toMatch(/scrollbar-width:\s*none/);
    expect(nav).toMatch(/border-right:\s*0/);
  });

  it("keeps each category a pill that does not shrink, and drops the headings and values beside them", () => {
    expect(rule(S + " .prefs-nav button")).toMatch(/flex:\s*none/);
    expect(rule(S + " .prefs-nav :is(.navsec, .nv)")).toMatch(/display:\s*none/);
  });

  it("gives the page comfortable side padding and room under it", () => {
    expect(rule(S + " .prefs-main")).toMatch(/padding:\s*16px 14px 48px/);
  });

  it("lets a single-page sheet use the whole body, as it does on a wide screen", () => {
    const solo = rule(S + "[data-solo] .prefs-main");
    expect(solo).toMatch(/grid-column:\s*1/);
    expect(solo).toMatch(/grid-row:\s*2 \/ -1/);
  });
});

describe("a settings page on a phone", () => {
  const P = '[data-fold~="phone"] .prefs-sheet .prefs-main';

  it("sets the page heading and its two lines of explanation across the full width, one under another", () => {
    const head = rule(P + " .prefs-pagetitle");
    expect(head).toMatch(/display:\s*grid/);
    expect(head).toMatch(/gap:\s*6px/);
    expect(rule(P + " .prefs-pagetitle > *")).toMatch(/max-width:\s*none/);
  });

  it("lets a row's control drop under its text when the two do not both fit", () => {
    expect(rule(P + " .srow")).toMatch(/flex-wrap:\s*wrap/);
    expect(rule(P + " .srow")).toMatch(/row-gap:\s*8px/);
    const meta = rule(P + " .srow-meta");
    expect(meta).toMatch(/flex:\s*1 1 12em/);
    expect(meta).toMatch(/min-width:\s*min\(12em, 100%\)/);
    expect(rule(P + " .srow-ctl")).toMatch(/margin-left:\s*auto/);
  });

  it("shows a row's explanation in full instead of cutting it at the first line", () => {
    const desc = rule(P + " .srow-desc");
    expect(desc).toMatch(/white-space:\s*normal/);
    expect(desc).toMatch(/overflow:\s*visible/);
  });
});

describe("the settings blocks whose layout is their own", () => {
  const P = '[data-fold~="phone"] .prefs-sheet .prefs-main';

  it("sets a block's control under its heading instead of beside it", () => {
    expect(rule(P + " .prefs-card > .grp")).toMatch(/grid-template-columns:\s*minmax\(0, 1fr\)/);
    const items = rule(P + " .prefs-card > .grp > .grp-items");
    expect(items).toMatch(/grid-column:\s*1/);
    expect(items).toMatch(/grid-row:\s*auto/);
    expect(items).toMatch(/margin-top:\s*8px/);
  });

  it("lets an extension's summary line wrap, so its version and source are read whole", () => {
    expect(rule(P + " .srv > summary, " + P + " .srv > .srv-hd")).toMatch(/flex-wrap:\s*wrap/);
    const sub = rule(P + " .srv .srv-sub");
    expect(sub).toMatch(/flex-wrap:\s*wrap/);
    expect(sub).toMatch(/white-space:\s*normal/);
    expect(sub).toMatch(/overflow:\s*visible/);
  });
});

describe("the page title and the skills list on a phone", () => {
  const P = S + " .prefs-main";

  it("gives the title the full width instead of reserving the desktop's right-hand gutter", () => {
    expect(rule(P + " .prefs-pagetitle")).toMatch(/padding-right:\s*0/);
  });

  it("lets a skill's controls drop below its text instead of squeezing it", () => {
    expect(rule(P + " .skrow")).toMatch(/flex-wrap:\s*wrap/);
    const main = rule(P + " .skrow .sk-main");
    expect(main).toMatch(/flex:\s*1 1 12em/);
    expect(main).toMatch(/min-width:\s*min\(12em, 100%\)/);
  });
});

describe("the settings sheet fills a phone's screen", () => {
  it("drops the backdrop's margin and the sheet's frame", () => {
    expect(rule('[data-fold~="phone"] .prefs')).toMatch(/padding:\s*0/);
    expect(rule('[data-fold~="phone"] .prefs')).toMatch(/inset:\s*0/);
    const sheet = rule("\n" + S + " {");
    expect(sheet).toBe("");
    const full = rule(S);
    expect(full).toMatch(/width:\s*100%/);
    expect(full).toMatch(/height:\s*100%/);
    expect(full).toMatch(/border:\s*0/);
    expect(full).toMatch(/border-radius:\s*0/);
  });
});

describe("the desktop's nav-column decorations on a phone", () => {
  it("draws neither the divider nor the darker strip behind a column that is no longer there", () => {
    expect(rule(S + "::before, " + S + "::after")).toMatch(/display:\s*none/);
  });
});

describe("the category strip's edge", () => {
  it("fades out at the right edge, so a cut-off tab reads as more to scroll to, and leaves room to scroll it clear", () => {
    const nav = rule(S + " .prefs-nav");
    expect(nav).toMatch(/mask-image:\s*linear-gradient\(90deg, #000 calc\(100% - 28px\), transparent\)/);
    expect(nav).toMatch(/padding:\s*4px 28px 8px 10px/);
  });
});
