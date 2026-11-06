import { describe, expect, it } from "vitest";
import css from "./recovery.css?raw";

const rule = (selector: string) => {
  const at = css.indexOf(`${selector} {`);
  return at < 0 ? "" : css.slice(css.indexOf("{", at) + 1, css.indexOf("}", at));
};

describe("the config banner follows the board", () => {
  it("is a hairline card with a tinted header and a footer rule", () => {
    expect(rule(".cfgbad .cfg-card")).toMatch(/border-radius:\s*var\(--r-md\)/);
    expect(rule(".cfgbad .cfg-hd")).toMatch(/padding:\s*12px 14px/);
    expect(rule(".cfgbad .cfg-card[data-lvl=\"err\"] .cfg-hd")).toMatch(/var\(--err\) 8%/);
    expect(rule(".cfgbad .cfg-card[data-lvl=\"warn\"] .cfg-hd")).toMatch(/var\(--warn\) 12%/);
    expect(rule(".cfgbad .cfg-ft")).toMatch(/border-top:\s*1px solid var\(--hair\)/);
  });

  it("sets the diff in the mono face with a 30px line gutter and error / ok tints", () => {
    expect(rule(".cfgbad .cfg-diff")).toMatch(/var\(--mono\)/);
    expect(rule(".cfgbad .cfg-diff > div > span")).toMatch(/width:\s*30px/);
    expect(rule(".cfgbad .cfg-diff .er")).toMatch(/var\(--err\) 10%/);
    expect(rule(".cfgbad .cfg-diff .ok")).toMatch(/var\(--ok\) 10%/);
  });
});

describe("the export chips follow the board", () => {
  it("are 28px pills whose selected state is the accent border and a filled 16px box", () => {
    expect(rule(".bk-chip")).toMatch(/height:\s*28px/);
    expect(rule(".bk-chip")).toMatch(/border-radius:\s*var\(--r-pill\)/);
    expect(rule(".bk-chip[data-on]")).toMatch(/border-color:\s*var\(--accent\)/);
    expect(rule(".bk-chip .box")).toMatch(/width:\s*16px/);
    expect(rule(".bk-chip[data-on] .box")).toMatch(/background:\s*var\(--accent\)/);
  });

  it("lays the two passphrase fields side by side", () => {
    expect(rule(".bk-fields")).toMatch(/grid-template-columns:\s*1fr 1fr/);
  });
});

describe("the import results follow the board", () => {
  it("draws a landed theme as a green strip and a refused one as a warning strip", () => {
    expect(rule(".themeacts[data-landed]")).toMatch(/var\(--ok\) 10%/);
    expect(rule(".themefail")).toMatch(/var\(--warn\) 12%/);
  });

  it("draws the armed delete in the error tone", () => {
    expect(css).toContain('.pal-rm[data-confirming]');
  });

  it("draws the session-import placeholder as a 440px dialog with a dashed drop area", () => {
    expect(rule(".ns-dlg")).toMatch(/width:\s*440px/);
    expect(rule(".ns-dlg .ns-drop")).toMatch(/border:\s*1px dashed var\(--border\)/);
  });
});

describe("the as-written fallback follows the board", () => {
  it("tags the source in a 20px pill and sets it in the mono face on the page surface", () => {
    expect(rule(".srcfb .srcfb-tag")).toMatch(/height:\s*20px/);
    expect(rule(".srcfb .srcfb-src")).toMatch(/var\(--mono\)/);
    expect(rule(".srcfb .srcfb-src")).toMatch(/background:\s*var\(--page\)/);
  });
});

describe("the simplified tool card follows the board", () => {
  it("is a one-line hairline row with the tag at its end", () => {
    expect(rule(".toolfb")).toMatch(/border-radius:\s*var\(--r-md\)/);
    expect(rule(".toolfb .toolfb-nm")).toMatch(/white-space:\s*nowrap/);
  });
});
