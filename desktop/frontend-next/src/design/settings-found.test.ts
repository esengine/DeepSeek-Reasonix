import { describe, expect, it } from "vitest";
import settings from "./settings.css?raw";
import studio from "../styles/studio.css?raw";

const rule = (selector: string): string => {
  const at = settings.lastIndexOf("\n" + selector + " {");
  return at < 0 ? "" : settings.slice(at, settings.indexOf("}", at));
};

describe("the settings search results", () => {
  const found = rule(':root:not([data-fold~="phone"]) .prefs-found');

  it("fill the page column beside the category list, not float across it", () => {
    expect(settings).toMatch(/\.prefs-sheet \{ grid-template-columns: 240px /);
    expect(found).toMatch(/left:\s*240px/);
    expect(found).toMatch(/right:\s*0/);
    expect(found).toMatch(/bottom:\s*0/);
    expect(found).toMatch(/width:\s*auto/);
  });

  it("cover the whole column, leaving room above the results", () => {
    expect(found).toMatch(/top:\s*0/);
    expect(found).toMatch(/padding:\s*58px /);
    expect(studio).toMatch(/\.prefs-hd \{ min-height: 58px/);
  });
});
